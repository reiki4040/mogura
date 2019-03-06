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

	return SRV{
		Priority: priority,
		Weight:   weight,
		Port:     port,
		Target:   target,
	}, nil
}
