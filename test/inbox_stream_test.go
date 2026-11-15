package test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The live inbox stream: server-sent events for mail received in this
// environment only, resumable with Last-Event-ID, behind an admin session.
func TestInboxStream(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.srv.StreamMax, e.srv.StreamPoll = 1500*time.Millisecond, 100*time.Millisecond
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")
	var envID, projectID, otherEnv, otherProject string
	e.must(e.pool.QueryRow(ctx, `SELECT e.id::text, p.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='sacas' AND e.mode='sandbox'`).Scan(&envID, &projectID))
	e.must(e.pool.QueryRow(ctx, `SELECT e.id::text, p.id::text FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.name='other' AND e.mode='sandbox'`).Scan(&otherEnv, &otherProject))
	insert := func(pid, eid, subject string) {
		_, err := e.pool.Exec(ctx, `INSERT INTO inbound_messages(id, project_id, environment_id, spool_id, envelope_from, envelope_to, header_from, subject, raw_size, retention_expires_at)
VALUES (gen_random_uuid(),$1,$2,gen_random_uuid(),'a@b.example','in@in.example','Ada <ada@example.org>',$3,8, now() + interval '1 day')`, pid, eid, subject)
		e.must(err)
	}
	insert(projectID, envID, "Before the stream") // not sent: the stream starts at "now"
	go func() {
		time.Sleep(400 * time.Millisecond)
		insert(projectID, envID, "Hello live")
		insert(otherProject, otherEnv, "Other project")
	}()
	r := e.raw("GET", wsBase+"/inbox/stream", map[string]string{"Cookie": admin.cookie}, nil)
	if r.Code != 200 || !strings.HasPrefix(r.Hdr.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %v %s", r.Code, r.Hdr, r.Raw)
	}
	if !strings.Contains(r.Raw, "event: inbound") || !strings.Contains(r.Raw, `"subject":"Hello live"`) ||
		strings.Contains(r.Raw, "Before the stream") || strings.Contains(r.Raw, "Other project") {
		t.Fatalf("stream: %s", r.Raw)
	}
	// Resume from the last event id: only what came after it.
	var lastID string
	for _, l := range strings.Split(r.Raw, "\n") {
		if strings.HasPrefix(l, "id: ") {
			lastID = strings.TrimPrefix(l, "id: ")
		}
	}
	insert(projectID, envID, "After reconnect")
	r = e.raw("GET", wsBase+"/inbox/stream", map[string]string{"Cookie": admin.cookie, "Last-Event-ID": lastID}, nil)
	if !strings.Contains(r.Raw, "After reconnect") || strings.Contains(r.Raw, "Hello live") {
		t.Fatalf("resume from %s: %s", lastID, r.Raw)
	}
	// Before the stream started, with an old cursor: the backlog comes first.
	old := strconv.FormatInt(time.Now().Add(-time.Hour).UnixNano(), 10) + ":"
	r = e.raw("GET", wsBase+"/inbox/stream", map[string]string{"Cookie": admin.cookie, "Last-Event-ID": old}, nil)
	if !strings.Contains(r.Raw, "Before the stream") {
		t.Fatalf("backlog: %s", r.Raw)
	}
}
