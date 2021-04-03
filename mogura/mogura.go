package mogura

import (
	"context"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	ENV_MOGURA_PASSPHRASE = "MOGURA_PASSPHRASE"

	WarningThresholdForRetrying = 3
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

	// test ssh connection fowarding
	testSshConn, err := m.sshClientConn.Dial("tcp", m.detectedRemote)
	if err != nil {
		// close local listener and remote connection. client can request to listener and wait forever if this close forgot.
		m.Close()
		if strings.Contains(err.Error(), "administratively prohibited") {
			return nil, fmt.Errorf("remote server does not allowed forwarding, please check sshd config or SELinux settings and more. original error: %v", err)
		} else {
			return nil, fmt.Errorf("remote dial test failed: %v", err)
		}
	}
	testSshConn.Close()

	ctx := context.TODO()
	// go accept loop
	go func(ctx context.Context) {
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
					localConn.Close()
					return
				default:
					// if not allowed forwarding in remote server by sshd config or SELinux, etc...
					if strings.Contains(err.Error(), "administratively prohibited") {
						m.errChan <- fmt.Errorf("remote server does not allowed forwarding, please check sshd config or SELinux settings and more. original error: %v", err)

						// close local listener connection that already accepted. client request wait forever if this close forgot.
						localConn.Close()

						// close local listener and remote connection. client can request to listener and wait forever if this close forgot.
						m.Close()
						return
					} else {
						m.errChan <- fmt.Errorf("remote dial failed: %v", err)
					}

					// not remote done? SSH connection is dead?
					sshErr := m.ConnectSSH()
					if sshErr != nil {
						m.errChan <- fmt.Errorf("failed ssh reconnect: %v", sshErr)
					}

					localConn.Close()
					continue
				}
			}

			// go forwarding
			timeout := m.Config.ForwardingTarget.ForwardingTimeout
			go forward(ctx, localConn, sshConn, timeout, m.errChan)
		}
	}(ctx)

	return m, nil
}

type Mogura struct {
	Config MoguraConfig

	errChan chan error

	// internal
	sshClientConn  *ssh.Client
	localListener  net.Listener
	detectedRemote string

	sshMutex sync.Mutex

	localDoneChan  chan struct{}
	remoteDoneChan chan struct{}
}

func (m *Mogura) ErrChan() <-chan error {
	return m.errChan
}

func (m *Mogura) ConnectSSH() error {
	m.sshMutex.Lock()
	defer m.sshMutex.Unlock()

	passphrase := os.Getenv(ENV_MOGURA_PASSPHRASE)
	clientConfig, err := GenSSHClientConfig(m.Config.BastionHostPort, m.Config.Username, m.Config.KeyPath, passphrase)
	if err != nil {
		return fmt.Errorf("ssh config error: %v", err)
	}

	// Setup sshClientConn (type *ssh.ClientConn)
	sshClientConn, err := ssh.Dial("tcp", m.Config.BastionHostPort, clientConfig)
	if err != nil {
		return fmt.Errorf("ssh.Dial failed: %v", err)
	}

	// close current connection before change new connection.
	if m.sshClientConn != nil {
		m.sshClientConn.Close()
	}

	m.sshClientConn = sshClientConn

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
		retryCount := 0
		for _ = range tick {
			err := m.ResolveRemote()
			if err != nil {
				retryCount++
				errChan <- err
				if retryCount > WarningThresholdForRetrying {
					errChan <- fmt.Errorf("resolve remote retry failed over %d times. it maybe will not recover it. stop mogura and check configuration", WarningThresholdForRetrying)
				}
				sshErr := m.ConnectSSH()
				if sshErr != nil {
					errChan <- fmt.Errorf("remote resolver failed and then ssh reconnect but failed: %v", sshErr)
				} else {
					// reconnected but can not notification way...
				}
			} else {
				retryCount = 0
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
	var lErr error
	if m.localListener != nil {
		if m.localDoneChan != nil {
			close(m.localDoneChan)
		}
		lErr = m.localListener.Close()
	}

	if lErr != nil {
		return fmt.Errorf("failed close local listener: %v", lErr)
	}

	m.localListener = nil
	return nil
}

func (m *Mogura) CloseRemoteConn() error {
	var rErr error
	if m.sshClientConn != nil {
		if m.remoteDoneChan != nil {
			close(m.remoteDoneChan)
		}
		rErr = m.sshClientConn.Close()
	}

	if rErr != nil {
		return fmt.Errorf("failed close ssh connection: %v", rErr)
	}

	m.sshClientConn = nil
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

func forward(ctx context.Context, localConn, sshConn net.Conn, timeout time.Duration, errChan chan<- error) {
	wg := &sync.WaitGroup{}
	ctx, cancelFunc := context.WithTimeout(ctx, timeout)

	// Copy localConn.Reader to sshConn.Writer
	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		_, err := io.Copy(sshConn, localConn)
		if err != nil {
			errChan <- fmt.Errorf("local -> remote transfer failed: %v", err)
		}
		wg.Done()
	}(wg)

	// Copy sshConn.Reader to localConn.Writer
	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		_, err := io.Copy(localConn, sshConn)
		if err != nil {
			errChan <- fmt.Errorf("remote -> local transfer failed: %v", err)
		}
		wg.Done()
	}(wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		cancelFunc()
		close(done)
	}()

	// waiting for forwarding... and close connections.
	select {
	// forwarding IO error
	case <-done:
		// currently it can not know finished forwarding. so it is process here when only happened errors in forwarding.
		errChan <- fmt.Errorf("got forwarding errors before timeout.")
	// timeout
	case <-ctx.Done():
		// basically proceed here with timeout, because currently it can not know that finished forwarding IO.
	}

	err := localConn.Close()
	if err != nil {
		errChan <- fmt.Errorf("forwarding end however failed close local conn: %v", err)
	}
	err = sshConn.Close()
	if err != nil {
		errChan <- fmt.Errorf("forwarding end, however failed close ssh conn: %v", err)
	}
}
