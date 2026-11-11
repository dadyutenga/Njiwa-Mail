package test

import (
	"sync"
	"sync/atomic"
	"testing"
)

// F04: installation-wide and per-sending-domain caps bound Live mail across
// projects and keys, atomically under concurrency; Sandbox is not counted.
func TestInstallationAndDomainLiveLimits(t *testing.T) {
	avoidWindowEdge()
	e := setup(t)
	a := e.provision("alpha")
	b := e.provisionFrom("beta", "security@notify-b.example.com")
	e.svc.Opts.LiveDomainPerMinute = 3
	e.svc.Opts.LiveGlobalPerMinute = 5
	t.Cleanup(func() { e.svc.Opts.LiveDomainPerMinute, e.svc.Opts.LiveGlobalPerMinute = 0, 0 })

	bBody := func() map[string]any {
		m := otpBody(testRcpt)
		m["from"] = "security@notify-b.example.com"
		return m
	}
	for i := 0; i < 3; i++ {
		if r := e.post(a.live, idem("a"+string(rune('0'+i))), otpBody(testRcpt)); r.Code != 202 {
			t.Fatalf("alpha %d: %d %s", i, r.Code, r.Raw)
		}
	}
	r := e.post(a.live, idem("a-over"), otpBody(testRcpt))
	if r.Code != 429 || errCode(r) != "rate_limited" || r.Hdr.Get("Retry-After") == "" {
		t.Fatalf("domain cap: %d %s", r.Code, r.Raw)
	}
	// Sandbox is never counted.
	if r := e.post(a.sbx, idem("sbx"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("sandbox limited: %d %s", r.Code, r.Raw)
	}
	// Another domain has its own domain budget but shares the installation one.
	for i := 0; i < 2; i++ {
		if r := e.post(b.live, idem("b"+string(rune('0'+i))), bBody()); r.Code != 202 {
			t.Fatalf("beta %d: %d %s", i, r.Code, r.Raw)
		}
	}
	if r := e.post(b.live, idem("b-over"), bBody()); r.Code != 429 {
		t.Fatalf("installation cap across projects: %d %s", r.Code, r.Raw)
	}
	// A replay of an accepted request is not charged again.
	if r := e.post(a.live, idem("a0"), otpBody(testRcpt)); r.Code != 202 && r.Code != 200 {
		t.Fatalf("idempotent replay limited: %d %s", r.Code, r.Raw)
	}
}

func TestParallelFloodIsBounded(t *testing.T) {
	avoidWindowEdge()
	e := setup(t)
	k := e.provision("flood")
	e.svc.Opts.LiveDomainPerMinute = 5
	t.Cleanup(func() { e.svc.Opts.LiveDomainPerMinute = 0 })
	var ok int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if r := e.post(k.live, idem("f"+string(rune('A'+i))), otpBody(testRcpt)); r.Code == 202 {
				atomic.AddInt32(&ok, 1)
			}
		}(i)
	}
	wg.Wait()
	if ok != 5 {
		t.Fatalf("accepted %d of 25 with a limit of 5", ok)
	}
	if n := e.count(`SELECT count(*) FROM messages m JOIN environments en ON en.id=m.environment_id WHERE en.mode='live'`); n != 5 {
		t.Fatalf("%d live messages stored", n)
	}
}
