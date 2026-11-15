package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

func TestSegments(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	sc, err := e.svc.ResolveScope(ctx, "sacas", "sandbox")
	e.must(err)
	listID, err := e.svc.CreateList(ctx, sc, "Customers", "")
	e.must(err)
	add := func(email, plan string, lists ...string) {
		_, _, err := e.svc.UpsertContact(ctx, sc, service.ContactInput{Email: email, Attributes: map[string]any{"plan": plan}, Lists: lists})
		e.must(err)
	}
	add("a@gmail.com", "pro", listID)
	add("b@gmail.com", "free", listID)
	add("c@example.org", "pro")
	add("d@example.org", "pro", listID)
	e.must(e.svc.Suppress(ctx, "sacas", "sandbox", "d@example.org", "complaint report"))

	preview := func(rules map[string]any) map[string]any {
		return e.admOK(admin, "POST", wsBase+"/segments/preview", rules).JSON
	}
	// Pro customers on the list: a, d (d complained, so not sendable).
	p := preview(map[string]any{"match": "all", "conditions": []map[string]string{
		{"field": "attribute", "op": "eq", "key": "plan", "value": "pro"}, {"field": "list", "op": "in", "value": listID}}})
	if p["matches"] != float64(2) || p["sendable"] != float64(1) {
		t.Fatalf("pro on list: %v", p)
	}
	// Any of: gmail or not on the list: a, b, c.
	p = preview(map[string]any{"match": "any", "conditions": []map[string]string{
		{"field": "domain", "op": "eq", "value": "GMAIL.com"}, {"field": "list", "op": "not_in", "value": listID}}})
	if p["matches"] != float64(3) {
		t.Fatalf("any: %v", p)
	}
	// Values are parameters, never SQL.
	p = preview(map[string]any{"conditions": []map[string]string{{"field": "attribute", "op": "eq", "key": "plan", "value": "x' OR '1'='1"}}})
	if p["matches"] != float64(0) {
		t.Fatalf("injection matched: %v", p)
	}
	for _, bad := range []map[string]string{{"field": "attribute", "op": "eq", "key": "plan; drop", "value": "x"},
		{"field": "nope", "op": "eq"}, {"field": "status", "op": "eq", "value": "deleted"}} {
		if r := e.adm(admin, "POST", wsBase+"/segments/preview", map[string]any{"conditions": []map[string]string{bad}}); r.Code != 400 {
			t.Fatalf("%v: %d %s", bad, r.Code, r.Raw)
		}
	}
	seg := e.admOK(admin, "POST", wsBase+"/segments", map[string]any{"name": "Pro", "rules": map[string]any{
		"conditions": []map[string]string{{"field": "attribute", "op": "eq", "key": "plan", "value": "pro"}, {"field": "status", "op": "eq", "value": "active"}}}})
	if r := e.admOK(admin, "GET", wsBase+"/segments", nil); !strings.Contains(r.Raw, `"name":"Pro"`) {
		t.Fatalf("list: %s", r.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/segments/"+str(seg.JSON, "id"), nil)
}
