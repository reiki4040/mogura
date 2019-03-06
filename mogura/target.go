package mogura

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"log"
	"strconv"
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
		// TODO fix logging...
		log.Printf("resolved SRV record %s => %s", t.Target, srvs[0].TargetPort())
		return srvs[0].TargetPort(), nil
	case "HOST-PORT":
		fallthrough
	default:
		// default Host and Port
		detectedRemote := t.Target + ":" + strconv.Itoa(t.TargetPort)

		return detectedRemote, nil
	}
}
