package mogura

import (
	"fmt"
	"io"
	"net"
	"strconv"

	"golang.org/x/crypto/ssh"
)

type TunnelConfig struct {
	LocalBindPort        string
	ForwardingRemotePort string
}

type Mogura struct {
	Name             string
	BastionHostPort  string
	Username         string
	KeyPath          string
	LocalBindPort    string
	ForwardingTarget RemoteTarget

	// internal
	sshClientConn  *ssh.Client
	localListener  net.Listener
	detectedRemote string
}

type RemoteTarget struct {
	ResolverType string
	RemoteName   string
	RemotePort   int
}

func (t RemoteTarget) Resolve() (string, error) {
	switch t.ResolverType {
	case "REMOTE-DNS":
		// TODO resolve A,AAAA,CNAME,SRV in bastion env resolver (ex: Route53 private DNS
		return "", fmt.Errorf("not yet implemented remote DNS resolver.")
	case "HOST-PORT":
		fallthrough
	default:
		// default Host and Port
		detectedRemote := t.RemoteName + ":" + strconv.Itoa(t.RemotePort)

		return detectedRemote, nil
	}
}

// error is ssh connection and local listener error.
// error channel transfer flow error
func (m *Mogura) Go() (<-chan error, error) {
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

	errChan := make(chan error)

	// go accept loop
	go func() {
		for {
			// Setup localConn (type net.Conn)
			localConn, err := m.localListener.Accept()
			if err != nil {
				errChan <- fmt.Errorf("listen.Accept failed: %v", err)
				// maybe reconnection.
			}

			// go forwarding
			go forward(localConn, m.sshClientConn, m.detectedRemote, errChan)
		}
	}()

	return errChan, nil
}

func (m *Mogura) ConnectSSH() error {
	clientConfig, err := GenSSHClientConfig(m.BastionHostPort, m.Username, m.KeyPath)
	if err != nil {
		return fmt.Errorf("ssh config error: %v", err)
	}

	// Setup sshClientConn (type *ssh.ClientConn)
	m.sshClientConn, err = ssh.Dial("tcp", m.BastionHostPort, clientConfig)
	if err != nil {
		return fmt.Errorf("ssh.Dial failed: %v", err)
	}

	return nil
}

func (m *Mogura) Listen() error {
	// Setup localListener (type net.Listener)
	var err error
	m.localListener, err = net.Listen("tcp", m.LocalBindPort)
	if err != nil {
		return fmt.Errorf("local port binding failed: %v", err)
	}

	return nil
}

func (m *Mogura) ResolveRemote() error {
	detected, err := m.ForwardingTarget.Resolve()
	if err != nil {
		return err
	}

	m.detectedRemote = detected
	return nil
}

func (m *Mogura) Close() error {
	if m.localListener != nil {
		m.localListener.Close()
	}
	if m.sshClientConn != nil {
		m.sshClientConn.Close()

	}

	// TODO error return
	return nil
}

func forward(localConn net.Conn, sshClientConn *ssh.Client, remoteport string, errChan chan<- error) {
	// Setup sshConn (type net.Conn)
	sshConn, err := sshClientConn.Dial("tcp", remoteport)
	// Copy localConn.Reader to sshConn.Writer
	go func() {
		_, err = io.Copy(sshConn, localConn)
		if err != nil {
			errChan <- fmt.Errorf("local -> remote transfer failed: %v", err)
		}
	}()
	// Copy sshConn.Reader to localConn.Writer
	go func() {
		_, err = io.Copy(localConn, sshConn)
		if err != nil {
			errChan <- fmt.Errorf("remote -> local transfer failed: %v", err)
		}
	}()
}
