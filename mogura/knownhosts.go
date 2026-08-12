package mogura

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// probeRemoteAddr is passed to the known_hosts callback when probing recorded
// keys. knownhosts splits host and port from it, so it must be a valid address,
// however the hostname argument takes preference in the lookup.
var probeRemoteAddr = &net.TCPAddr{IP: net.IPv4zero, Port: 22}

// hostKeyCallback builds host key verification from an OpenSSH known_hosts
// file. it returns the callback and the host key algorithms to offer for
// hostport.
//
// unknown hosts are rejected. mogura runs unattended, so there is no way to ask
// the user whether an unrecorded key should be trusted.
func hostKeyCallback(knownHostsPath, hostport string) (ssh.HostKeyCallback, []string, error) {
	if knownHostsPath == "" {
		return nil, nil, fmt.Errorf("known_hosts path is empty")
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("known_hosts file %s does not exist. record the bastion host key with:\n\t%s",
				knownHostsPath, keyscanHint(hostport, knownHostsPath))
		}
		return nil, nil, fmt.Errorf("unable to read known_hosts file %s: %v", knownHostsPath, err)
	}

	verify := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := callback(hostname, remote, key); err != nil {
			return describeHostKeyError(err, hostport, knownHostsPath, key)
		}

		return nil
	}

	return verify, hostKeyAlgorithms(callback, hostport), nil
}

// hostKeyAlgorithms returns the host key algorithms recorded for hostport, or
// nil to leave the ssh package default.
//
// this is not cosmetic. the default order prefers rsa-sha2-* while OpenSSH
// records the ed25519 key, so a bastion serving both would present an RSA key
// that known_hosts has no line for, and verification would fail as a key
// mismatch even though nothing is wrong.
func hostKeyAlgorithms(callback ssh.HostKeyCallback, hostport string) []string {
	// probe with a key that can never match: the resulting KeyError carries
	// every key recorded for hostport.
	err := callback(hostport, probeRemoteAddr, probePublicKey{})

	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) || len(keyErr.Want) == 0 {
		// host is not recorded, or known_hosts can not be looked up. the
		// verification itself reports what is wrong.
		return nil
	}

	algos := make([]string, 0, len(keyErr.Want))
	for _, want := range keyErr.Want {
		for _, algo := range keyTypeAlgorithms(want.Key.Type()) {
			if !slices.Contains(algos, algo) {
				algos = append(algos, algo)
			}
		}
	}

	return algos
}

// keyTypeAlgorithms expands a key type recorded in known_hosts into the public
// key algorithms that may carry it. the certificate variants are included
// because a @cert-authority line records the plain CA key type while the
// bastion presents a certificate.
func keyTypeAlgorithms(keyType string) []string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return []string{
			ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA,
			ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01,
		}
	case ssh.KeyAlgoED25519:
		return []string{ssh.KeyAlgoED25519, ssh.CertAlgoED25519v01}
	case ssh.KeyAlgoECDSA256:
		return []string{ssh.KeyAlgoECDSA256, ssh.CertAlgoECDSA256v01}
	case ssh.KeyAlgoECDSA384:
		return []string{ssh.KeyAlgoECDSA384, ssh.CertAlgoECDSA384v01}
	case ssh.KeyAlgoECDSA521:
		return []string{ssh.KeyAlgoECDSA521, ssh.CertAlgoECDSA521v01}
	case ssh.KeyAlgoSKED25519:
		return []string{ssh.KeyAlgoSKED25519, ssh.CertAlgoSKED25519v01}
	case ssh.KeyAlgoSKECDSA256:
		return []string{ssh.KeyAlgoSKECDSA256, ssh.CertAlgoSKECDSA256v01}
	default:
		return []string{keyType}
	}
}

// describeHostKeyError turns a knownhosts error into a message that tells the
// user what happened and how to resolve it.
func describeHostKeyError(err error, hostport, knownHostsPath string, presented ssh.PublicKey) error {
	var revokedErr *knownhosts.RevokedError
	if errors.As(err, &revokedErr) {
		return fmt.Errorf("host key of %s is revoked in %s: %s", hostport, knownHostsPath, revokedErr.Revoked.String())
	}

	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return fmt.Errorf("host key verification of %s failed: %v", hostport, err)
	}

	if len(keyErr.Want) == 0 {
		return fmt.Errorf("host key of %s is not in %s. mogura does not connect to unknown hosts. check the fingerprint %s, then record it with:\n\t%s",
			hostport, knownHostsPath, ssh.FingerprintSHA256(presented), keyscanHint(hostport, knownHostsPath))
	}

	return fmt.Errorf("host key of %s does not match %s. it may be a man-in-the-middle attack. presented key is %s. if the bastion host key was changed for a known reason, then remove the recorded key with:\n\tssh-keygen -f %s -R %s",
		hostport, knownHostsPath, ssh.FingerprintSHA256(presented), knownHostsPath, knownhosts.Normalize(hostport))
}

// keyscanHint builds the ssh-keyscan command that records hostport.
func keyscanHint(hostport, knownHostsPath string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return fmt.Sprintf("ssh-keyscan %s >> %s", hostport, knownHostsPath)
	}

	if port == "22" {
		return fmt.Sprintf("ssh-keyscan %s >> %s", host, knownHostsPath)
	}

	return fmt.Sprintf("ssh-keyscan -p %s %s >> %s", port, host, knownHostsPath)
}

// probePublicKey is a key that matches nothing. it only exists to make
// knownhosts report the keys it has recorded for a host.
type probePublicKey struct{}

func (probePublicKey) Type() string {
	return ssh.KeyAlgoED25519
}

func (probePublicKey) Marshal() []byte {
	return []byte("mogura known_hosts probe")
}

func (probePublicKey) Verify([]byte, *ssh.Signature) error {
	return errors.New("probe key can not verify a signature")
}
