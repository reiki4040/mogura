package mogura

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/reiki4040/dns"
	"golang.org/x/crypto/ssh"
)

func NewDNSClient(conn *ssh.Client, remoteDNS string) *DNSClient {
	return &DNSClient{
		sshClientConn: conn,
		remoteDNS:     remoteDNS,
	}
}

type DNSClient struct {
	sshClientConn *ssh.Client
	remoteDNS     string
}

func (d *DNSClient) QuerySRV(domain string) ([]SRV, error) {
	co := new(dns.Conn)
	co.ForceTCP = true
	var err error
	if co.Conn, err = d.sshClientConn.Dial("tcp4", d.remoteDNS); err != nil {
		return nil, err
	}
	defer co.Close()

	m := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Authoritative:     false,
			AuthenticatedData: false,
			CheckingDisabled:  false,
			RecursionDesired:  true,
			Opcode:            dns.OpcodeQuery,
		},
		Question: make([]dns.Question, 1),
	}

	m.Question[0] = dns.Question{
		Name:   dns.Fqdn(domain),
		Qtype:  dns.TypeSRV,
		Qclass: uint16(dns.ClassINET),
	}

	co.SetReadDeadline(time.Now().Add(2 * time.Second))
	co.SetWriteDeadline(time.Now().Add(2 * time.Second))

	if err := co.WriteMsg(m); err != nil {
		return nil, fmt.Errorf("dns write error: %v", err)
	}

	r, err := co.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("dns read error: %v", err)
	}

	records := make([]SRV, 0, len(r.Answer))
	for _, ans := range r.Answer {
		srv, err := ParseSRV(ans.String())
		if err != nil {
			return nil, err
		}

		records = append(records, srv)
	}
	return records, nil
}

type SRV struct {
	Priority int
	Weight   int
	Port     string
	Target   string
}

func (s SRV) TargetPort() string {
	return s.Target + ":" + s.Port
}

func ParseSRV(raw string) (SRV, error) {
	tabSplited := strings.Split(raw, "\t")
	if len(tabSplited) != 5 {
		return SRV{}, fmt.Errorf("invalid format DNS Answer returned: %s", raw)
	}
	rawSRV := tabSplited[4]

	items := strings.Split(rawSRV, " ")
	if len(items) != 4 {
		return SRV{}, fmt.Errorf("invalid format SRV Record returned: %s", rawSRV)
	}
	priority, err := strconv.Atoi(items[0])
	if err != nil {
		return SRV{}, fmt.Errorf("not numeric SRV Priority: %s", items[0])
	}
	weight, err := strconv.Atoi(items[1])
	if err != nil {
		return SRV{}, fmt.Errorf("not numeric SRV Weight: %s", items[1])
	}
	port := items[2]
	target := items[3]

	return SRV{
		Priority: priority,
		Weight:   weight,
		Port:     port,
		Target:   target,
	}, nil
}
