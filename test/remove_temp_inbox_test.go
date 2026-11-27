package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/db"
)

// Migration 0045 on an installation that used temporary inboxes: they and their
// mail go, their files are deleted by the scheduler, and permanent mailboxes
// keep receiving.
func TestRemovingTemporaryInboxesKeepsRealMail(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("sacas")
	real := "support@" + inDomain
	temp := "tmp-abc@" + inDomain
	for _, local := range []string{"support", "tmp-abc"} {
		_, err := e.svc.CreateInboundAddress(ctx, "sacas", "sandbox", local, nil, "store", "")
		e.must(err)
	}
	// The schema before the migration.
	for _, q := range []string{
		`ALTER TABLE inbound_addresses ADD COLUMN temporary boolean NOT NULL DEFAULT false, ADD COLUMN expires_at timestamptz`,
		`ALTER TABLE projects ADD COLUMN temp_inbox_limit integer NOT NULL DEFAULT 10`,
		`UPDATE inbound_addresses SET temporary=true, expires_at=now()+interval '1 day' WHERE local_part='tmp-abc'`,
		`DELETE FROM schema_migrations WHERE version='0045_remove_temporary_inboxes.sql'`,
	} {
		_, err := e.pool.Exec(ctx, q)
		e.must(err)
	}
	addr := e.listener("127.0.0.0/8")
	for _, to := range []string{real, temp} {
		e.must(smtpSend(addr, "asha@customer.example", []string{to}, inboundMail(to)))
	}
	// Mail was stored for both before the upgrade.
	e.process()
	var tempID string
	e.must(e.pool.QueryRow(ctx, `SELECT m.id::text FROM inbound_messages m JOIN inbound_addresses a ON a.id=m.address_id WHERE a.temporary`).Scan(&tempID))
	dir := filepath.Join(e.svc.Opts.DataDir, "attachments", tempID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected an attachment on disk: %v", err)
	}

	e.must(db.Migrate(ctx, e.pool))
	if n := e.count(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND column_name IN ('temporary','temp_inbox_limit')`); n != 0 {
		t.Fatalf("columns left: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM inbound_addresses WHERE local_part='tmp-abc'`); n != 0 {
		t.Fatal("the temporary inbox remains")
	}
	if n := e.count(`SELECT count(*) FROM inbound_messages`); n != 1 {
		t.Fatalf("permanent mail: %d", n)
	}
	handled, err := e.svc.PurgeRemovedInboundFiles(ctx)
	e.must(err)
	if _, err := os.Stat(dir); handled != 1 || !os.IsNotExist(err) {
		t.Fatalf("files: handled=%d err=%v", handled, err)
	}
	// Real inbound mail keeps working.
	e.must(smtpSend(addr, "juma@customer.example", []string{real}, strings.Replace(inboundMail(real), "reply-1@", "reply-2@", 1)))
	e.process()
	if n := e.count(`SELECT count(*) FROM inbound_messages`); n != 2 {
		t.Fatalf("after the upgrade: %d", n)
	}
}
