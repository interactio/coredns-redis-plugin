package redis

import (
	"fmt"
	"github.com/patrickmn/go-cache"
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
	cacheKey := trimmedName

	qtype := state.Type()

	var record *Record

	rec, found := redis.Cache.Get(cacheKey)
	if found {
		fmt.Println("Cache hit: ", cacheKey)

		record = rec.(*Record)

	} else {
		fmt.Println("Cache miss: ", cacheKey)

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
			return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
		}

		location := redis.findLocation(trimmedName, z)
		if len(location) == 0 { // empty, no results
			fmt.Println("LOCATION ERR [0], NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z, "location: ", location)
			return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
		}

		record = redis.get(location, z)
		if record == nil {
			fmt.Println("RECORD ERR [nil], NextOrFailure. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", zone, "z: ", z, "location: ", location, "record: ", record)
			return plugin.NextOrFailure(qname, redis.Next, ctx, w, r)
		}

		record.z = z

		redis.Cache.Set(cacheKey, record, cache.DefaultExpiration)
	}

	answers := make([]dns.RR, 0, 10)
	extras := make([]dns.RR, 0, 10)

	switch qtype {
	case "A":
		answers, extras = redis.A(qname, record.z, record)
	case "AAAA":
		answers, extras = redis.AAAA(qname, record.z, record)
	case "CNAME":
		answers, extras = redis.CNAME(qname, record.z, record)
	case "TXT":
		answers, extras = redis.TXT(qname, record.z, record)
	case "NS":
		answers, extras = redis.NS(qname, record.z, record)
	case "MX":
		answers, extras = redis.MX(qname, record.z, record)
	case "SRV":
		answers, extras = redis.SRV(qname, record.z, record)
	case "SOA":
		answers, extras = redis.SOA(qname, record.z, record)
	case "CAA":
		answers, extras = redis.CAA(qname, record.z, record)

	default:
		fmt.Println("DEFAULT CASE, NOERROR. ", "qname: ", qname, "qtype: ", qtype, "trimmedName: ", trimmedName, "zone: ", "record: ", record)
	}

	m := new(dns.Msg)

	m.Authoritative, m.RecursionAvailable, m.Compress = true, false, true

	m.Answer = append(m.Answer, answers...)
	m.Extra = append(m.Extra, extras...)

	state.SizeAndDo(m)
	m = state.Scrub(m)

	m.SetReply(r)
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
