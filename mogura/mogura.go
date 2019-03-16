package mogura

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"time"
)

type MoguraConfig struct {
	Name             string
	BastionHostPort  string
	Username         string
	KeyPath          string
	RemoteDNS        string
	LocalBindPort    string
	ForwardingTarget Target
}

// error is ssh connection and local listener error.
// error channel transfer flow error
func GoMogura(c MoguraConfig) (*Mogura, error) {
	m := &Mogura{
		Config: c,
	}

	m.localDoneChan = make(chan struct{})
	m.remoteDoneChan = make(chan struct{})
	err := m.ConnectSSH()
	if err != nil {
		return nil, err
	}

	err = m.Listen()
	if err != nil {
		return nil, err
	}

	err = m.ResolveRemote()
	if err != nil {
		return nil, err
	}

	m.errChan = make(chan error)
	resolveErrChan := m.GoResolveCycle(10)
	go func() {
		// chain error channel
		for e := range resolveErrChan {
			m.errChan <- e
		}
	}()

	// go accept loop
	go func() {
		for {
			// Setup localConn (type net.Conn)
			// closed check logic refs:
			// https://stackoverflow.com/questions/13417095/how-do-i-stop-a-listening-server-in-go
			localConn, err := m.localListener.Accept()
			if err != nil {
				select {
				case <-m.localDoneChan:
					return
				default:
					// maybe reconnection.
					m.errChan <- fmt.Errorf("listen.Accept failed: %v", err)
					continue
				}
			}

			// Setup sshConn (type net.Conn)
			sshConn, err := m.sshClientConn.Dial("tcp", m.detectedRemote)
			if err != nil {
				select {
				case <-m.remoteDoneChan:
					return
				default:
					m.errChan <- fmt.Errorf("remote dial failed: %v", err)
					continue
				}
			}

			// go forwarding
			go forward(localConn, sshConn, m.errChan)
		}
	}()

	return m, nil
}

type Mogura struct {
	Config MoguraConfig

	errChan chan error

	// internal
	sshClientConn  *ssh.Client
	localListener  net.Listener
	detectedRemote string

	localDoneChan  chan struct{}
	remoteDoneChan chan struct{}
}

func (m *Mogura) ErrChan() <-chan error {
	return m.errChan
}

func (m *Mogura) ConnectSSH() error {
	clientConfig, err := GenSSHClientConfig(m.Config.BastionHostPort, m.Config.Username, m.Config.KeyPath)
	if err != nil {
		return fmt.Errorf("ssh config error: %v", err)
	}

	// Setup sshClientConn (type *ssh.ClientConn)
	m.sshClientConn, err = ssh.Dial("tcp", m.Config.BastionHostPort, clientConfig)
	if err != nil {
		return fmt.Errorf("ssh.Dial failed: %v", err)
	}

	return nil
}

func (m *Mogura) Listen() error {
	// Setup localListener (type net.Listener)
	var err error
	m.localListener, err = net.Listen("tcp", m.Config.LocalBindPort)
	if err != nil {
		return fmt.Errorf("local port binding failed: %v", err)
	}

	return nil
}

func (m *Mogura) GoResolveCycle(interval int64) <-chan error {
	errChan := make(chan error)
	tick := time.Tick(time.Duration(interval) * time.Second)
	go func() {
		for _ = range tick {
			err := m.ResolveRemote()
			if err != nil {
				errChan <- err
			}
		}
	}()

	return errChan
}

func (m *Mogura) ResolveRemote() error {
	err := m.Config.ForwardingTarget.Resolve(m.sshClientConn, m.Config.RemoteDNS)
	if err != nil {
		return err
	}

	detect := m.Config.ForwardingTarget.ResolvedTargetAndPort()
	if detect != "" && detect != m.detectedRemote {
		// TODO logging
		log.Printf("target changed: %s -> %s", m.detectedRemote, detect)
		m.detectedRemote = detect
	}

	return nil
}

func (m *Mogura) CloseLocalConn() error {
	close(m.localDoneChan)

	var lErr error
	if m.localListener != nil {
		lErr = m.localListener.Close()
	}

	if lErr != nil {
		return fmt.Errorf("failed close local listener: %v", lErr)
	}

	return nil
}

func (m *Mogura) CloseRemoteConn() error {
	close(m.remoteDoneChan)

	var rErr error
	if m.sshClientConn != nil {
		rErr = m.sshClientConn.Close()
	}

	if rErr != nil {
		return fmt.Errorf("failed close ssh connection: %v", rErr)
	}

	return nil
}

func (m *Mogura) Close() error {
	lErr := m.CloseLocalConn()
	rErr := m.CloseRemoteConn()

	if lErr != nil && rErr != nil {
		return fmt.Errorf("%v and %v", lErr, rErr)
	}

	if lErr != nil {
		return lErr
	}

	if rErr != nil {
		return rErr
	}

	return nil
}

func forward(localConn, sshConn net.Conn, errChan chan<- error) {
	// Copy localConn.Reader to sshConn.Writer
	go func() {
		_, err := io.Copy(sshConn, localConn)
		if err != nil {
			errChan <- fmt.Errorf("local -> remote transfer failed: %v", err)
		}
	}()

	// Copy sshConn.Reader to localConn.Writer
	go func() {
		_, err := io.Copy(localConn, sshConn)
		if err != nil {
			errChan <- fmt.Errorf("remote -> local transfer failed: %v", err)
		}
	}()
}
