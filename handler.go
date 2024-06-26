package redis

import (
	"fmt"
	"regexp"
	"strings"

	// "fmt"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"golang.org/x/net/context"
)

var pattern = regexp.MustCompile(`-.*`)

// ServeDNS implements the plugin.Handler interface.
func (redis *Redis) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	qname := state.Name()

	parts := strings.Split(qname, ".")
	parts[0] = pattern.ReplaceAllString(parts[0], "")

	trimmedName := strings.Join(parts, ".")

	qtype := state.Type()

	if time.Since(redis.LastZoneUpdate) > zoneUpdateTime {
		redis.LoadZones()
	}

	zone := plugin.Zones(redis.Zones).Matches(qname)
	if zone == "" {
		fmt.Println("zone err [empty] NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone)
		return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
	}

	z := redis.load(zone)
	if z == nil {
		fmt.Println("z err [nil] NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z)
		return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
	}

	if qtype == "AXFR" {
		records := redis.AXFR(z)

		ch := make(chan *dns.Envelope)
		tr := new(dns.Transfer)
		tr.TsigSecret = nil

		go func(ch chan *dns.Envelope) {
			j, l := 0, 0

			for i, r := range records {
				l += dns.Len(r)
				if l > transferLength {
					ch <- &dns.Envelope{RR: records[j:i]}
					l = 0
					j = i
				}
			}
			if j < len(records) {
				ch <- &dns.Envelope{RR: records[j:]}
			}
			close(ch)
		}(ch)

		err := tr.Out(w, r, ch)
		if err != nil {
			fmt.Println(err)
		}
		w.Hijack()
		return dns.RcodeSuccess, nil
	}

	location := redis.findLocation(trimmedName, z)
	if len(location) == 0 { // empty, no results
		fmt.Println("LOCATION ERR [0], NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z, "location: ", location)
		return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
	}

	answers := make([]dns.RR, 0, 10)
	extras := make([]dns.RR, 0, 10)

	record := redis.get(location, z)
	if record == nil {
		fmt.Println("RECORD ERR [nil], NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z, "location: ", location, "record: ", record)
		return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
	}

	switch qtype {
	case "A":
		answers, extras = redis.A(qname, z, record)
	case "AAAA":
		answers, extras = redis.AAAA(qname, z, record)
	case "CNAME":
		answers, extras = redis.CNAME(qname, z, record)
	case "TXT":
		answers, extras = redis.TXT(qname, z, record)
	case "NS":
		answers, extras = redis.NS(qname, z, record)
	case "MX":
		answers, extras = redis.MX(qname, z, record)
	case "SRV":
		answers, extras = redis.SRV(qname, z, record)
	case "SOA":
		answers, extras = redis.SOA(qname, z, record)
	case "CAA":
		answers, extras = redis.CAA(qname, z, record)

	default:
		fmt.Println("DEFAULT CASE, NOERROR. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z, "location: ", location, "record: ", record)
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative, m.RecursionAvailable, m.Compress = true, false, true

	m.Answer = append(m.Answer, answers...)
	m.Extra = append(m.Extra, extras...)

	state.SizeAndDo(m)
	m = state.Scrub(m)
	_ = w.WriteMsg(m)

	return dns.RcodeSuccess, nil
}

// Name implements the Handler interface.
func (redis *Redis) Name() string { return "redis" }

func (redis *Redis) errorResponse(state request.Request, zone string, rcode int, err error) (int, error) {
	m := new(dns.Msg)
	m.SetRcode(state.Req, rcode)
	m.Authoritative, m.RecursionAvailable, m.Compress = true, false, true

	state.SizeAndDo(m)
	_ = state.W.WriteMsg(m)
	// Return success as the rcode to signal we have written to the client.
	return dns.RcodeSuccess, err
}
