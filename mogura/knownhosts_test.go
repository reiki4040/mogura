package mogura

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newTestHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed generate ed25519 key: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed convert to ssh public key: %v", err)
	}

	return sshPub
}

func newTestRSAHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed generate rsa key: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("failed convert to ssh public key: %v", err)
	}

	return sshPub
}

// writeKnownHosts writes a known_hosts file that records key for hostport,
// and returns its path.
func writeKnownHosts(t *testing.T, hostport string, key ssh.PublicKey) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(hostport)}, key)
	if err := os.WriteFile(path, []byte(line+"\n"), 0600); err != nil {
		t.Fatalf("failed write known_hosts: %v", err)
	}

	return path
}

func testRemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
}

func TestHostKeyCallbackVerify(t *testing.T) {
	recordedKey := newTestHostKey(t)
	otherKey := newTestHostKey(t)

	cases := []struct {
		name string
		// recorded is the host recorded in known_hosts.
		recorded string
		// hostport is the bastion mogura connects to.
		hostport string
		// presented is the key the bastion presents in the handshake.
		presented   ssh.PublicKey
		wantErr     bool
		wantErrHint string
	}{
		{
			name:      "recorded host presents the recorded key",
			recorded:  "bastion.example.com:22",
			hostport:  "bastion.example.com:22",
			presented: recordedKey,
			wantErr:   false,
		},
		{
			name:      "recorded host on a non standard port",
			recorded:  "localhost:2222",
			hostport:  "localhost:2222",
			presented: recordedKey,
			wantErr:   false,
		},
		{
			name:        "recorded host presents a different key",
			recorded:    "bastion.example.com:22",
			hostport:    "bastion.example.com:22",
			presented:   otherKey,
			wantErr:     true,
			wantErrHint: "ssh-keygen",
		},
		{
			name:        "unknown host is rejected",
			recorded:    "other.example.com:22",
			hostport:    "bastion.example.com:22",
			presented:   recordedKey,
			wantErr:     true,
			wantErrHint: "ssh-keyscan",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			knownHostsPath := writeKnownHosts(t, c.recorded, recordedKey)

			verify, _, err := hostKeyCallback(knownHostsPath, c.hostport)
			if err != nil {
				t.Fatalf("hostKeyCallback failed: %v", err)
			}

			// Act
			err = verify(c.hostport, testRemoteAddr(), c.presented)

			// Assert
			if !c.wantErr {
				if err != nil {
					t.Fatalf("want verified, got error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("want verification error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantErrHint) {
				t.Errorf("want error containing %q for recovery, got: %v", c.wantErrHint, err)
			}
		})
	}
}

func TestHostKeyCallbackKnownHostsFile(t *testing.T) {
	cases := []struct {
		name            string
		knownHostsPath  func(t *testing.T) string
		wantErrContains string
	}{
		{
			name: "missing known_hosts file",
			knownHostsPath: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "not_exist_known_hosts")
			},
			wantErrContains: "ssh-keyscan",
		},
		{
			name: "empty known_hosts path",
			knownHostsPath: func(t *testing.T) string {
				return ""
			},
			wantErrContains: "known_hosts path is empty",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			path := c.knownHostsPath(t)

			// Act
			_, _, err := hostKeyCallback(path, "bastion.example.com:22")

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

// TestHostKeyAlgorithmsFromKnownHosts guards the case that makes verification
// unusable in practice: the ssh package default prefers rsa-sha2-*, so a
// bastion serving several host key types would present a key that known_hosts
// has no line for.
func TestHostKeyAlgorithmsFromKnownHosts(t *testing.T) {
	const hostport = "bastion.example.com:22"

	cases := []struct {
		name        string
		recorded    string
		hostKey     func(t *testing.T) ssh.PublicKey
		wantAlgos   []string
		unwantAlgos []string
	}{
		{
			name:     "ed25519 only",
			recorded: hostport,
			hostKey:  newTestHostKey,
			wantAlgos: []string{
				ssh.KeyAlgoED25519,
				ssh.CertAlgoED25519v01,
			},
			unwantAlgos: []string{
				ssh.KeyAlgoRSASHA256,
				ssh.KeyAlgoRSASHA512,
				ssh.KeyAlgoRSA,
			},
		},
		{
			name:     "rsa expands to the sha2 algorithms",
			recorded: hostport,
			hostKey:  newTestRSAHostKey,
			wantAlgos: []string{
				ssh.KeyAlgoRSASHA512,
				ssh.KeyAlgoRSASHA256,
				ssh.KeyAlgoRSA,
			},
			unwantAlgos: []string{
				ssh.KeyAlgoED25519,
			},
		},
		{
			name:      "unknown host leaves the default order",
			recorded:  "other.example.com:22",
			hostKey:   newTestHostKey,
			wantAlgos: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			knownHostsPath := writeKnownHosts(t, c.recorded, c.hostKey(t))

			// Act
			_, algos, err := hostKeyCallback(knownHostsPath, hostport)
			if err != nil {
				t.Fatalf("hostKeyCallback failed: %v", err)
			}

			// Assert
			if c.wantAlgos == nil {
				if algos != nil {
					t.Fatalf("want default order(nil), got %v", algos)
				}
				return
			}

			for _, want := range c.wantAlgos {
				if !slices.Contains(algos, want) {
					t.Errorf("want %s offered, got %v", want, algos)
				}
			}
			for _, unwant := range c.unwantAlgos {
				if slices.Contains(algos, unwant) {
					t.Errorf("want %s not offered, got %v", unwant, algos)
				}
			}
		})
	}
}

func TestKeyTypeAlgorithms(t *testing.T) {
	cases := []struct {
		name    string
		keyType string
		want    []string
	}{
		{
			name:    "rsa expands to sha2 and certificate algorithms",
			keyType: ssh.KeyAlgoRSA,
			want: []string{
				ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA,
				ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01,
			},
		},
		{
			name:    "ed25519",
			keyType: ssh.KeyAlgoED25519,
			want:    []string{ssh.KeyAlgoED25519, ssh.CertAlgoED25519v01},
		},
		{
			name:    "ecdsa nistp256",
			keyType: ssh.KeyAlgoECDSA256,
			want:    []string{ssh.KeyAlgoECDSA256, ssh.CertAlgoECDSA256v01},
		},
		{
			name:    "ecdsa nistp384",
			keyType: ssh.KeyAlgoECDSA384,
			want:    []string{ssh.KeyAlgoECDSA384, ssh.CertAlgoECDSA384v01},
		},
		{
			name:    "ecdsa nistp521",
			keyType: ssh.KeyAlgoECDSA521,
			want:    []string{ssh.KeyAlgoECDSA521, ssh.CertAlgoECDSA521v01},
		},
		{
			name:    "security key ed25519",
			keyType: ssh.KeyAlgoSKED25519,
			want:    []string{ssh.KeyAlgoSKED25519, ssh.CertAlgoSKED25519v01},
		},
		{
			name:    "security key ecdsa",
			keyType: ssh.KeyAlgoSKECDSA256,
			want:    []string{ssh.KeyAlgoSKECDSA256, ssh.CertAlgoSKECDSA256v01},
		},
		{
			name:    "unknown key type is offered as is",
			keyType: "ssh-dss",
			want:    []string{"ssh-dss"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := keyTypeAlgorithms(c.keyType)
			if !slices.Equal(got, c.want) {
				t.Errorf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestKeyscanHint(t *testing.T) {
	cases := []struct {
		name     string
		hostport string
		want     string
	}{
		{
			name:     "default port is not passed to ssh-keyscan",
			hostport: "bastion.example.com:22",
			want:     "ssh-keyscan bastion.example.com >> /home/user/.ssh/known_hosts",
		},
		{
			name:     "non default port",
			hostport: "localhost:2222",
			want:     "ssh-keyscan -p 2222 localhost >> /home/user/.ssh/known_hosts",
		},
		{
			name:     "host without port",
			hostport: "bastion.example.com",
			want:     "ssh-keyscan bastion.example.com >> /home/user/.ssh/known_hosts",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := keyscanHint(c.hostport, "/home/user/.ssh/known_hosts")
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}
