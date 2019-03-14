package mogura

import (
	"fmt"
	"regexp"
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

func (d *DNSClient) Query(domain, queryType string) (*dns.Msg, error) {
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

	qType := dns.TypeA
	switch queryType {
	case "SRV":
		qType = dns.TypeSRV
	}

	m.Question[0] = dns.Question{
		Name:   dns.Fqdn(domain),
		Qtype:  qType,
		Qclass: uint16(dns.ClassINET),
	}

	co.SetReadDeadline(time.Now().Add(2 * time.Second))
	co.SetWriteDeadline(time.Now().Add(2 * time.Second))

	if err := co.WriteMsg(m); err != nil {
		return nil, fmt.Errorf("dns write error: %v", err)
	}

	dnsMsg, err := co.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("dns read error: %v", err)
	}

	return dnsMsg, nil
}

func (d *DNSClient) QueryA(domain string) ([]A, error) {
	dnsMsg, err := d.Query(domain, "A")
	if err != nil {
		return nil, err
	}

	records := make([]A, 0, len(dnsMsg.Answer))
	for _, ans := range dnsMsg.Answer {
		a, err := ParseA(ans.String())
		if err != nil {
			return nil, err
		}

		records = append(records, a)
	}

	return records, nil
}

func (d *DNSClient) QuerySRV(domain string) ([]SRV, error) {
	dnsMsg, err := d.Query(domain, "SRV")
	if err != nil {
		return nil, err
	}

	records := make([]SRV, 0, len(dnsMsg.Answer))
	for _, ans := range dnsMsg.Answer {
		srv, err := ParseSRV(ans.String())
		if err != nil {
			return nil, err
		}

		records = append(records, srv)
	}

	return records, nil
}

type A struct {
	TTL    int
	Target string
}

type SRV struct {
	TTL      int
	Priority int
	Weight   int
	Port     string
	Target   string
}

func (s SRV) TargetPort() string {
	return s.Target + ":" + s.Port
}

func ParseA(raw string) (A, error) {
	whitespace := regexp.MustCompile(`\s+`)
	replaced := whitespace.ReplaceAllString(raw, " ")

	splited := strings.Split(replaced, " ")
	if len(splited) != 5 {
		return A{}, fmt.Errorf("invalid format A record answer returned: %s", raw)
	}

	ttl, err := strconv.Atoi(splited[1])
	if err != nil {
		return A{}, fmt.Errorf("not numeric A ttl: %s", splited[1])
	}

	a := A{
		TTL:    ttl,
		Target: splited[4],
	}

	return a, nil
}

func ParseSRV(raw string) (SRV, error) {
	whitespace := regexp.MustCompile(`\s+`)
	replaced := whitespace.ReplaceAllString(raw, " ")

	splited := strings.Split(replaced, " ")
	if len(splited) != 8 {
		return SRV{}, fmt.Errorf("invalid format SRV record answer returned: %s", raw)
	}

	priority, err := strconv.Atoi(splited[4])
	if err != nil {
		return SRV{}, fmt.Errorf("not numeric SRV Priority: %s", splited[4])
	}
	weight, err := strconv.Atoi(splited[5])
	if err != nil {
		return SRV{}, fmt.Errorf("not numeric SRV Weight: %s", splited[5])
	}
	port := splited[6]
	target := splited[7]

	ttl, err := strconv.Atoi(splited[1])
	if err != nil {
		return SRV{}, fmt.Errorf("not numeric SRV ttl: %s", splited[1])
	}

	return SRV{
		Priority: priority,
		Weight:   weight,
		Port:     port,
		Target:   target,
		TTL:      ttl,
	}, nil
}
