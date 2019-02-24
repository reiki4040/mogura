package mogura

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
)

type MoguraConfig struct {
	Name             string
	BastionHostPort  string
	Username         string
	KeyPath          string
	LocalBindPort    string
	ForwardingTarget RemoteTarget
}

// error is ssh connection and local listener error.
// error channel transfer flow error
func GoMogura(c MoguraConfig) (*Mogura, error) {
	m := &Mogura{
		Config: c,
	}

	m.doneChan = make(chan struct{})
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
	log.Printf("remote: %s", m.detectedRemote)

	m.errChan = make(chan error)

	// go accept loop
	go func() {
		for {
			// Setup localConn (type net.Conn)
			// closed check logic refs:
			// https://stackoverflow.com/questions/13417095/how-do-i-stop-a-listening-server-in-go
			localConn, err := m.localListener.Accept()
			if err != nil {
				select {
				case <-m.doneChan:
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
				case <-m.doneChan:
					return
				default:
					m.errChan <- fmt.Errorf("remote dial failed: %v", err)
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

	doneChan chan struct{}
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

func (m *Mogura) ResolveRemote() error {
	detected, err := m.Config.ForwardingTarget.Resolve()
	if err != nil {
		return err
	}

	m.detectedRemote = detected
	return nil
}

func (m *Mogura) Close() error {
	close(m.doneChan)

	var lErr error
	if m.localListener != nil {
		lErr = m.localListener.Close()
	}

	var sErr error
	if m.sshClientConn != nil {
		sErr = m.sshClientConn.Close()
	}

	if lErr != nil && sErr != nil {
		return fmt.Errorf("failed close local lister: %v, and close ssh connection: %v", lErr, sErr)
	}

	if lErr != nil {
		return fmt.Errorf("failed close local listener: %v", lErr)
	}

	if sErr != nil {
		return fmt.Errorf("failed close ssh connection: %v", sErr)
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
