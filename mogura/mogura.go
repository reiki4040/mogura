package mogura

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
)

type TunnelConfig struct {
	LocalBindPort        string
	ForwardingRemotePort string
}

type Mogura struct {
	Name                 string
	BastionHostPort      string
	Username             string
	KeyPath              string
	LocalBindPort        string
	ForwardingRemotePort string
}

// error is mogura config and binding error.
// error channel send transfer error
func (m *Mogura) Go() (<-chan error, error) {
	clientConfig, err := GenSSHClientConfig(m.BastionHostPort, m.Username, m.KeyPath)
	if err != nil {
		return nil, err
	}

	// Setup localListener (type net.Listener)
	localListener, err := net.Listen("tcp", m.LocalBindPort)
	if err != nil {
		return nil, fmt.Errorf("local port binding failed: %v", err)
	} else {
		errChan := make(chan error)

		// go accept loop
		go func() {
			for {
				// Setup localConn (type net.Conn)
				localConn, err := localListener.Accept()
				if err != nil {
					errChan <- fmt.Errorf("listen.Accept failed: %v", err)
					// maybe reconnection.
				}

				// go forwarding
				go forward(errChan, localConn, m.BastionHostPort, m.ForwardingRemotePort, clientConfig)
			}
		}()

		return errChan, nil
	}
}

func forward(errChan chan<- error, localConn net.Conn, hostport, remoteport string, config *ssh.ClientConfig) {
	// Setup sshClientConn (type *ssh.ClientConn)
	sshClientConn, err := ssh.Dial("tcp", hostport, config)
	if err != nil {
		errChan <- fmt.Errorf("ssh.Dial failed: %v", err)
	}
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
