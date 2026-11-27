package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// Every table that refers to a project is known, and "cascade" is really backed
// by a cascading foreign key.
func TestEveryProjectTableIsCoveredByDelete(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	rows, err := e.pool.Query(ctx, `SELECT DISTINCT c.table_name, EXISTS (SELECT 1 FROM pg_constraint k
   WHERE k.contype='f' AND k.confdeltype='c' AND k.conrelid=c.table_name::regclass)
 FROM information_schema.columns c WHERE c.table_schema='public' AND c.column_name IN ('project_id','environment_id')`)
	e.must(err)
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var table string
		var cascades bool
		e.must(rows.Scan(&table, &cascades))
		seen[table] = true
		switch service.ProjectTables[table] {
		case "explicit":
		case "cascade":
			if !cascades {
				t.Errorf("%s says cascade but has no cascading foreign key", table)
			}
		default:
			t.Errorf("table %s refers to a project but ProjectTables does not say how deleting one removes it", table)
		}
	}
	for table := range service.ProjectTables {
		if !seen[table] {
			t.Errorf("ProjectTables names %s, which does not refer to a project", table)
		}
	}
}

func TestArchiveRestoreAndDeleteProject(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("sacas")
	other := e.provision("other")
	admin := e.user("admin@njiwa.co.tz", "admin")
	_, err := e.svc.CreateInboundAddress(ctx, "sacas", "sandbox", "orders", nil, "store", "")
	e.must(err)
	to := "orders@" + inDomain
	e.must(smtpSend(e.listener("127.0.0.0/8"), "asha@customer.example", []string{to}, inboundMail(to)))
	e.process()
	var pid, inID string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name='sacas'`).Scan(&pid))
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM inbound_messages WHERE project_id=$1`, pid).Scan(&inID))
	dir := filepath.Join(e.svc.Opts.DataDir, "attachments", inID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	envs := []string{}
	rows, err := e.pool.Query(ctx, `SELECT id::text FROM environments WHERE project_id=$1`, pid)
	e.must(err)
	for rows.Next() {
		var id string
		e.must(rows.Scan(&id))
		envs = append(envs, id)
	}
	rows.Close()

	// The list carries the counts.
	l := e.admOK(admin, "GET", "/admin/v1/projects", nil)
	for _, want := range []string{`"live_active":2`, `"sandbox_active":1`, `"messages_30d":0`, `"domains":1`, `"slug":"sacas"`} {
		if !strings.Contains(l.Raw, want) {
			t.Errorf("list lacks %s: %s", want, l.Raw)
		}
	}

	// A queued Live message blocks archiving and says how many.
	if r := e.post(k.live, idem("queued"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	base := "/admin/v1/projects/sacas"
	if r := e.adm(admin, "POST", base+"/archive", map[string]any{"confirm": "sacas"}); r.Code != 409 || errCode(r) != "project_has_queued_mail" || !strings.Contains(r.Raw, "1 Live message") {
		t.Fatalf("with queued mail: %d %s", r.Code, r.Raw)
	}
	if n := e.runWorker(); n != 1 {
		t.Fatalf("worker %d", n)
	}
	if r := e.adm(admin, "POST", base+"/archive", map[string]any{"confirm": "wrong"}); r.Code != 400 {
		t.Fatalf("wrong name: %d", r.Code)
	}
	if r := e.adm(admin, "DELETE", base, map[string]any{"confirm": "sacas"}); r.Code != 409 || errCode(r) != "project_not_archived" {
		t.Fatalf("delete before archive: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "POST", base+"/archive", map[string]any{"confirm": "sacas"})
	// Hidden, closed to the workspace, and its keys no longer send.
	if l := e.admOK(admin, "GET", "/admin/v1/projects", nil); strings.Contains(l.Raw, `"name":"sacas"`) {
		t.Fatalf("still listed: %s", l.Raw)
	}
	if l := e.admOK(admin, "GET", "/admin/v1/projects?archived=true", nil); !strings.Contains(l.Raw, `"name":"sacas"`) {
		t.Fatalf("not in the archive: %s", l.Raw)
	}
	if r := e.adm(admin, "GET", wsBase+"/checklist", nil); r.Code != 404 {
		t.Fatalf("workspace of an archived project: %d", r.Code)
	}
	if r := e.post(k.live, idem("archived"), otpBody(testRcpt)); r.Code < 400 {
		t.Fatalf("an archived project sent: %d", r.Code)
	}
	e.admOK(admin, "POST", base+"/restore", nil)
	if r := e.post(k.sbx, idem("restored"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("after restore: %d %s", r.Code, r.Raw)
	}
	e.runWorker()

	e.admOK(admin, "POST", base+"/archive", map[string]any{"confirm": "sacas"})
	rm := e.admOK(admin, "GET", base+"/removal", nil)
	if rm.JSON["domain_claims"] != float64(1) || rm.JSON["attachments"] == float64(0) {
		t.Fatalf("removal: %s", rm.Raw)
	}
	e.admOK(admin, "DELETE", base, map[string]any{"confirm": "sacas"})
	for table := range service.ProjectTables {
		var hasProject, hasEnv bool
		e.must(e.pool.QueryRow(ctx, `SELECT bool_or(column_name='project_id'), bool_or(column_name='environment_id') FROM information_schema.columns
 WHERE table_schema='public' AND table_name=$1`, table).Scan(&hasProject, &hasEnv))
		if hasProject {
			if n := e.count(`SELECT count(*) FROM `+table+` WHERE project_id=$1`, pid); n != 0 {
				t.Errorf("%s keeps %d rows by project", table, n)
			}
		}
		if hasEnv {
			if n := e.count(`SELECT count(*) FROM `+table+` WHERE environment_id = ANY($1::uuid[])`, envs); n != 0 {
				t.Errorf("%s keeps %d rows by environment", table, n)
			}
		}
	}
	if e.count(`SELECT count(*) FROM domain_claims WHERE name='notify.example.com'`) != 0 {
		t.Error("the domain claim was not released")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("attachment files remain: %v", err)
	}
	if e.count(`SELECT count(*) FROM audit_log WHERE action IN ('project.archive','project.delete')`) < 2 {
		t.Error("not audited")
	}
	// The other project is untouched.
	if r := e.post(other.sbx, idem("other"), otpBody(testRcpt)); r.Code != 202 && r.Code != 403 {
		t.Fatalf("other project: %d %s", r.Code, r.Raw)
	}
	if e.count(`SELECT count(*) FROM api_keys k JOIN projects p ON p.id=k.project_id WHERE p.name='other'`) == 0 {
		t.Error("the other project lost its keys")
	}
}

func TestRenameRevokeAndOwnersOnly(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := e.provision("sacas")
	e.provision("taken")
	admin := e.user("admin@njiwa.co.tz", "admin")
	_, err := e.svc.CreateUser(ctx, "outsider@njiwa.co.tz", adminPass, "admin")
	e.must(err)
	_, err = e.svc.CreateUser(ctx, "dev@njiwa.co.tz", adminPass, "admin")
	e.must(err)
	_, err = e.pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id, role, added_by)
SELECT p.id, u.id, 'admin', 'test' FROM projects p, users u WHERE p.name='sacas' AND u.email='dev@njiwa.co.tz'`)
	e.must(err)
	outsider, member := e.login("outsider@njiwa.co.tz"), e.login("dev@njiwa.co.tz")
	base := "/admin/v1/projects/sacas"
	for _, c := range []struct {
		s    session
		want int
	}{{outsider, 404}, {member, 403}} {
		for _, call := range [][2]string{{"POST", "/archive"}, {"DELETE", ""}, {"POST", "/revoke-keys"}, {"POST", "/rename"}, {"GET", "/removal"}} {
			if r := e.adm(c.s, call[0], base+call[1], map[string]any{"confirm": "sacas", "name": "x"}); r.Code != c.want {
				t.Errorf("%s %s: %d, want %d", call[0], call[1], r.Code, c.want)
			}
		}
	}
	if r := e.adm(admin, "POST", base+"/rename", map[string]any{"name": "taken"}); r.Code != 409 {
		t.Fatalf("taken: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "POST", base+"/rename", map[string]any{"name": "savings"})
	// The key still works under the new name.
	if r := e.post(k.sbx, idem("renamed"), otpBody(testRcpt)); r.Code != 202 {
		t.Fatalf("after rename: %d %s", r.Code, r.Raw)
	}
	r := e.admOK(admin, "POST", "/admin/v1/projects/savings/revoke-keys", nil)
	if r.JSON["revoked"] != float64(3) {
		t.Fatalf("%s", r.Raw)
	}
	if r := e.post(k.sbx, idem("revoked"), otpBody(testRcpt)); r.Code != 401 {
		t.Fatalf("revoked key: %d", r.Code)
	}
	// The grace period ends: the scheduler deletes it.
	e.runWorker()
	e.admOK(admin, "POST", "/admin/v1/projects/savings/archive", map[string]any{"confirm": "savings"})
	if n, err := e.svc.PurgeArchivedProjects(ctx); err != nil || n != 0 {
		t.Fatalf("purged early: %d %v", n, err)
	}
	_, err = e.pool.Exec(ctx, `UPDATE projects SET purge_after=now() WHERE name='savings'`)
	e.must(err)
	if n, err := e.svc.PurgeArchivedProjects(ctx); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if e.count(`SELECT count(*) FROM projects WHERE name='savings'`) != 0 {
		t.Fatal("still there")
	}
}
