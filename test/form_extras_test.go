package test

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestFormRoutingRulesTagsSpamAndExport(t *testing.T) {
	e := setup(t)
	e.svc.Opts.FormsEnabled = true
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")
	fid := str(e.admOK(admin, "POST", wsBase+"/forms", map[string]any{"name": "Contact", "allowed_origins": []string{siteOrigin + "/"}, "digest": "none"}).JSON, "id")
	other := str(e.admOK(admin, "POST", "/admin/v1/w/other/sandbox/forms", map[string]any{"name": "Other", "allowed_origins": []string{siteOrigin + "/"}, "digest": "none"}).JSON, "id")
	path := "/v1/forms/" + fid
	site := map[string]string{"Origin": siteOrigin}
	me := str(e.admOK(admin, "GET", "/admin/v1/auth/me", nil).JSON, "id")
	rules := wsBase + "/forms/" + fid + "/rules"

	// Validation.
	for name, body := range map[string]map[string]any{
		"no name":      {"op": "contains", "value": "x", "add_tags": []string{"a"}},
		"bad regex":    {"name": "r", "op": "regex", "value": "([", "add_tags": []string{"a"}},
		"no action":    {"name": "r", "op": "contains", "value": "x"},
		"bad priority": {"name": "r", "op": "contains", "value": "x", "set_priority": "urgent"},
		"bad op":       {"name": "r", "op": "like", "value": "x", "add_tags": []string{"a"}},
		"tag spaces":   {"name": "r", "op": "contains", "value": "x", "add_tags": []string{"two words"}},
		"exists/field": {"name": "r", "op": "exists", "add_tags": []string{"a"}},
		"unknown user": {"name": "r", "op": "contains", "value": "x", "assign_to": "00000000-0000-4000-8000-000000000000"},
	} {
		if r := e.adm(admin, "POST", rules, body); r.Code != 400 {
			t.Fatalf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	// A rule of another project's form is not reachable.
	if r := e.adm(admin, "GET", wsBase+"/forms/"+other+"/rules", nil); r.Code != 404 {
		t.Fatalf("other form: %d", r.Code)
	}

	r1 := str(e.admOK(admin, "POST", rules, map[string]any{"name": "Refunds", "field": "message", "op": "contains", "value": "REFUND", "set_priority": "high", "add_tags": []string{"Billing", "refund"}}).JSON, "id")
	r2 := str(e.admOK(admin, "POST", rules, map[string]any{"name": "Partners", "field": "email", "op": "domain", "value": "@partner.example", "add_tags": []string{"partner"}, "assign_to": me}).JSON, "id")
	r3 := str(e.admOK(admin, "POST", rules, map[string]any{"name": "Everything else low", "op": "exists", "field": "message", "set_priority": "low"}).JSON, "id")

	// Dry run.
	dry := e.admOK(admin, "POST", rules+"/test", map[string]any{"fields": map[string]string{"email": "x@partner.example", "message": "I want a Refund"}})
	if dry.JSON["priority"] != "low" || len(dry.JSON["matched"].([]any)) != 3 || dry.JSON["assignee"] != me {
		t.Fatalf("dry run: %s", dry.Raw)
	}
	// Reorder: refunds last, with stop on the catch-all so it wins.
	if r := e.adm(admin, "PUT", rules+"/order", map[string]any{"ids": []string{r3, r2}}); r.Code != 400 {
		t.Fatalf("incomplete order: %d", r.Code)
	}
	e.admOK(admin, "PUT", rules+"/order", map[string]any{"ids": []string{r3, r2, r1}})
	got := e.admOK(admin, "GET", rules, nil).JSON["items"].([]any)
	if len(got) != 3 || str(got[0].(map[string]any), "id") != r3 || str(got[2].(map[string]any), "id") != r1 {
		t.Fatalf("order: %v", got)
	}
	dry = e.admOK(admin, "POST", rules+"/test", map[string]any{"fields": map[string]string{"email": "x@example.org", "message": "refund please"}})
	if dry.JSON["priority"] != "high" {
		t.Fatalf("last matching priority wins: %s", dry.Raw)
	}
	e.admOK(admin, "PUT", rules+"/"+r3, map[string]any{"name": "Everything else low", "op": "exists", "field": "message", "set_priority": "low", "stop": true})
	dry = e.admOK(admin, "POST", rules+"/test", map[string]any{"fields": map[string]string{"email": "x@example.org", "message": "refund please"}})
	if dry.JSON["priority"] != "low" || len(dry.JSON["matched"].([]any)) != 1 {
		t.Fatalf("stop: %s", dry.Raw)
	}
	e.admOK(admin, "PUT", rules+"/"+r3, map[string]any{"name": "Everything else low", "op": "exists", "field": "message", "set_priority": "low", "enabled": false})
	if dry = e.admOK(admin, "POST", rules+"/test", map[string]any{"fields": map[string]string{"email": "x@example.org", "message": "refund please"}}); dry.JSON["priority"] != "high" {
		t.Fatalf("disabled rule still applied: %s", dry.Raw)
	}

	// Real submissions get the labels.
	e.resetFormLimits()
	if r := e.formPost(path, "application/json", `{"name":"Neema","email":"neema@partner.example","message":"I need a refund"}`, site); r.Code != 202 {
		t.Fatalf("post: %d %s", r.Code, r.Raw)
	}
	e.resetFormLimits()
	if r := e.formPost(path, "application/json", `{"name":"Juma","email":"juma@example.org","message":"Hello there"}`, site); r.Code != 202 {
		t.Fatalf("post: %d %s", r.Code, r.Raw)
	}
	items := e.admOK(admin, "GET", wsBase+"/forms/submissions", nil).JSON["items"].([]any)
	var neema, juma string
	for _, it := range items {
		m := it.(map[string]any)
		if strings.Contains(str(m, "preview"), "refund") {
			neema = str(m, "id")
			if m["priority"] != "high" || m["assigned_to"] != "admin@njiwa.co.tz" || strings.Join(toStrings(m["tags"]), ",") != "partner,billing,refund" {
				t.Fatalf("labels from rules: %v", m)
			}
		} else {
			juma = str(m, "id")
			if m["priority"] != "normal" || len(toStrings(m["tags"])) != 0 {
				t.Fatalf("untouched: %v", m)
			}
		}
	}
	if neema == "" || juma == "" {
		t.Fatalf("items: %v", items)
	}
	if n := len(e.admOK(admin, "GET", wsBase+"/forms/submissions?tag=Billing", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("tag filter: %d", n)
	}
	if n := len(e.admOK(admin, "GET", wsBase+"/forms/submissions?priority=normal", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("priority filter: %d", n)
	}

	// Manual labels.
	e.admOK(admin, "PATCH", wsBase+"/forms/submissions/"+juma, map[string]any{"tags": []string{"VIP", "vip", " follow-up "}, "priority": "high"})
	d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+juma, nil)
	if d.JSON["priority"] != "high" || strings.Join(toStrings(d.JSON["tags"]), ",") != "vip,follow-up" {
		t.Fatalf("manual labels: %s", d.Raw)
	}
	if r := e.adm(admin, "PATCH", wsBase+"/forms/submissions/"+juma, map[string]any{"priority": "urgent"}); r.Code != 400 {
		t.Fatalf("bad priority: %d", r.Code)
	}
	if r := e.adm(admin, "PATCH", wsBase+"/forms/submissions/"+juma, map[string]any{"tags": []string{strings.Repeat("x", 31)}}); r.Code != 400 {
		t.Fatalf("long tag: %d", r.Code)
	}
	e.admOK(admin, "PATCH", wsBase+"/forms/submissions/"+juma, map[string]any{"tags": []string{}})
	if d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+juma, nil); len(toStrings(d.JSON["tags"])) != 0 || d.JSON["priority"] != "high" {
		t.Fatalf("clearing tags keeps priority: %s", d.Raw)
	}

	// Mark spam, block the sender; the next message from them is quarantined; not-spam undoes it.
	e.admOK(admin, "POST", wsBase+"/forms/submissions/"+juma+"/spam", map[string]any{"block_sender": true})
	if d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+juma, nil); d.JSON["state"] != "spam" {
		t.Fatalf("spam: %s", d.Raw)
	}
	if n := e.count(`SELECT count(*) FROM form_filters WHERE kind='email' AND pattern='juma@example.org' AND action='quarantine'`); n != 1 {
		t.Fatalf("filter: %d", n)
	}
	e.admOK(admin, "POST", wsBase+"/forms/submissions/"+juma+"/spam", map[string]any{"block_sender": true})
	if n := e.count(`SELECT count(*) FROM form_filters WHERE kind='email' AND pattern='juma@example.org'`); n != 1 {
		t.Fatalf("blocking twice made %d filters", n)
	}
	e.resetFormLimits()
	e.formPost(path, "application/json", `{"name":"Juma","email":"juma@example.org","message":"Again"}`, site)
	if n := e.count(`SELECT count(*) FROM form_submissions WHERE state='spam' AND action='quarantine'`); n != 1 {
		t.Fatalf("blocked sender was not quarantined: %d", n)
	}
	// Quarantined messages get no labels.
	if n := e.count(`SELECT count(*) FROM form_submissions WHERE action='quarantine' AND (cardinality(tags)>0 OR priority<>'normal' OR assigned_to IS NOT NULL)`); n != 0 {
		t.Fatalf("quarantined labelled: %d", n)
	}
	e.admOK(admin, "POST", wsBase+"/forms/submissions/"+juma+"/not-spam", nil)
	if d := e.admOK(admin, "GET", wsBase+"/forms/submissions/"+juma, nil); d.JSON["state"] != "new" {
		t.Fatalf("not spam: %s", d.Raw)
	}
	if n := e.count(`SELECT count(*) FROM form_filters WHERE kind='email'`); n != 0 {
		t.Fatalf("filter not lifted: %d", n)
	}
	if r := e.adm(admin, "POST", "/admin/v1/w/other/sandbox/forms/submissions/"+juma+"/spam", nil); r.Code != 404 {
		t.Fatalf("other project: %d", r.Code)
	}

	// Summary.
	sum := e.admOK(admin, "GET", wsBase+"/forms/submissions/summary?days=7", nil).JSON
	tot := sum["totals"].(map[string]any)
	if tot["submissions"] != float64(3) || tot["spam"] != float64(1) || tot["high_priority_open"] != float64(2) || len(sum["by_form"].([]any)) != 1 {
		t.Fatalf("summary: %v", sum)
	}

	// Export: decrypted fields, formulas defused, spam excluded unless asked for.
	e.admOK(admin, "PATCH", wsBase+"/forms/submissions/"+neema, map[string]any{"tags": []string{"=cmd"}})
	ex := e.adm(admin, "GET", wsBase+"/forms/submissions/export", nil)
	if ex.Code != 200 || !strings.HasPrefix(ex.Hdr.Get("Content-Type"), "text/csv") || !strings.Contains(ex.Hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", ex.Code, ex.Hdr)
	}
	recs, err := csv.NewReader(strings.NewReader(ex.Raw)).ReadAll()
	if err != nil || len(recs) != 3 {
		t.Fatalf("export rows %d: %v\n%s", len(recs), err, ex.Raw)
	}
	if !strings.Contains(ex.Raw, "neema@partner.example") || strings.Contains(ex.Raw, ",=cmd") || !strings.Contains(ex.Raw, "'=cmd") {
		t.Fatalf("export content:\n%s", ex.Raw)
	}
	if sp := e.adm(admin, "GET", wsBase+"/forms/submissions/export?state=spam", nil); strings.Count(sp.Raw, "\n") != 2 {
		t.Fatalf("spam export:\n%s", sp.Raw)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE action='form_submission.export'`); n != 2 {
		t.Fatalf("export audited %d times", n)
	}

	// Deleting a rule; deleting the form removes its rules.
	e.admOK(admin, "DELETE", rules+"/"+r1, nil)
	if r := e.adm(admin, "DELETE", rules+"/"+r1, nil); r.Code != 404 {
		t.Fatalf("delete twice: %d", r.Code)
	}
	e.admOK(admin, "DELETE", wsBase+"/forms/"+fid, nil)
	if n := e.count(`SELECT count(*) FROM form_rules`); n != 0 {
		t.Fatalf("rules left: %d", n)
	}
}

func toStrings(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, x := range a {
			out = append(out, x.(string))
		}
	}
	return out
}
