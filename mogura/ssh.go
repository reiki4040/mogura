package mogura

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

type SSHClientOption struct {
	HostPort   string
	Username   string
	KeyPath    string
	Passphrase string

	KnownHostsPath string
	// InsecureIgnoreHostKey disables host key verification. it makes the
	// connection vulnerable to man-in-the-middle attacks, so it must stay
	// opt-in.
	InsecureIgnoreHostKey bool
}

func GenSSHClientConfig(o SSHClientOption) (*ssh.ClientConfig, error) {
	key, err := os.ReadFile(o.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key: %v", err)
	}

	var signer ssh.Signer
	if o.Passphrase == "" {
		signer, err = ssh.ParsePrivateKey(key)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(o.Passphrase))
	}
	// Create the Signer for this private key.
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %v", err)
	}

	// Create sshClientConfig
	sshConfig := &ssh.ClientConfig{
		User: o.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
	}

	if o.InsecureIgnoreHostKey {
		sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

		return sshConfig, nil
	}

	verify, algos, err := hostKeyCallback(o.KnownHostsPath, o.HostPort)
	if err != nil {
		return nil, err
	}
	sshConfig.HostKeyCallback = verify
	sshConfig.HostKeyAlgorithms = algos

	return sshConfig, nil
}
