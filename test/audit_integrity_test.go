package test

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
)

// bypass runs statements with triggers off, as a database superuser could, to simulate tampering.
func (e *env) bypass(stmts ...string) {
	e.t.Helper()
	ctx := context.Background()
	tx, err := e.svc.Pool.Begin(ctx)
	e.must(err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
	e.must(err)
	for _, s := range stmts {
		_, err = tx.Exec(ctx, s)
		e.must(err)
	}
	e.must(tx.Commit(ctx))
}

// seedAudit writes a few ordinary entries through the service.
func (e *env) seedAudit() {
	e.t.Helper()
	ctx := context.Background()
	for i, a := range []string{"project.create", "domain.create", "project.update", "api_key.create", "user.update"} {
		e.must(e.svc.Audit(ctx, service.Actor{Name: "tester@njiwa.co.tz"}, a, strings.Split(a, ".")[0], jsonNum(int64(i)),
			map[string]any{"step": i, "note": "<b>=1+2</b>"}))
	}
}

func verify(t *testing.T, e *env, admin session) map[string]any {
	t.Helper()
	return e.admOK(admin, "GET", "/admin/v1/audit/verify", nil).JSON
}

func TestAuditLogIsAppendOnlyAndHashChained(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.seedAudit()

	// Every entry is chained: contiguous sequence numbers, each linking to the previous hash.
	if n := e.count(`SELECT count(*) FROM audit_log a JOIN audit_log b ON b.seq = a.seq + 1 WHERE b.prev_hash <> a.hash`); n != 0 {
		t.Fatalf("%d broken links", n)
	}
	if v := verify(t, e, admin); v["ok"] != true || v["entries"].(float64) < 3 {
		t.Fatalf("fresh log does not verify: %v", v)
	}

	// Nobody can rewrite or remove entries through the database either.
	for _, stmt := range []string{`UPDATE audit_log SET actor='someone else'`, `DELETE FROM audit_log`, `TRUNCATE audit_log`} {
		if _, err := e.svc.Pool.Exec(ctx, stmt); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s was not refused: %v", stmt, err)
		}
	}

	// Tampering behind the triggers' back is detected, at the entry that was touched.
	var mid int64
	e.must(e.svc.Pool.QueryRow(ctx, `SELECT seq FROM audit_log ORDER BY seq OFFSET 1 LIMIT 1`).Scan(&mid))
	e.bypass(`UPDATE audit_log SET details = '{"forged":true}' WHERE seq = ` + jsonNum(mid))
	v := verify(t, e, admin)
	if v["ok"] != false || int64(v["broken_seq"].(float64)) != mid || !strings.Contains(v["problem"].(string), "changed") {
		t.Fatalf("edit not detected: %v", v)
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestAuditTamperingByRemoval(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.seedAudit()
	var mid int64
	e.must(e.svc.Pool.QueryRow(ctx, `SELECT seq FROM audit_log ORDER BY seq OFFSET 1 LIMIT 1`).Scan(&mid))

	e.bypass(`DELETE FROM audit_log WHERE seq = ` + jsonNum(mid))
	if v := verify(t, e, admin); v["ok"] != false || int64(v["broken_seq"].(float64)) != mid+1 || !strings.Contains(v["problem"].(string), "missing") {
		t.Fatalf("a removed entry was not detected: %v", v)
	}

}

// Removing the newest entries is caught by the chain head.
func TestAuditTamperingAtTheEnd(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.seedAudit()
	var last int64
	e.must(e.svc.Pool.QueryRow(ctx, `SELECT max(seq) FROM audit_log`).Scan(&last))
	e.bypass(`DELETE FROM audit_log WHERE seq = ` + jsonNum(last))
	if v := verify(t, e, admin); v["ok"] != false || !strings.Contains(v["problem"].(string), "newest") {
		t.Fatalf("a removed newest entry was not detected: %v", v)
	}
}

// Retention removes the oldest entries only, moves the anchor, records itself, and the
// rest of the chain still verifies.
func TestAuditRetentionKeepsTheChainVerifiable(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	for range 3 {
		_, err := e.svc.Pool.Exec(ctx, `INSERT INTO audit_log(actor, action, at) VALUES ('old', 'test.old', now() - interval '400 days')`)
		e.must(err)
	}
	e.seedAudit()
	before := e.count(`SELECT count(*) FROM audit_log`)

	r, err := e.svc.RunRetention(ctx)
	e.must(err)
	// The admin user's entries came first, so everything up to the last old entry goes.
	if r.AuditEntries < 3 || e.count(`SELECT count(*) FROM audit_log WHERE action='test.old'`) != 0 {
		t.Fatalf("old entries not purged: %d", r.AuditEntries)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action='audit.retention_purge'`) != 1 {
		t.Fatal("the purge was not recorded")
	}
	if got := e.count(`SELECT count(*) FROM audit_log`); got != before-int(r.AuditEntries)+1 {
		t.Fatalf("expected %d entries, got %d", before-int(r.AuditEntries)+1, got)
	}
	if v := verify(t, e, admin); v["ok"] != true || v["anchor_seq"].(float64) == 0 {
		t.Fatalf("chain does not verify after retention: %v", v)
	}
	// A second run finds nothing more to do.
	r, err = e.svc.RunRetention(ctx)
	e.must(err)
	if r.AuditEntries != 0 {
		t.Fatalf("second run purged %d", r.AuditEntries)
	}
}

func TestAuditExportFormatsAndFilters(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.seedAudit()

	pdf := e.adm(admin, "GET", "/admin/v1/audit/export?format=pdf", nil)
	if pdf.Code != 200 || !strings.HasPrefix(pdf.Raw, "%PDF-") || pdf.Hdr.Get("Content-Type") != "application/pdf" ||
		!strings.Contains(pdf.Hdr.Get("Content-Disposition"), ".pdf") || pdf.Hdr.Get("X-Audit-Integrity") != "verified" {
		t.Fatalf("pdf: %d %v %.40q", pdf.Code, pdf.Hdr, pdf.Raw)
	}

	csv := e.adm(admin, "GET", "/admin/v1/audit/export?format=csv&action=project.", nil)
	lines := strings.Split(strings.TrimSpace(csv.Raw), "\n")
	if csv.Code != 200 || !strings.HasPrefix(lines[0], "seq,at_utc,actor,action") || len(lines) < 2 {
		t.Fatalf("csv: %d %q", csv.Code, csv.Raw)
	}
	for _, l := range lines[1:] {
		if !strings.Contains(l, ",project.") {
			t.Fatalf("action filter ignored: %q", l)
		}
	}

	jl := e.adm(admin, "GET", "/admin/v1/audit/export?format=jsonl", nil)
	sc := bufio.NewScanner(strings.NewReader(jl.Raw))
	n := 0
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil || row["hash"] == "" || row["seq"] == nil {
			t.Fatalf("jsonl line %d: %v %q", n, err, sc.Text())
		}
		n++
	}
	if jl.Code != 200 || n == 0 {
		t.Fatalf("jsonl: %d, %d lines", jl.Code, n)
	}

	// A date range in the future matches nothing; a bad timestamp or format is refused.
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	if r := e.admOK(admin, "GET", "/admin/v1/audit?from="+future, nil); len(items(r)) != 0 {
		t.Fatal("from filter ignored")
	}
	for _, p := range []string{"/admin/v1/audit/export?format=docx", "/admin/v1/audit?from=yesterday"} {
		if r := e.adm(admin, "GET", p, nil); r.Code != 400 {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}

	// Every export is itself in the audit log.
	if got := e.count(`SELECT count(*) FROM audit_log WHERE action='audit.export'`); got != 3 {
		t.Fatalf("exports recorded: %d", got)
	}
}
