package server

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies parses a comma separated list of IPs and CIDRs.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
			}
			prefixes = append(prefixes, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		prefixes = append(prefixes, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return prefixes, nil
}

// client describes who sent a request, after trusted proxies.
type client struct {
	addr  netip.Addr
	https bool
}

func (s *Server) isTrusted(a netip.Addr) bool {
	for _, p := range s.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientOf returns the client address and scheme. Forwarding headers
// (X-Real-IP, X-Forwarded-For, X-Forwarded-Proto) are only used when the
// direct peer is a trusted proxy.
func (s *Server) clientOf(r *http.Request) client {
	c := client{https: r.TLS != nil}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return c
	}
	c.addr = peer.Unmap()
	if !s.isTrusted(c.addr) {
		return c
	}

	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		c.https = strings.EqualFold(strings.TrimSpace(proto), "https")
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		c.addr = a.Unmap()
		return c
	}
	// rightmost address that is not a trusted proxy
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		c.addr = a.Unmap()
		if !s.isTrusted(c.addr) {
			break
		}
	}
	return c
}

// ClientID identifies a client for rate limits; IPv6 clients are grouped
// by /64 since a single client usually controls a whole /64.
func ClientID(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}
