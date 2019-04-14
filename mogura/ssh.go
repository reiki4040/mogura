package mogura

import (
	"fmt"
	"io/ioutil"

	"golang.org/x/crypto/ssh"
)

func GenSSHClientConfig(hostport, username, keyPath, passphrase string) (*ssh.ClientConfig, error) {
	key, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key: %v", err)
	}

	var signer ssh.Signer
	if passphrase == "" {
		signer, err = ssh.ParsePrivateKey(key)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	}
	// Create the Signer for this private key.
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %v", err)
	}

	// Create sshClientConfig
	sshConfig := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		//HostKeyCallback: mogura.FixedHostKey(hostKey),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	return sshConfig, nil
}
