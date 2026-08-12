package mogura

import (
	"fmt"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/crypto/ssh"
)

const dnsTimeout = 2 * time.Second

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

func (d *DNSClient) Query(domain string, qtype uint16) (*dns.Msg, error) {
	co := new(dns.Conn)
	var err error
	if co.Conn, err = d.sshClientConn.Dial("tcp4", d.remoteDNS); err != nil {
		return nil, err
	}
	defer co.Close()

	co.SetReadDeadline(time.Now().Add(dnsTimeout))
	co.SetWriteDeadline(time.Now().Add(dnsTimeout))

	if err := co.WriteMsg(newQueryMsg(domain, qtype)); err != nil {
		return nil, fmt.Errorf("dns write error: %v", err)
	}

	dnsMsg, err := co.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("dns read error: %v", err)
	}

	// without this, a name that does not exist and a broken resolver both end
	// up as an empty answer, and the tunnel reports the same thing for both.
	if dnsMsg.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("dns query %s failed: %s", domain, dns.RcodeToString[dnsMsg.Rcode])
	}

	return dnsMsg, nil
}

// newQueryMsg builds the query message for domain.
func newQueryMsg(domain string, qtype uint16) *dns.Msg {
	return &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Authoritative:     false,
			AuthenticatedData: false,
			CheckingDisabled:  false,
			RecursionDesired:  true,
			Opcode:            dns.OpcodeQuery,
		},
		Question: []dns.Question{
			{
				Name:   dns.Fqdn(domain),
				Qtype:  qtype,
				Qclass: uint16(dns.ClassINET),
			},
		},
	}
}

// answersOf returns the records of type T in the answer section. an answer
// carries records that were not asked for, the CNAME of a chain for instance,
// and those must not be read as the requested type.
func answersOf[T dns.RR](msg *dns.Msg) []T {
	records := make([]T, 0, len(msg.Answer))
	for _, ans := range msg.Answer {
		if record, ok := ans.(T); ok {
			records = append(records, record)
		}
	}

	return records
}

func (d *DNSClient) QueryA(domain string) ([]*dns.A, error) {
	dnsMsg, err := d.Query(domain, dns.TypeA)
	if err != nil {
		return nil, err
	}

	return answersOf[*dns.A](dnsMsg), nil
}

func (d *DNSClient) QueryCNAME(domain string) ([]*dns.CNAME, error) {
	dnsMsg, err := d.Query(domain, dns.TypeCNAME)
	if err != nil {
		return nil, err
	}

	return answersOf[*dns.CNAME](dnsMsg), nil
}

func (d *DNSClient) QuerySRV(domain string) ([]*dns.SRV, error) {
	dnsMsg, err := d.Query(domain, dns.TypeSRV)
	if err != nil {
		return nil, err
	}

	return answersOf[*dns.SRV](dnsMsg), nil
}
