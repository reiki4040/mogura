package mogura

import (
	"fmt"
	"github.com/reiki4040/mogura/resolver"
	"golang.org/x/crypto/ssh"
	"strconv"
	"strings"
)

var (
	route53Resolver = resolver.NewRoute53Resolver("ap-northeast-1")
)

type RemoteTarget struct {
	ResolverType string
	Resolver     string
	RemoteName   string
	RemotePort   int
}

func (t RemoteTarget) Resolve(conn *ssh.Client) (string, error) {
	switch t.ResolverType {
	case "DNSViaSSH":
		client := NewDNSClient(conn, t.Resolver)
		srvs, err := client.QuerySRV(t.RemoteName)
		if err != nil {
			return "", err
		}
		if len(srvs) == 0 {
			return "", fmt.Errorf("no answer DNS query")
		}

		return srvs[0].TargetPort(), nil
	case "ROUTE53":
		// TODO resolve private hosted zone via Route53 API (not DNS request)
		splited := strings.Split(t.RemoteName, ":")
		if len(splited) != 2 {
			return "", fmt.Errorf("invalid Route53 type remote_name: %s. format is hostedzoneId:DNSName", t.RemoteName)
		}

		result, err := route53Resolver.Resolve(splited[0], splited[1])
		if err != nil {
			return "", err
		}

		switch result.Type {
		case "A":
			// TODO random get values(records)
			detectedRemote := result.Values[0] + ":" + strconv.Itoa(t.RemotePort)
			return detectedRemote, nil
		case "CNAME":
			// TODO CNAME
			return "", fmt.Errorf("not yet implemented CNAME.")
		case "SRV":
			// TODO SRV
			return "", fmt.Errorf("not yet implemented SRV.")
		default:
			return "", fmt.Errorf("unknown Route53 record type: %s", result.Type)
		}

	case "HOST-PORT":
		fallthrough
	default:
		// default Host and Port
		detectedRemote := t.RemoteName + ":" + strconv.Itoa(t.RemotePort)

		return detectedRemote, nil
	}
}
