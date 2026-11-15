// Package netguard keeps connections to addresses that someone else chose (an identity
// provider's discovery document, for one) from reaching this server's own network:
// loopback, private ranges, link-local addresses (cloud metadata endpoints among them)
// and the like.
//
// The check runs on the address actually dialled, after DNS resolution and for every
// connection, including those made for redirects, so a hostname that resolves to a
// private address, a DNS answer that changes between checking and connecting, and a
// redirect to an internal URL are all stopped the same way.
package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var ErrBlocked = errors.New("destination is a private or reserved address")

// nonPublic are the ranges that are never public. 0.0.0.0 reaches the local host on most systems.
var nonPublic = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b:1::/48", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// ParseNetworks reads a list of CIDRs (or single addresses) that may be reached though they are not public.
func ParseNetworks(items []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, it := range items {
		if it == "" {
			continue
		}
		if p, err := netip.ParsePrefix(it); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(it)
		if err != nil {
			return nil, errors.New("not an address or network: " + it)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// Check refuses a non-public address unless it lies inside one of the allowed networks.
func Check(ip netip.Addr, allowed []netip.Prefix) error {
	// An IPv4 address written as IPv6 (::ffff:10.0.0.1) is judged as IPv4.
	ip = ip.Unmap()
	for _, a := range allowed {
		if a.Contains(ip) {
			return nil
		}
	}
	for _, b := range nonPublic {
		if b.Contains(ip) {
			return ErrBlocked
		}
	}
	return nil
}

// Client is an HTTP client that only connects to public addresses, plus the allowed networks
// (read at each connection, so a setting can change while the process runs). It follows at
// most three redirects.
func Client(timeout time.Duration, allowed func() []netip.Prefix) *http.Client {
	d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return ErrBlocked
		}
		var nets []netip.Prefix
		if allowed != nil {
			nets = allowed()
		}
		return Check(ap.Addr(), nets)
	}}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         d.DialContext,
			TLSHandshakeTimeout: timeout,
			Proxy:               nil, // a proxy would make the connection on our behalf and bypass the check
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
