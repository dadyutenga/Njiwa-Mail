package preflight

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

type res struct {
	ptr map[string][]string
	a   map[string][]string
	mx  map[string][]string
}

func nf(n string) error { return &net.DNSError{Err: "no such host", Name: n, IsNotFound: true} }

func (r res) LookupAddr(_ context.Context, a string) ([]string, error) {
	if v := r.ptr[a]; v != nil {
		return v, nil
	}
	return nil, nf(a)
}
func (r res) LookupIPAddr(_ context.Context, h string) ([]net.IPAddr, error) {
	var out []net.IPAddr
	for _, s := range r.a[h] {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	if out == nil {
		return nil, nf(h)
	}
	return out, nil
}
func (r res) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	var out []*net.MX
	for _, h := range r.mx[n] {
		out = append(out, &net.MX{Host: h})
	}
	if out == nil {
		return nil, nf(n)
	}
	return out, nil
}

// goodHost is a supported, healthy VPS with correct DNS and open port 25.
func goodHost() (Env, Params) {
	files := map[string]string{
		"/etc/os-release": "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n",
		"/proc/meminfo":   "MemTotal: 2048000 kB\nMemAvailable: 1500000 kB\n",
		// postgres on 127.0.0.1:5432, API on 127.0.0.1:8080, smtp on 0.0.0.0:25
		"/proc/net/tcp": "  sl  local_address rem_address   st\n   0: 0100007F:1538 00000000:0000 0A\n   1: 0100007F:1F90 00000000:0000 0A\n   2: 00000000:0019 00000000:0000 0A\n",
	}
	exists := map[string]bool{"/run/systemd/system": true, "/run/systemd/timesync/synchronized": true}
	env := Env{
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		Stat: func(p string) (os.FileInfo, error) {
			if exists[p] {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
		Statfs:       func(string) (uint64, uint64, error) { return 20 << 30, 1_000_000, nil },
		InterfaceIPs: func() ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("203.0.113.10")}, nil },
		Resolver: res{
			ptr: map[string][]string{"203.0.113.10": {"mailout.example.com."}},
			a:   map[string][]string{"mailout.example.com": {"203.0.113.10"}},
			mx:  map[string][]string{"gmail.com": {"gmail-smtp-in.l.google.com."}},
		},
		DialBanner: func(_ context.Context, addr string) (string, error) {
			if addr == "gmail-smtp-in.l.google.com:25" {
				return "220 mx.google.com ESMTP", nil
			}
			return "", errors.New("connection refused")
		},
	}
	return env, Params{Hostname: "MailOut.Example.com.", IP: "203.0.113.10"}
}

func byKey(cs []Check, k string) Check {
	for _, c := range cs {
		if c.Key == k {
			return c
		}
	}
	return Check{}
}

func TestHealthyHostIsReady(t *testing.T) {
	env, p := goodHost()
	cs := Run(context.Background(), env, p)
	if ok, first := Summary(cs); !ok {
		t.Fatalf("not ready: %+v", *first)
	}
}

func TestEachProblemIsReportedWithCodeAndFix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Env, *Params)
		key    string
		status Status
		code   string
	}{
		{"unsupported OS", func(e *Env, _ *Params) {
			e.ReadFile = func(p string) ([]byte, error) {
				if p == "/etc/os-release" {
					return []byte("ID=centos\nVERSION_ID=\"7\"\nPRETTY_NAME=\"CentOS 7\"\n"), nil
				}
				return []byte("MemAvailable: 900000 kB\n"), nil
			}
		}, "os", NeedsAction, CodeOSUnsupported},
		{"low disk", func(e *Env, _ *Params) {
			e.Statfs = func(string) (uint64, uint64, error) { return 1 << 30, 1_000_000, nil }
		}, "disk", NeedsAction, CodeDiskLow},
		{"port 25 blocked", func(e *Env, _ *Params) {
			e.DialBanner = func(context.Context, string) (string, error) { return "", errors.New("i/o timeout") }
		}, "outbound_25", BlockedExternalAction, CodeEgressBlocked},
		{"PTR missing", func(e *Env, _ *Params) {
			e.Resolver = res{a: map[string][]string{"mailout.example.com": {"203.0.113.10"}}, mx: map[string][]string{"gmail.com": {"gmail-smtp-in.l.google.com."}}}
		}, "ptr", BlockedExternalAction, CodePTRMissing},
		{"PTR names another host", func(e *Env, _ *Params) {
			r := e.Resolver.(res)
			r.ptr = map[string][]string{"203.0.113.10": {"static.provider.net."}}
			e.Resolver = r
		}, "ptr", BlockedExternalAction, CodePTRMismatch},
		{"PTR not forward-confirmed", func(e *Env, _ *Params) {
			r := e.Resolver.(res)
			r.a = map[string][]string{"mailout.example.com": {"198.51.100.1"}}
			e.Resolver = r
		}, "ptr", NeedsAction, CodeFCrDNSFailed},
		{"existing MTA", func(e *Env, _ *Params) {
			e.Stat = func(p string) (os.FileInfo, error) {
				if p == "/usr/sbin/postfix" || p == "/run/systemd/system" || p == "/run/systemd/timesync/synchronized" {
					return nil, nil
				}
				return nil, os.ErrNotExist
			}
		}, "mta_conflict", NeedsAction, CodeMTAConflict},
		{"IP behind NAT", func(e *Env, _ *Params) {
			e.InterfaceIPs = func() ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.5")}, nil }
		}, "sending_ip", CannotConfirm, CodeIPNotLocal},
		{"clock unknown", func(e *Env, _ *Params) {
			e.Stat = func(p string) (os.FileInfo, error) {
				if p == "/run/systemd/system" {
					return nil, nil
				}
				return nil, os.ErrNotExist
			}
		}, "clock", CannotConfirm, CodeClockUnsynced},
		{"missing parameters", func(_ *Env, p *Params) { p.IP = "" }, "parameters", NeedsAction, CodeMissingParameters},
		{"PostgreSQL on all interfaces", func(e *Env, _ *Params) {
			orig := e.ReadFile
			e.ReadFile = func(p string) ([]byte, error) {
				if p == "/proc/net/tcp" {
					// 0.0.0.0:5432 listening, and an IPv6 [::]:8080
					return []byte("  sl  local_address rem_address   st\n   0: 00000000:1538 00000000:0000 0A\n"), nil
				}
				if p == "/proc/net/tcp6" {
					return []byte("  sl  local_address rem_address   st\n   0: 00000000000000000000000000000000:1F90 00000000000000000000000000000000:0000 0A\n"), nil
				}
				return orig(p)
			}
		}, "private_ports", NeedsAction, CodePrivatePortPublic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, p := goodHost()
			c.mutate(&env, &p)
			cs := Run(context.Background(), env, p)
			got := byKey(cs, c.key)
			if got.Status != c.status || got.Code != c.code {
				t.Fatalf("%s: %+v", c.key, got)
			}
			if c.status != Ready && got.Fix == "" && c.status != CannotConfirm {
				t.Fatalf("no fix: %+v", got)
			}
			if ok, _ := Summary(cs); ok {
				t.Fatal("summary must not be ready")
			}
		})
	}
}
