package mogura

import (
	"fmt"
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

func (d *DNSClient) QueryA(domain string) ([]*dns.A, error) {
	dnsMsg, err := d.Query(domain, "A")
	if err != nil {
		return nil, err
	}

	records := make([]*dns.A, 0, len(dnsMsg.Answer))
	for _, ans := range dnsMsg.Answer {
		a := ans.(*dns.A)
		records = append(records, a)
	}

	return records, nil
}

func (d *DNSClient) QuerySRV(domain string) ([]*dns.SRV, error) {
	dnsMsg, err := d.Query(domain, "SRV")
	if err != nil {
		return nil, err
	}

	records := make([]*dns.SRV, 0, len(dnsMsg.Answer))
	for _, ans := range dnsMsg.Answer {
		srv := ans.(*dns.SRV)
		records = append(records, srv)
	}

	return records, nil
}
