package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A minimal Mailcow: enough of /api/v1 for the adapter, checking the key header.
type fakeMailcow struct {
	mu         sync.Mutex
	domains    map[string]map[string]any
	dkim       map[string]string
	transports []map[string]any
	calls      []string
}

func (f *fakeMailcow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-API-Key") != "good-key" {
		w.WriteHeader(401)
		return
	}
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	ok := func() { w.Write([]byte(`[{"type":"success","msg":["ok"]}]`)) }
	switch {
	case r.URL.Path == "/api/v1/get/status/version":
		w.Write([]byte(`{"version":"2026-01"}`))
	case strings.HasPrefix(r.URL.Path, "/api/v1/get/domain/"):
		d := strings.TrimPrefix(r.URL.Path, "/api/v1/get/domain/")
		if m, found := f.domains[d]; found {
			json.NewEncoder(w).Encode(m)
		} else {
			w.Write([]byte(`{}`))
		}
	case r.URL.Path == "/api/v1/add/domain":
		f.domains[body["domain"].(string)] = map[string]any{"active": 1, "relay_all_recipients": body["relay_all_recipients"]}
		ok()
	case strings.HasPrefix(r.URL.Path, "/api/v1/get/dkim/"):
		d := strings.TrimPrefix(r.URL.Path, "/api/v1/get/dkim/")
		if t := f.dkim[d]; t != "" {
			json.NewEncoder(w).Encode(map[string]any{"dkim_txt": t})
		} else {
			w.Write([]byte(`[]`))
		}
	case r.URL.Path == "/api/v1/add/dkim":
		f.dkim[body["domains"].(string)] = "v=DKIM1; k=rsa; p=ABCDEF"
		ok()
	case r.URL.Path == "/api/v1/get/transport/all":
		json.NewEncoder(w).Encode(f.transports)
	case r.URL.Path == "/api/v1/add/transport":
		f.transports = append(f.transports, map[string]any{"destination": body["destination"], "nexthop": body["nexthop"], "active": 1})
		ok()
	default:
		w.WriteHeader(404)
	}
}

func TestMailcowAdapterReadsAndWrites(t *testing.T) {
	fm := &fakeMailcow{domains: map[string]map[string]any{}, dkim: map[string]string{}}
	srv := httptest.NewServer(fm)
	defer srv.Close()
	ctx := context.Background()
	p := NewMailcow(srv.URL, "good-key")
	if err := NewMailcow(srv.URL, "bad").Ping(ctx); err == nil || !strings.Contains(err.Error(), "refused the key") {
		t.Fatalf("bad key: %v", err)
	}
	if err := p.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	if st, err := p.DomainState(ctx, "a.example"); err != nil || st["exists"] != false {
		t.Fatalf("absent domain: %v %v", st, err)
	}
	if err := p.AddDomain(ctx, "a.example", true); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.DomainState(ctx, "a.example"); st["exists"] != true || st["active"] != true {
		t.Fatalf("domain state: %v", st)
	}

	if st, err := p.DKIMState(ctx, "a.example"); err != nil || st["present"] != false {
		t.Fatalf("absent key (Mailcow answers []): %v %v", st, err)
	}
	if err := p.AddDKIM(ctx, "a.example", "njiwa"); err != nil {
		t.Fatal(err)
	}
	st, _ := p.DKIMState(ctx, "a.example")
	if st["present"] != true || len(st["fingerprint"].(string)) != 16 {
		t.Fatalf("dkim state: %v", st)
	}
	for k, v := range st {
		if s, ok := v.(string); ok && strings.Contains(s, "ABCDEF") {
			t.Fatalf("the public key itself leaked into state field %s", k)
		}
	}
	if txt, _ := p.DKIMRecord(ctx, "a.example"); !strings.Contains(txt, "p=ABCDEF") {
		t.Fatalf("record: %q", txt)
	}

	if st, _ := p.TransportState(ctx, "in.example"); st["exists"] != false {
		t.Fatalf("absent transport: %v", st)
	}
	if err := p.AddTransport(ctx, "in.example", "[172.22.1.200]:2525"); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.TransportState(ctx, "IN.example"); st["exists"] != true || st["nexthop"] != "[172.22.1.200]:2525" {
		t.Fatalf("transport state: %v", st)
	}
}
