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

type Target struct {
	TargetType string
	Target     string
	TargetPort int
}

func (t Target) Resolve(conn *ssh.Client, resolver string) (string, error) {
	switch t.TargetType {
	case "SRV":
		client := NewDNSClient(conn, resolver)
		srvs, err := client.QuerySRV(t.Target)
		if err != nil {
			return "", fmt.Errorf("failed SRV query to remote DNS: %v", err)
		}
		if len(srvs) == 0 {
			return "", fmt.Errorf("no answer %s", t.Target)
		}

		// TODO if priority are same, then shuffle
		return srvs[0].TargetPort(), nil
	case "ROUTE53":
		// TODO resolve private hosted zone via Route53 API (not DNS request)
		splited := strings.Split(t.Target, ":")
		if len(splited) != 2 {
			return "", fmt.Errorf("invalid Route53 type remote_name: %s. format is hostedzoneId:DNSName", t.Target)
		}

		result, err := route53Resolver.Resolve(splited[0], splited[1])
		if err != nil {
			return "", err
		}

		switch result.Type {
		case "A":
			// TODO random get values(records)
			detectedRemote := result.Values[0] + ":" + strconv.Itoa(t.TargetPort)
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
		detectedRemote := t.Target + ":" + strconv.Itoa(t.TargetPort)

		return detectedRemote, nil
	}
}
