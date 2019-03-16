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

	resolvedTarget string
	resolvedPort   string
	ttl            int
}

func (t *Target) Validate() error {
	if t.Target == "" {
		return fmt.Errorf("target is required.")
	}

	switch t.TargetType {
	case "SRV":
		if t.TargetPort != 0 {
			return fmt.Errorf("target port is specifeid, however target type SRV.")
		}
	case "HOST-IP":
		fallthrough
	default:
		if t.TargetPort == 0 {
			return fmt.Errorf("target port is require.")
		}
	}

	return nil
}

func (t *Target) Resolve(conn *ssh.Client, resolver string) error {
	switch t.TargetType {
	case "SRV":
		client := NewDNSClient(conn, resolver)
		srvs, err := client.QuerySRV(t.Target)
		if err != nil {
			return fmt.Errorf("failed SRV query to remote DNS: %v", err)
		}
		if len(srvs) == 0 {
			return fmt.Errorf("no answer %s", t.Target)
		}

		// Why do not auto detect AWS ECS ServiceDiscovery A record...?
		// detect A record by myself.
		targets, err := client.QueryA(srvs[0].Target)
		if err != nil {
			return fmt.Errorf("failed %s A query to remote DNS: %v", srvs[0].Target, err)
		}

		if len(targets) == 0 {
			return fmt.Errorf("%s answer is empty.", srvs[0].Target)
		}

		// TODO if priority are same, then shuffle
		// TODO fix logging...
		log.Printf("resolved SRV record %s => %s", t.Target, srvs[0].TargetPort())
		log.Printf("resolved A record %s => %s", srvs[0].Target, targets[0].Target)
		t.resolvedTarget = targets[0].Target
		t.resolvedPort = srvs[0].Port

		// TODO start goroutine that resolve again after ttl
		return nil
	case "HOST-PORT":
		fallthrough
	default:
		// default Host and Port
		t.resolvedTarget = t.Target
		t.resolvedPort = strconv.Itoa(t.TargetPort)

		return nil
	}
}

func (t *Target) ResolvedTargetAndPort() string {
	return t.resolvedTarget + ":" + t.resolvedPort
}
