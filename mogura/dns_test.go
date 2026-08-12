package mogura

import (
	"fmt"
	"slices"
	"testing"

	"github.com/miekg/dns"
)

func newTestAnswer(t *testing.T, records ...string) *dns.Msg {
	t.Helper()

	msg := &dns.Msg{}
	for _, record := range records {
		rr, err := dns.NewRR(record)
		if err != nil {
			t.Fatalf("failed parse record %q: %v", record, err)
		}
		msg.Answer = append(msg.Answer, rr)
	}

	return msg
}

// aTargets, cnameTargets and srvTargets pick one field of the records that
// answersOf returned, so a table can compare them as plain strings.
func aTargets(msg *dns.Msg) []string {
	targets := []string{}
	for _, record := range answersOf[*dns.A](msg) {
		targets = append(targets, record.A.String())
	}

	return targets
}

func cnameTargets(msg *dns.Msg) []string {
	targets := []string{}
	for _, record := range answersOf[*dns.CNAME](msg) {
		targets = append(targets, record.Target)
	}

	return targets
}

func srvTargets(msg *dns.Msg) []string {
	targets := []string{}
	for _, record := range answersOf[*dns.SRV](msg) {
		targets = append(targets, fmt.Sprintf("%s:%d", record.Target, record.Port))
	}

	return targets
}

func TestAnswersOf(t *testing.T) {
	cases := []struct {
		name    string
		answer  []string
		extract func(*dns.Msg) []string
		want    []string
	}{
		{
			// a resolver answers an A query for a name that is a CNAME with the
			// whole chain. reading every record as an A crashed mogura here.
			name: "A answer that carries the CNAME of the chain",
			answer: []string{
				"db.internal. 60 IN CNAME real.internal.",
				"real.internal. 60 IN A 10.0.0.5",
			},
			extract: aTargets,
			want:    []string{"10.0.0.5"},
		},
		{
			name: "A answer without any A record",
			answer: []string{
				"db.internal. 60 IN CNAME real.internal.",
			},
			extract: aTargets,
			want:    []string{},
		},
		{
			name: "several A records keep their order",
			answer: []string{
				"real.internal. 60 IN A 10.0.0.5",
				"real.internal. 60 IN A 10.0.0.6",
			},
			extract: aTargets,
			want:    []string{"10.0.0.5", "10.0.0.6"},
		},
		{
			name: "CNAME answer that carries the resolved A record",
			answer: []string{
				"db.internal. 60 IN CNAME real.internal.",
				"real.internal. 60 IN A 10.0.0.5",
			},
			extract: cnameTargets,
			want:    []string{"real.internal."},
		},
		{
			name: "SRV answer that carries the A record of its target",
			answer: []string{
				"_grpclb._tcp.service.internal. 60 IN SRV 1 1 50051 node.internal.",
				"node.internal. 60 IN A 10.0.0.7",
			},
			extract: srvTargets,
			want:    []string{"node.internal.:50051"},
		},
		{
			name:    "empty answer",
			answer:  nil,
			extract: aTargets,
			want:    []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			msg := newTestAnswer(t, c.answer...)

			// Act
			got := c.extract(msg)

			// Assert
			if !slices.Equal(got, c.want) {
				t.Errorf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestAnswersOfReturnsEmptySliceNotNil(t *testing.T) {
	// Arrange
	msg := newTestAnswer(t, "db.internal. 60 IN CNAME real.internal.")

	// Act
	got := answersOf[*dns.A](msg)

	// Assert
	if got == nil {
		t.Fatal("want an empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("want no record, got %v", got)
	}
}

func TestNewQueryMsg(t *testing.T) {
	cases := []struct {
		name      string
		domain    string
		qtype     uint16
		wantName  string
		wantQtype uint16
	}{
		{
			name:      "A query",
			domain:    "db.internal",
			qtype:     dns.TypeA,
			wantName:  "db.internal.",
			wantQtype: dns.TypeA,
		},
		{
			// QueryCNAME used to send an A query, because the type came from a
			// string with an implicit default.
			name:      "CNAME query does not fall back to A",
			domain:    "db.internal",
			qtype:     dns.TypeCNAME,
			wantName:  "db.internal.",
			wantQtype: dns.TypeCNAME,
		},
		{
			name:      "SRV query",
			domain:    "_grpclb._tcp.service.internal",
			qtype:     dns.TypeSRV,
			wantName:  "_grpclb._tcp.service.internal.",
			wantQtype: dns.TypeSRV,
		},
		{
			name:      "a name that is already fully qualified is kept",
			domain:    "db.internal.",
			qtype:     dns.TypeA,
			wantName:  "db.internal.",
			wantQtype: dns.TypeA,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			msg := newQueryMsg(c.domain, c.qtype)

			// Assert
			if len(msg.Question) != 1 {
				t.Fatalf("want 1 question, got %d", len(msg.Question))
			}

			question := msg.Question[0]
			if question.Name != c.wantName {
				t.Errorf("want name %q, got %q", c.wantName, question.Name)
			}
			if question.Qtype != c.wantQtype {
				t.Errorf("want qtype %s, got %s", dns.TypeToString[c.wantQtype], dns.TypeToString[question.Qtype])
			}
			if question.Qclass != dns.ClassINET {
				t.Errorf("want class INET, got %d", question.Qclass)
			}
			if !msg.RecursionDesired {
				t.Error("want recursion desired, the remote DNS has to follow the chain")
			}
			if msg.Opcode != dns.OpcodeQuery {
				t.Errorf("want opcode query, got %d", msg.Opcode)
			}
		})
	}
}
