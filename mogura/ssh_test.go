package mogura

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeTestPrivateKey writes an unencrypted ed25519 private key and returns its
// path.
func writeTestPrivateKey(t *testing.T) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed generate ed25519 key: %v", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "mogura test key")
	if err != nil {
		t.Fatalf("failed marshal private key: %v", err)
	}

	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("failed write private key: %v", err)
	}

	return path
}

func TestGenSSHClientConfigVerifiesHostKey(t *testing.T) {
	const hostport = "bastion.example.com:22"

	// Arrange
	keyPath := writeTestPrivateKey(t)
	hostKey := newTestHostKey(t)
	knownHostsPath := writeKnownHosts(t, hostport, hostKey)

	// Act
	config, err := GenSSHClientConfig(SSHClientOption{
		HostPort:       hostport,
		Username:       "mogura",
		KeyPath:        keyPath,
		KnownHostsPath: knownHostsPath,
	})
	if err != nil {
		t.Fatalf("GenSSHClientConfig failed: %v", err)
	}

	// Assert
	if config.HostKeyCallback == nil {
		t.Fatal("want host key verification, got no HostKeyCallback")
	}
	if len(config.HostKeyAlgorithms) == 0 {
		t.Error("want host key algorithms from known_hosts, got none")
	}

	if err := config.HostKeyCallback(hostport, testRemoteAddr(), hostKey); err != nil {
		t.Errorf("want the recorded host key verified, got error: %v", err)
	}

	if err := config.HostKeyCallback(hostport, testRemoteAddr(), newTestHostKey(t)); err == nil {
		t.Error("want an unrecorded host key rejected, got verified")
	}
}

func TestGenSSHClientConfigFailsWithoutKnownHosts(t *testing.T) {
	// Arrange
	keyPath := writeTestPrivateKey(t)
	missingPath := filepath.Join(t.TempDir(), "not_exist_known_hosts")

	// Act
	_, err := GenSSHClientConfig(SSHClientOption{
		HostPort:       "bastion.example.com:22",
		Username:       "mogura",
		KeyPath:        keyPath,
		KnownHostsPath: missingPath,
	})

	// Assert
	if err == nil {
		t.Fatal("want error when known_hosts is missing, got nil")
	}
	if !strings.Contains(err.Error(), "ssh-keyscan") {
		t.Errorf("want error containing ssh-keyscan for recovery, got: %v", err)
	}
}

func TestGenSSHClientConfigInsecureIgnoreHostKey(t *testing.T) {
	// Arrange
	keyPath := writeTestPrivateKey(t)

	// Act
	// known_hosts is not set on purpose. insecure mode must not need it.
	config, err := GenSSHClientConfig(SSHClientOption{
		HostPort:              "bastion.example.com:22",
		Username:              "mogura",
		KeyPath:               keyPath,
		InsecureIgnoreHostKey: true,
	})
	if err != nil {
		t.Fatalf("GenSSHClientConfig failed: %v", err)
	}

	// Assert
	if config.HostKeyCallback == nil {
		t.Fatal("want HostKeyCallback set, got nil")
	}
	if err := config.HostKeyCallback("bastion.example.com:22", testRemoteAddr(), newTestHostKey(t)); err != nil {
		t.Errorf("want any host key accepted in insecure mode, got error: %v", err)
	}
	if config.HostKeyAlgorithms != nil {
		t.Errorf("want the default algorithm order in insecure mode, got %v", config.HostKeyAlgorithms)
	}
}

func TestGenSSHClientConfigPrivateKey(t *testing.T) {
	knownHostsPath := writeKnownHosts(t, "bastion.example.com:22", newTestHostKey(t))

	cases := []struct {
		name            string
		keyPath         func(t *testing.T) string
		wantErrContains string
	}{
		{
			name:            "missing private key",
			keyPath:         func(t *testing.T) string { return filepath.Join(t.TempDir(), "not_exist_key") },
			wantErrContains: "unable to read private key",
		},
		{
			name: "broken private key",
			keyPath: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "broken_key")
				if err := os.WriteFile(path, []byte("not a private key"), 0600); err != nil {
					t.Fatalf("failed write key: %v", err)
				}
				return path
			},
			wantErrContains: "unable to parse private key",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			_, err := GenSSHClientConfig(SSHClientOption{
				HostPort:       "bastion.example.com:22",
				Username:       "mogura",
				KeyPath:        c.keyPath(t),
				KnownHostsPath: knownHostsPath,
			})

			// Assert
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantErrContains) {
				t.Errorf("want error containing %q, got: %v", c.wantErrContains, err)
			}
		})
	}
}
