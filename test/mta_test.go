package test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/mtaagent"
)

type fakeAgent struct {
	mu      sync.Mutex
	queue   []mtaagent.QueueEntry
	actions []string
	err     error
}

func (f *fakeAgent) Queue(context.Context) ([]mtaagent.QueueEntry, error) { return f.queue, f.err }
func (f *fakeAgent) Status(context.Context) (*mtaagent.Status, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &mtaagent.Status{Queue: map[string]int{"deferred": 1}, Total: 1, PostfixActive: true, SignerActive: true}, nil
}
func (f *fakeAgent) Act(_ context.Context, id string, a mtaagent.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.actions = append(f.actions, string(a)+" "+id)
	return nil
}
func (f *fakeAgent) Flush(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, "flush")
	return f.err
}

// E10: queue view and controls, operator only, audited, delete needs the id typed back.
func TestMTAQueueControls(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	op := e.user("op@njiwa.co.tz", "admin")
	e.admOK(op, "POST", "/admin/v1/users", map[string]any{"email": "limited@njiwa.co.tz", "password": adminPass, "installation_operator": false})
	limited := e.login("limited@njiwa.co.tz")
	k := e.provision("q")
	r := e.post(k.live, idem("q1"), otpBody(testRcpt))
	id := str(r.JSON, "id")
	e.runWorker()
	e.must(execOK(e, `UPDATE messages SET mta_queue_id='4C1D2E3F4A' WHERE id='`+id+`'`))

	// Not configured: say so, change nothing.
	if res := e.adm(op, "GET", "/admin/v1/mta/queue", nil); res.Code != 409 || errCode(res) != "mta_agent_not_configured" {
		t.Fatalf("not configured: %d %s", res.Code, res.Raw)
	}
	if s := e.admOK(op, "GET", "/admin/v1/mta/status", nil); s.JSON["configured"] != false {
		t.Fatalf("status: %s", s.Raw)
	}

	agent := &fakeAgent{queue: []mtaagent.QueueEntry{
		{QueueName: "deferred", QueueID: "4C1D2E3F4A", ArrivalTime: time.Now().Add(-time.Hour).Unix(), MessageSize: 900, Sender: "bounces@notify.example.com",
			Recipients: []mtaagent.Recipient{{Address: testRcpt, DelayReason: "connect to mx.example.org: Connection timed out; copy to secret@example.org"}}},
		{QueueName: "hold", QueueID: "0000AAAA11", ArrivalTime: time.Now().Unix(), Sender: "x@y.example", Recipients: []mtaagent.Recipient{{Address: "other@example.org"}}},
	}}
	e.srv.MTA = agent

	for _, path := range []string{"/admin/v1/mta/queue", "/admin/v1/mta/status"} {
		if res := e.adm(limited, "GET", path, nil); res.Code != 403 || errCode(res) != "operator_required" {
			t.Fatalf("restricted admin on %s: %d", path, res.Code)
		}
	}
	list := e.admOK(op, "GET", "/admin/v1/mta/queue", nil)
	if strings.Contains(list.Raw, testRcpt) || strings.Contains(list.Raw, "secret@example.org") || strings.Contains(list.Raw, "other@example.org") {
		t.Fatalf("queue view exposes addresses: %s", list.Raw)
	}
	its := items(list)
	if len(its) != 2 || its[0]["queue"] != "deferred" || !strings.Contains(list.Raw, "Connection timed out") {
		t.Fatalf("queue view: %s", list.Raw)
	}
	m, _ := its[0]["message"].(map[string]any)
	if m["message_id"] != id || m["project"] != "q" {
		t.Fatalf("queue entry not tied to our message: %v", its[0])
	}
	if its[1]["message"] != nil {
		t.Fatal("an unknown queue entry was tied to a message")
	}

	// Hold, release, requeue, flush.
	for _, a := range []string{"hold", "release", "requeue"} {
		e.admOK(op, "POST", "/admin/v1/mta/queue/4C1D2E3F4A/"+a, map[string]any{})
	}
	e.admOK(op, "POST", "/admin/v1/mta/flush", map[string]any{})
	if got := strings.Join(agent.actions, ","); got != "hold 4C1D2E3F4A,release 4C1D2E3F4A,requeue 4C1D2E3F4A,flush" {
		t.Fatalf("agent calls: %s", got)
	}
	if res := e.adm(op, "POST", "/admin/v1/mta/queue/not%20an%20id/hold", map[string]any{}); res.Code != 404 {
		t.Fatalf("invalid queue id: %d", res.Code)
	}

	// Delete needs the id typed back.
	agent.actions = nil
	for _, body := range []map[string]any{{}, {"confirm": "yes"}, {"confirm": "0000AAAA11"}} {
		if res := e.adm(op, "POST", "/admin/v1/mta/queue/4C1D2E3F4A/delete", body); res.Code != 422 || errCode(res) != "confirmation_required" {
			t.Fatalf("delete without the right confirmation (%v): %d %s", body, res.Code, res.Raw)
		}
	}
	if len(agent.actions) != 0 {
		t.Fatalf("a refused delete reached the agent: %v", agent.actions)
	}
	e.admOK(op, "POST", "/admin/v1/mta/queue/4C1D2E3F4A/delete", map[string]any{"confirm": "4c1d2e3f4a"})
	if len(agent.actions) != 1 || agent.actions[0] != "delete 4C1D2E3F4A" {
		t.Fatalf("delete: %v", agent.actions)
	}

	// Audit trail and message timeline.
	for _, a := range []string{"hold", "release", "requeue", "delete", "flush"} {
		if e.count(`SELECT count(*) FROM audit_log WHERE action=$1`, "mta.queue."+a) != 1 {
			t.Fatalf("mta.queue.%s not audited", a)
		}
	}
	if e.count(`SELECT count(*) FROM delivery_events WHERE message_id=$1 AND source='operator' AND safe_reason LIKE '%will not be delivered%'`, id) != 1 {
		t.Fatal("the message timeline does not show the queue deletion")
	}

	// A stopped agent is reported, not hidden; status degrades to "unreachable".
	agent.err = mtaagent.ErrUnreachable
	if res := e.adm(op, "GET", "/admin/v1/mta/queue", nil); res.Code != 503 || errCode(res) != "mta_agent_unreachable" {
		t.Fatalf("unreachable: %d %s", res.Code, res.Raw)
	}
	if s := e.admOK(op, "GET", "/admin/v1/mta/status", nil); s.JSON["reachable"] != false {
		t.Fatalf("status when unreachable: %s", s.Raw)
	}
	_ = ctx
}
