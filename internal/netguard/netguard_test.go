package netguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	allowed, err := ParseNetworks([]string{"10.20.0.0/16", "192.168.5.7"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ip      string
		allowed []netip.Prefix
		ok      bool
	}{
		{"8.8.8.8", nil, true},
		{"2606:4700::1111", nil, true},
		{"127.0.0.1", nil, false},
		{"::1", nil, false},
		{"10.0.0.5", nil, false},
		{"172.16.3.4", nil, false},
		{"192.168.1.1", nil, false},
		{"169.254.169.254", nil, false}, // cloud metadata
		{"100.64.1.1", nil, false},
		{"0.0.0.0", nil, false},
		{"fd00::1", nil, false},
		{"fe80::1", nil, false},
		{"::ffff:10.0.0.1", nil, false}, // IPv4 written as IPv6
		{"10.20.1.1", allowed, true},
		{"192.168.5.7", allowed, true},
		{"192.168.5.8", allowed, false},
		{"10.21.0.1", allowed, false},
	}
	for _, c := range cases {
		err := Check(netip.MustParseAddr(c.ip), c.allowed)
		if (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok=%v", c.ip, err, c.ok)
		}
	}
	if _, err := ParseNetworks([]string{"not-a-network"}); err == nil {
		t.Error("a bad network was accepted")
	}
}

func TestClientRefusesInternalServersAndRedirectsToThem(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer internal.Close()
	get := func(c *http.Client, u string) error {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", u, nil)
		res, err := c.Do(req)
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	if err := get(Client(2*time.Second, nil), internal.URL); err == nil {
		t.Fatal("connected to a loopback server")
	}
	// A redirect from an allowed place to a blocked one is stopped too: only 127.0.0.1 is allowed
	// below, and the redirect names 127.0.0.2.
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:"+portOf(internal.URL)+"/", http.StatusFound)
	}))
	defer hop.Close()
	only := func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")} }
	if err := get(Client(2*time.Second, only), hop.URL); err == nil {
		t.Fatal("followed a redirect to a blocked address")
	}
	// Allowed networks work, and are read at each connection.
	all := func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")} }
	if err := get(Client(2*time.Second, all), internal.URL); err != nil {
		t.Fatalf("allowed network refused: %v", err)
	}
}

func portOf(u string) string {
	for i := len(u) - 1; i >= 0; i-- {
		if u[i] == ':' {
			return u[i+1:]
		}
	}
	return ""
}
