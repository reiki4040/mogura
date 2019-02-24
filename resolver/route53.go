package resolver

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

func NewRoute53Resolver(region string) *Route53Resolver {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))

	client := route53.New(sess)
	return &Route53Resolver{
		client: client,
	}
}

type Route53Result struct {
	Name   string
	Type   string
	Values []string
	TTL    int64
}

type Route53Resolver struct {
	client *route53.Route53

	// hostedzoneId -> Name -> result
	cache map[string]map[string]Route53Result
}

func (r *Route53Resolver) Load() error {
	resp, err := r.client.ListHostedZones(&route53.ListHostedZonesInput{})
	if err != nil {
		return fmt.Errorf("failed list hosted zones: %v", err)
	}

	c := make(map[string]map[string]Route53Result)
	for _, z := range resp.HostedZones {
		// only private zone
		if !aws.BoolValue(z.Config.PrivateZone) {
			continue
		}

		in := &route53.ListResourceRecordSetsInput{
			HostedZoneId: z.Id,
		}

		r, err := r.client.ListResourceRecordSets(in)
		if err != nil {
			return fmt.Errorf("failed list hosted zones: %v", err)
		}

		zoneC := make(map[string]Route53Result, len(r.ResourceRecordSets))
		for _, rs := range r.ResourceRecordSets {
			// skip SOA, TXT etc...
			switch aws.StringValue(rs.Type) {
			case "A":
			case "CNAME":
			case "AAAA":
			case "SRV":
			default:
				continue
			}

			values := make([]string, 0, len(rs.ResourceRecords))
			for _, record := range rs.ResourceRecords {
				values = append(values, aws.StringValue(record.Value))
			}

			result := Route53Result{
				Name:   aws.StringValue(rs.Name),
				Type:   aws.StringValue(rs.Type),
				Values: values,
				TTL:    aws.Int64Value(rs.TTL),
			}
			zoneC[result.Name] = result
		}

		c[aws.StringValue(z.Id)] = zoneC
	}

	r.cache = c
	return nil
}

func (r *Route53Resolver) Resolve(zoneId, name string) (Route53Result, error) {
	// TODO force reload for update DNS
	if r.cache == nil {
		err := r.Load()
		if err != nil {
			return Route53Result{}, err
		}
	}

	z, ok := r.cache[zoneId]
	if !ok {
		return Route53Result{}, fmt.Errorf("unknown zone id: %s", zoneId)
	}
	result, ok := z[name]
	if !ok {
		return Route53Result{}, fmt.Errorf("unknown name: %s", name)
	}

	return result, nil
}
