// Package netlist parses lists of IP addresses and CIDR ranges. It has no
// dependencies, so the inbound listener and the readiness checks share one
// parser and cannot disagree about what a setting means.
package netlist

import (
	"fmt"
	"net"
	"strings"
)

// Parse parses a comma-separated list of IPs and CIDR ranges
// (INBOUND_ALLOWED_SOURCES). A bare address is a single-host range.
func Parse(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			if ip := net.ParseIP(f); ip == nil {
				return nil, fmt.Errorf("invalid source address %q", f)
			} else if ip.To4() != nil {
				f += "/32"
			} else {
				f += "/128"
			}
		}
		_, n, err := net.ParseCIDR(f)
		if err != nil {
			return nil, fmt.Errorf("invalid source range %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

// Contains reports whether any range contains ip.
func Contains(nets []*net.IPNet, ip net.IP) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
