package redis

import (
	"fmt"
	"strconv"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	caddy.RegisterPlugin("redis", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func setup(c *caddy.Controller) error {
	r, err := redisParse(c)
	if err != nil {
		fmt.Println("setup redis parse error: ", err)
		return plugin.Error("redis", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		r.Next = next
		return r
	})

	return nil
}

func redisParse(c *caddy.Controller) (*Redis, error) {
	redis := Redis{
		keyPrefix: "",
		keySuffix: "",
		Ttl:       300,
	}
	var (
		err error
	)

	for c.Next() {
		if c.NextBlock() {
			for {
				switch c.Val() {
				case "address":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.redisAddress = c.Val()
				case "password":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.redisPassword = c.Val()
				case "prefix":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.keyPrefix = c.Val()
				case "suffix":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.keySuffix = c.Val()
				case "connect_timeout":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.connectTimeout, err = strconv.Atoi(c.Val())
					if err != nil {
						fmt.Println("connect_timeout err: ", err)
						redis.connectTimeout = 0
					}
				case "read_timeout":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.readTimeout, err = strconv.Atoi(c.Val())
					if err != nil {
						fmt.Println("read_timeout err: ", err)
						redis.readTimeout = 0
					}
				case "ttl":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					var val int
					val, err = strconv.Atoi(c.Val())
					if err != nil {
						fmt.Println("ttl err", err)
						val = defaultTtl
					}
					redis.Ttl = uint32(val)
				case "use_tls":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.useTls, err = strconv.ParseBool(c.Val())
					if err != nil {
						fmt.Println("use_tls err", err)
						redis.useTls = true
					}
				case "tls_skip_verify":
					if !c.NextArg() {
						return &Redis{}, c.ArgErr()
					}
					redis.tlsSkipVerify, err = strconv.ParseBool(c.Val())
					if err != nil {
						fmt.Println("skipverify err", err)
						redis.tlsSkipVerify = true
					}
				default:
					if c.Val() != "}" {
						fmt.Println("unknown property: ", c.Val())
						return &Redis{}, c.Errf("unknown property '%s'", c.Val())
					}
				}

				if !c.Next() {
					break
				}
			}

		}

		redis.Connect()
		redis.LoadZones()

		return &redis, nil
	}
	return &Redis{}, nil
}
