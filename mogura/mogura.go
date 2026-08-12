package mogura

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	ENV_MOGURA_PASSPHRASE = "MOGURA_PASSPHRASE"

	WarningThresholdForRetrying = 3

	// resolveInterval is how often the remote target is resolved again.
	resolveInterval = 10 * time.Second
)

// errClosed is returned when mogura is asked to use a connection it has
// already closed.
var errClosed = errors.New("mogura is closed")

type MoguraConfig struct {
	Name             string
	BastionHostPort  string
	Username         string
	KeyPath          string
	RemoteDNS        string
	LocalBindPort    string
	ForwardingTarget Target

	KnownHostsPath        string
	InsecureIgnoreHostKey bool
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
		// the ssh connection is already open, it must not be left behind.
		m.Close()
		return nil, err
	}

	err = m.ResolveRemote()
	if err != nil {
		// the local port is already bound, it must not be left behind.
		m.Close()
		return nil, err
	}

	m.errChan = make(chan error)
	resolveErrChan := m.GoResolveCycle(resolveInterval)
	go func() {
		// chain error channel
		for e := range resolveErrChan {
			select {
			case m.errChan <- e:
			case <-m.remoteDoneChan:
				return
			}
		}
	}()

	// test ssh connection fowarding
	client, remote, err := m.connection()
	if err != nil {
		m.Close()
		return nil, err
	}

	testSshConn, err := client.Dial("tcp", remote)
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
			listener, err := m.listener()
			if err != nil {
				// mogura was closed.
				return
			}

			// Setup localConn (type net.Conn)
			// closed check logic refs:
			// https://stackoverflow.com/questions/13417095/how-do-i-stop-a-listening-server-in-go
			localConn, err := listener.Accept()
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

			client, remote, err := m.connection()
			if err != nil {
				localConn.Close()
				return
			}

			// Setup sshConn (type net.Conn)
			sshConn, err := client.Dial("tcp", remote)
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
						if errors.Is(sshErr, errClosed) {
							localConn.Close()
							return
						}
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

	// connectMu serializes reconnects. it is held across ssh.Dial, so that two
	// goroutines do not open a connection at the same time.
	connectMu sync.Mutex

	// stateMu guards the fields below. it is never held across a network call,
	// the accept loop must not wait for a reconnect to finish.
	stateMu        sync.Mutex
	sshClientConn  *ssh.Client
	localListener  net.Listener
	detectedRemote string
	closed         bool

	localDoneChan   chan struct{}
	remoteDoneChan  chan struct{}
	localCloseOnce  sync.Once
	remoteCloseOnce sync.Once
}

func (m *Mogura) ErrChan() <-chan error {
	return m.errChan
}

// connection returns the ssh client and the address to forward to as one
// consistent pair, so that the caller can not mix a client with an address
// that was resolved for another one.
func (m *Mogura) connection() (*ssh.Client, string, error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	if m.sshClientConn == nil {
		return nil, "", errClosed
	}

	return m.sshClientConn, m.detectedRemote, nil
}

func (m *Mogura) client() (*ssh.Client, error) {
	client, _, err := m.connection()

	return client, err
}

func (m *Mogura) listener() (net.Listener, error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	if m.localListener == nil {
		return nil, errClosed
	}

	return m.localListener, nil
}

// setDetectedRemote stores addr and reports the address it replaced, so that
// the caller can log the change without holding the lock.
func (m *Mogura) setDetectedRemote(addr string) (string, bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	previous := m.detectedRemote
	if addr == "" || addr == previous {
		return previous, false
	}
	m.detectedRemote = addr

	return previous, true
}

func (m *Mogura) ConnectSSH() error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	passphrase := os.Getenv(ENV_MOGURA_PASSPHRASE)
	clientConfig, err := GenSSHClientConfig(SSHClientOption{
		HostPort:              m.Config.BastionHostPort,
		Username:              m.Config.Username,
		KeyPath:               m.Config.KeyPath,
		Passphrase:            passphrase,
		KnownHostsPath:        m.Config.KnownHostsPath,
		InsecureIgnoreHostKey: m.Config.InsecureIgnoreHostKey,
	})
	if err != nil {
		return fmt.Errorf("ssh config error: %v", err)
	}

	// Setup sshClientConn (type *ssh.ClientConn)
	sshClientConn, err := ssh.Dial("tcp", m.Config.BastionHostPort, clientConfig)
	if err != nil {
		return fmt.Errorf("ssh.Dial failed: %v", err)
	}

	m.stateMu.Lock()
	if m.closed {
		m.stateMu.Unlock()
		// mogura was closed while dialing. this connection must not revive it.
		sshClientConn.Close()

		return errClosed
	}
	current := m.sshClientConn
	m.sshClientConn = sshClientConn
	m.stateMu.Unlock()

	// close current connection before change new connection.
	if current != nil {
		current.Close()
	}

	return nil
}

func (m *Mogura) Listen() error {
	// Setup localListener (type net.Listener)
	localListener, err := net.Listen("tcp", m.Config.LocalBindPort)
	if err != nil {
		return fmt.Errorf("local port binding failed: %v", err)
	}

	m.stateMu.Lock()
	m.localListener = localListener
	m.stateMu.Unlock()

	return nil
}

func (m *Mogura) GoResolveCycle(interval time.Duration) <-chan error {
	errChan := make(chan error)

	go func() {
		defer close(errChan)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		retryCount := 0
		for {
			select {
			case <-m.remoteDoneChan:
				return
			case <-ticker.C:
			}

			err := m.ResolveRemote()
			if err == nil {
				retryCount = 0
				continue
			}

			if errors.Is(err, errClosed) {
				return
			}

			retryCount++
			if !m.reportResolveErr(errChan, err) {
				return
			}

			if retryCount > WarningThresholdForRetrying {
				if !m.reportResolveErr(errChan, fmt.Errorf("resolve remote retry failed over %d times. it maybe will not recover it. stop mogura and check configuration", WarningThresholdForRetrying)) {
					return
				}
			}

			sshErr := m.ConnectSSH()
			if sshErr != nil {
				if errors.Is(sshErr, errClosed) {
					return
				}
				if !m.reportResolveErr(errChan, fmt.Errorf("remote resolver failed and then ssh reconnect but failed: %v", sshErr)) {
					return
				}
			}
		}
	}()

	return errChan
}

// reportResolveErr reports err and tells whether the resolve cycle should keep
// going. it gives up when mogura is closed, so that a reader that went away
// can not block the cycle forever.
func (m *Mogura) reportResolveErr(errChan chan<- error, err error) bool {
	select {
	case errChan <- err:
		return true
	case <-m.remoteDoneChan:
		return false
	}
}

func (m *Mogura) ResolveRemote() error {
	client, err := m.client()
	if err != nil {
		return err
	}

	err = m.Config.ForwardingTarget.Resolve(client, m.Config.RemoteDNS)
	if err != nil {
		return err
	}

	detect := m.Config.ForwardingTarget.ResolvedTargetAndPort()
	if previous, changed := m.setDetectedRemote(detect); changed {
		// TODO logging
		log.Printf("target changed: %s -> %s", previous, detect)
	}

	return nil
}

func (m *Mogura) CloseLocalConn() error {
	var localListener net.Listener

	m.localCloseOnce.Do(func() {
		if m.localDoneChan != nil {
			close(m.localDoneChan)
		}

		m.stateMu.Lock()
		localListener = m.localListener
		m.localListener = nil
		m.stateMu.Unlock()
	})

	if localListener == nil {
		return nil
	}

	// closing is done outside the lock, it can block on the network.
	if err := localListener.Close(); err != nil {
		return fmt.Errorf("failed close local listener: %v", err)
	}

	return nil
}

func (m *Mogura) CloseRemoteConn() error {
	var sshClientConn *ssh.Client

	m.remoteCloseOnce.Do(func() {
		if m.remoteDoneChan != nil {
			close(m.remoteDoneChan)
		}

		m.stateMu.Lock()
		// closed keeps a reconnect that is already dialing from reviving mogura.
		m.closed = true
		sshClientConn = m.sshClientConn
		m.sshClientConn = nil
		m.stateMu.Unlock()
	})

	if sshClientConn == nil {
		return nil
	}

	if err := sshClientConn.Close(); err != nil {
		return fmt.Errorf("failed close ssh connection: %v", err)
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
