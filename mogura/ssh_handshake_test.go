package mogura

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newTestSigner(t *testing.T, key any) ssh.Signer {
	t.Helper()

	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("failed create signer: %v", err)
	}

	return signer
}

func newTestED25519Signer(t *testing.T) ssh.Signer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed generate ed25519 key: %v", err)
	}

	return newTestSigner(t, priv)
}

func newTestRSASigner(t *testing.T) ssh.Signer {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed generate rsa key: %v", err)
	}

	return newTestSigner(t, priv)
}

// startTestSSHServer starts an ssh server that serves the given host keys and
// returns its address. it only completes the handshake, that is all the host
// key verification needs.
func startTestSSHServer(t *testing.T, hostKeys ...ssh.Signer) string {
	t.Helper()

	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	for _, hostKey := range hostKeys {
		serverConfig.AddHostKey(hostKey)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() {
				defer conn.Close()

				sshConn, chans, reqs, err := ssh.NewServerConn(conn, serverConfig)
				if err != nil {
					// the client rejected the host key. that is a valid outcome.
					return
				}
				defer sshConn.Close()

				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					newChan.Reject(ssh.Prohibited, "test server")
				}
			}()
		}
	}()

	return listener.Addr().String()
}

// dialTestServer runs a real ssh handshake against hostport with the config
// mogura generates.
func dialTestServer(t *testing.T, hostport string, o SSHClientOption) error {
	t.Helper()

	o.HostPort = hostport
	clientConfig, err := GenSSHClientConfig(o)
	if err != nil {
		return err
	}
	clientConfig.Timeout = 5 * time.Second

	client, err := ssh.Dial("tcp", hostport, clientConfig)
	if err != nil {
		return err
	}
	client.Close()

	return nil
}

// TestSSHHandshakeHostKeyVerification runs the verification against a real
// handshake, so the client and the server actually negotiate a host key.
func TestSSHHandshakeHostKeyVerification(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	hostKey := newTestED25519Signer(t)
	hostport := startTestSSHServer(t, hostKey)

	t.Run("recorded host key connects", func(t *testing.T) {
		// Arrange
		knownHosts := writeKnownHosts(t, hostport, hostKey.PublicKey())

		// Act
		err := dialTestServer(t, hostport, SSHClientOption{
			Username:       "mogura",
			KeyPath:        keyPath,
			KnownHostsPath: knownHosts,
		})

		// Assert
		if err != nil {
			t.Fatalf("want connected, got error: %v", err)
		}
	})

	t.Run("unknown host is rejected", func(t *testing.T) {
		// Arrange
		// known_hosts records another host, so this bastion is unknown.
		knownHosts := writeKnownHosts(t, "other.example.com:22", hostKey.PublicKey())

		// Act
		err := dialTestServer(t, hostport, SSHClientOption{
			Username:       "mogura",
			KeyPath:        keyPath,
			KnownHostsPath: knownHosts,
		})

		// Assert
		if err == nil {
			t.Fatal("want connection rejected, got connected")
		}
		if !strings.Contains(err.Error(), "ssh-keyscan") {
			t.Errorf("want error containing ssh-keyscan for recovery, got: %v", err)
		}
	})

	t.Run("changed host key is rejected", func(t *testing.T) {
		// Arrange
		// the recorded key is not the one the server serves.
		knownHosts := writeKnownHosts(t, hostport, newTestED25519Signer(t).PublicKey())

		// Act
		err := dialTestServer(t, hostport, SSHClientOption{
			Username:       "mogura",
			KeyPath:        keyPath,
			KnownHostsPath: knownHosts,
		})

		// Assert
		if err == nil {
			t.Fatal("want connection rejected, got connected")
		}
		if !strings.Contains(err.Error(), "man-in-the-middle") {
			t.Errorf("want error that reports a possible attack, got: %v", err)
		}
	})

	t.Run("insecure option connects without known_hosts", func(t *testing.T) {
		// Act
		err := dialTestServer(t, hostport, SSHClientOption{
			Username:              "mogura",
			KeyPath:               keyPath,
			InsecureIgnoreHostKey: true,
		})

		// Assert
		if err != nil {
			t.Fatalf("want connected, got error: %v", err)
		}
	})
}

// TestSSHHandshakeWithMixedHostKeyTypes covers the case that makes host key
// verification unusable without HostKeyAlgorithms: the bastion serves both rsa
// and ed25519, while known_hosts records only the ed25519 key, as OpenSSH
// records it. the ssh package default prefers rsa-sha2-*, so the server would
// present a key that known_hosts has no line for.
func TestSSHHandshakeWithMixedHostKeyTypes(t *testing.T) {
	// Arrange
	keyPath := writeTestPrivateKey(t)
	rsaHostKey := newTestRSASigner(t)
	ed25519HostKey := newTestED25519Signer(t)

	hostport := startTestSSHServer(t, rsaHostKey, ed25519HostKey)
	knownHosts := writeKnownHosts(t, hostport, ed25519HostKey.PublicKey())

	// Act
	err := dialTestServer(t, hostport, SSHClientOption{
		Username:       "mogura",
		KeyPath:        keyPath,
		KnownHostsPath: knownHosts,
	})

	// Assert
	if err != nil {
		t.Fatalf("want connected with the recorded ed25519 host key, got error: %v", err)
	}

	// the same connection must fail without the algorithms taken from
	// known_hosts. this is what the default order does.
	verify, _, err := hostKeyCallback(knownHosts, hostport)
	if err != nil {
		t.Fatalf("hostKeyCallback failed: %v", err)
	}

	defaultOrderConfig := &ssh.ClientConfig{
		User:            "mogura",
		HostKeyCallback: verify,
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", hostport, defaultOrderConfig)
	if err == nil {
		client.Close()
		t.Fatal("want the default algorithm order to fail, got connected. the HostKeyAlgorithms workaround may no longer be needed")
	}
	if !strings.Contains(err.Error(), "man-in-the-middle") {
		t.Errorf("want a host key mismatch from the default order, got: %v", err)
	}
}
