package mogura

import (
	"fmt"
	"github.com/reiki4040/mogura/resolver"
	"strconv"
	"strings"
)

var (
	route53Resolver = resolver.NewRoute53Resolver("ap-northeast-1")
)

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
