package test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
	"gopkg.in/yaml.v3"
)

// docs/openapi.yaml is the canonical public contract: the documentation site,
// llms.txt and the agent skill are generated from it. These tests fail when it
// drifts from the handlers, so the portal can never publish a contradiction.

type spec struct{}

func loadSpec(t *testing.T) (*spec, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	return &spec{}, generic
}

func dig(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

func strs(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, x.(string))
		}
	}
	sort.Strings(out)
	return out
}

var httpMethods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

func TestPublicOpenAPIRoutesMatchTheServer(t *testing.T) {
	e := setup(t)
	_, g := loadSpec(t)
	inSpec := map[string]bool{}
	for p, item := range g["paths"].(map[string]any) {
		for m := range item.(map[string]any) {
			if httpMethods[m] {
				inSpec[strings.ToUpper(m)+" "+p] = true
			}
		}
	}
	inServer := map[string]bool{}
	colon := regexp.MustCompile(`:([A-Za-z]+)`)
	for _, r := range e.h.GetRoutes(true) {
		if !(strings.HasPrefix(r.Path, "/v1/") || strings.HasPrefix(r.Path, "/health/")) {
			continue
		}
		if r.Method == "HEAD" || r.Method == "OPTIONS" || r.Method == "USE" {
			continue
		}
		inServer[r.Method+" "+colon.ReplaceAllString(r.Path, "{$1}")] = true
	}
	for k := range inServer {
		if !inSpec[k] {
			t.Errorf("route %s exists but docs/openapi.yaml does not describe it", k)
		}
	}
	for k := range inSpec {
		if !inServer[k] {
			t.Errorf("docs/openapi.yaml describes %s but the server has no such route", k)
		}
	}
	if len(inServer) != 19 {
		t.Errorf("expected 19 public operations, found %d: update docs/public-portal/AUDIT.md", len(inServer))
	}
}

func TestPublicOpenAPIIsWellFormed(t *testing.T) {
	_, g := loadSpec(t)
	ids := map[string]string{}
	var refs []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if r, ok := x["$ref"].(string); ok {
				refs = append(refs, r)
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(g)
	for _, r := range refs {
		parts := strings.Split(strings.TrimPrefix(r, "#/"), "/")
		if dig(g, parts...) == nil {
			t.Errorf("unresolved reference %s", r)
		}
	}
	for p, item := range g["paths"].(map[string]any) {
		for m, op := range item.(map[string]any) {
			if !httpMethods[m] {
				continue
			}
			o := op.(map[string]any)
			id, _ := o["operationId"].(string)
			if id == "" {
				t.Errorf("%s %s has no operationId", m, p)
			}
			if prev, dup := ids[id]; dup {
				t.Errorf("operationId %s used by %s and %s %s", id, prev, m, p)
			}
			ids[id] = m + " " + p
			if o["summary"] == nil || o["responses"] == nil {
				t.Errorf("%s %s needs a summary and responses", m, p)
			}
			if strings.HasPrefix(p, "/v1/emails") || strings.HasPrefix(p, "/v1/inbound") || strings.HasPrefix(p, "/v1/suppressions") {
				if o["x-required-scopes"] == nil || o["x-error-codes"] == nil {
					t.Errorf("%s %s must list x-required-scopes and x-error-codes", m, p)
				}
			}
		}
	}
	if !strings.Contains(g["info"].(map[string]any)["description"].(string), "no `delivered` state") {
		t.Error("the spec must state that there is no delivered state")
	}
	for _, s := range g["servers"].([]any) {
		u := s.(map[string]any)["url"].(string)
		if !strings.Contains(u, "example.com") {
			t.Errorf("server URL %q must be a placeholder, never a real or hosted endpoint", u)
		}
	}
}

func goSource(t *testing.T, names ...string) string {
	t.Helper()
	var b strings.Builder
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join("..", n))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
	}
	return b.String()
}

func TestPublicOpenAPIScopesStatesAndCodesMatchSource(t *testing.T) {
	_, g := loadSpec(t)
	svc := goSource(t, "internal/service/service.go")
	var codeScopes []string
	for _, m := range regexp.MustCompile(`Scope[A-Za-z]* += "([a-z_]+:[a-z_]+)"`).FindAllStringSubmatch(svc, -1) {
		codeScopes = append(codeScopes, m[1])
	}
	sort.Strings(codeScopes)
	desc := dig(g, "components", "securitySchemes", "bearer", "description").(string)
	for _, sc := range codeScopes {
		if !strings.Contains(desc, sc) {
			t.Errorf("scope %s exists in source but the security scheme does not list it", sc)
		}
	}
	used := map[string]bool{}
	for _, item := range g["paths"].(map[string]any) {
		for m, op := range item.(map[string]any) {
			if httpMethods[m] {
				for _, s := range strs(op.(map[string]any)["x-required-scopes"]) {
					for _, sc := range regexp.MustCompile(`[a-z_]+:[a-z_]+`).FindAllString(s, -1) {
						used[sc] = true
					}
				}
			}
		}
	}
	for sc := range used {
		if !inList(codeScopes, sc) {
			t.Errorf("spec requires unknown scope %s", sc)
		}
	}

	// Message states: the database constraint is the source of truth.
	mig, _ := os.ReadFile(filepath.Join("..", "internal", "db", "migrations", "0003_complete.sql"))
	m := regexp.MustCompile(`state IN \(([^)]*)\)`).FindStringSubmatch(string(mig))
	if m == nil {
		t.Fatal("state constraint not found")
	}
	var states []string
	for _, s := range strings.Split(m[1], ",") {
		states = append(states, strings.Trim(strings.TrimSpace(s), "'"))
	}
	sort.Strings(states)
	for _, name := range []string{"Status", "Accepted"} {
		got := strs(dig(g, "components", "schemas", name, "properties", "status", "enum"))
		if !reflect.DeepEqual(got, states) {
			t.Errorf("%s.status enum %v differs from the database states %v", name, got, states)
		}
	}
	for _, s := range states {
		if s == "delivered" {
			t.Error("a delivered state must never exist")
		}
	}

	// Error codes: everything the spec lists exists in source, and every code
	// the public paths can return is listed somewhere in the spec.
	src := goSource(t, "internal/service/accept.go", "internal/service/auth.go", "internal/service/status.go", "internal/service/claims.go",
		"internal/service/product.go", "internal/service/adminviews.go", "internal/service/templates.go", "internal/api/api.go", "internal/tmpl/tmpl.go",
		"internal/service/verifyaddr.go", "internal/service/ingest.go")
	known := map[string]bool{}
	for _, r := range []*regexp.Regexp{regexp.MustCompile(`invalid\("([a-z_]+)"\)`), regexp.MustCompile(`newErr\(\d+, "([a-z_]+)"\)`), regexp.MustCompile(`Code: "([a-z_]+)"`), regexp.MustCompile(`bad\("([a-z_]+)"\)`)} {
		for _, mm := range r.FindAllStringSubmatch(src, -1) {
			known[mm[1]] = true
		}
	}
	listed := map[string]bool{}
	for _, item := range g["paths"].(map[string]any) {
		for m, op := range item.(map[string]any) {
			if httpMethods[m] {
				for _, c := range strs(op.(map[string]any)["x-error-codes"]) {
					listed[c] = true
					if !known[c] {
						t.Errorf("the spec lists error code %q but no handler returns it", c)
					}
				}
			}
		}
	}
	// Codes that are not API errors a client sees, or that are documented elsewhere.
	ignore := map[string]bool{"internal_error": true, "forbidden": true, "unauthenticated": true, "body_too_large": true}
	accept := goSource(t, "internal/service/accept.go", "internal/tmpl/tmpl.go")
	for _, r := range []*regexp.Regexp{regexp.MustCompile(`invalid\("([a-z_]+)"\)`), regexp.MustCompile(`newErr\(\d+, "([a-z_]+)"\)`), regexp.MustCompile(`Code: "([a-z_]+)"`), regexp.MustCompile(`bad\("([a-z_]+)"\)`)} {
		for _, mm := range r.FindAllStringSubmatch(accept, -1) {
			if !listed[mm[1]] && !ignore[mm[1]] {
				t.Errorf("accept.go can return %q but docs/openapi.yaml does not list it", mm[1])
			}
		}
	}
}

func jsonTags(tp reflect.Type) []string {
	var out []string
	for i := 0; i < tp.NumField(); i++ {
		tag := strings.Split(tp.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

func propNames(g map[string]any, schema string) []string {
	p, _ := dig(g, "components", "schemas", schema, "properties").(map[string]any)
	var out []string
	for k := range p {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestPublicOpenAPISchemasMatchGoTypes(t *testing.T) {
	_, g := loadSpec(t)
	for schema, typ := range map[string]reflect.Type{
		"SendRequest": reflect.TypeOf(service.SendRequest{}),
		"Attachment":  reflect.TypeOf(service.AttachmentIn{}),
		"Accepted":    reflect.TypeOf(service.Accepted{}),
		"Status":      reflect.TypeOf(service.Status{}),
	} {
		if got, want := propNames(g, schema), jsonTags(typ); !reflect.DeepEqual(got, want) {
			t.Errorf("schema %s has %v; the Go type has %v", schema, got, want)
		}
	}
	if got, want := propNames(g, "BatchResponse"), []string{"results"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BatchResponse: %v", got)
	}
	items := dig(g, "components", "schemas", "BatchResponse", "properties", "results", "items", "properties").(map[string]any)
	var got []string
	for k := range items {
		got = append(got, k)
	}
	sort.Strings(got)
	if want := jsonTags(reflect.TypeOf(service.BatchResult{})); !reflect.DeepEqual(got, want) {
		t.Errorf("batch result: %v vs %v", got, want)
	}
	if got, want := strs(dig(g, "components", "schemas", "SendRequest", "required")), []string{"from", "to"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SendRequest.required %v", got)
	}
}

// validate checks a decoded response against a component schema: no property the
// schema does not declare, every required one present, enums respected.
func validate(t *testing.T, g map[string]any, schema string, v any, where string) {
	t.Helper()
	s, _ := dig(g, "components", "schemas", schema).(map[string]any)
	if s == nil {
		t.Fatalf("no schema %s", schema)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Errorf("%s: not an object: %v", where, v)
		return
	}
	props, _ := s["properties"].(map[string]any)
	for k, val := range obj {
		p, ok := props[k].(map[string]any)
		if !ok {
			t.Errorf("%s: response field %q is not in schema %s", where, k, schema)
			continue
		}
		if en := strs(p["enum"]); len(en) > 0 {
			if sv, isStr := val.(string); isStr && !inList(en, sv) {
				t.Errorf("%s: %s=%q not in enum %v", where, k, sv, en)
			}
		}
	}
	for _, r := range strs(s["required"]) {
		if _, ok := obj[r]; !ok {
			t.Errorf("%s: required field %q missing from %v", where, r, obj)
		}
	}
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// The documented examples are sent to a real handler, and the real responses
// are checked against the published schemas.
func TestPublicOpenAPIExamplesAndResponsesMatchTheHandlers(t *testing.T) {
	e := setup(t)
	_, g := loadSpec(t)
	k := e.provision("docs")
	ctx := context.Background()
	supKey, _, err := e.svc.CreateKey(ctx, "docs", "live", []string{"suppressions:manage", "inbound:read", "emails:read", "emails:send"}, "docs", 0)
	e.must(err)

	ex := dig(g, "paths", "/v1/emails", "post", "requestBody", "content", "application/json", "examples", "otp", "value").(map[string]any)
	ex["from"] = liveFrom // the example's sender is a placeholder; ours is the approved one
	r := e.do("POST", "/v1/emails", k.live, "docs-otp-0001", ex)
	if r.Code != 202 {
		t.Fatalf("documented OTP example: %d %s", r.Code, r.Raw)
	}
	validate(t, g, "Accepted", r.JSON, "POST /v1/emails")
	id := str(r.JSON, "id")
	if again := e.do("POST", "/v1/emails", k.live, "docs-otp-0001", ex); again.Code != 202 || str(again.JSON, "id") != id {
		t.Fatalf("identical replay must return the same id: %d %s", again.Code, again.Raw)
	}
	changed := map[string]any{}
	for kk, v := range ex {
		changed[kk] = v
	}
	changed["variables"] = map[string]any{"name": "Ada", "code": "000000", "expires_in_minutes": 5}
	if c := e.do("POST", "/v1/emails", k.live, "docs-otp-0001", changed); c.Code != 409 || errCode(c) != "idempotency_conflict" {
		t.Fatalf("same key, different body: %d %s", c.Code, c.Raw)
	}
	if bad := e.do("POST", "/v1/emails", k.live, "docs-otp-0002", map[string]any{"from": liveFrom, "to": testRcpt, "surprise": 1}); bad.Code != 422 || errCode(bad) != "invalid_request" {
		t.Fatalf("unknown fields must be rejected: %d %s", bad.Code, bad.Raw)
	}
	if bad := e.do("POST", "/v1/emails", "", "docs-otp-0003", ex); bad.Code != 401 || !strings.Contains(bad.Hdr.Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("no key: %d %v", bad.Code, bad.Hdr)
	}
	validate(t, g, "Error", map[string]any{"error": map[string]any{"code": "x"}}, "error example") // schema sanity

	st := e.do("GET", "/v1/emails/"+id, k.live, "", nil)
	if st.Code != 200 {
		t.Fatalf("status: %d %s", st.Code, st.Raw)
	}
	validate(t, g, "Status", st.JSON, "GET /v1/emails/{id}")
	if strings.Contains(st.Raw, testRcpt) || strings.Contains(st.Raw, "482913") {
		t.Fatal("the status response must never contain the recipient or variables")
	}
	if r := e.do("GET", "/v1/emails/"+id, k.sbx, "", nil); r.Code != 404 {
		t.Fatalf("another environment's message must be a 404: %d", r.Code)
	}

	batch := dig(g, "paths", "/v1/emails/batch", "post", "requestBody", "content", "application/json", "example").(map[string]any)
	msgs := batch["messages"].([]any)
	m0 := msgs[0].(map[string]any)
	m0["from"], m0["to"] = liveFrom, testRcpt
	br := e.do("POST", "/v1/emails/batch", k.live, "", batch)
	if br.Code != 200 {
		t.Fatalf("batch example: %d %s", br.Code, br.Raw)
	}
	validate(t, g, "BatchResponse", br.JSON, "POST /v1/emails/batch")
	for _, it := range br.JSON["results"].([]any) {
		res := it.(map[string]any)
		itemSchema := dig(g, "components", "schemas", "BatchResponse", "properties", "results", "items", "properties").(map[string]any)
		for key := range res {
			if _, ok := itemSchema[key]; !ok {
				t.Errorf("batch result field %q undocumented", key)
			}
		}
	}

	// Cancel a scheduled message, then a second cancel is not_cancellable.
	sched := map[string]any{"from": liveFrom, "to": testRcpt, "template": "otp", "variables": map[string]any{"code": "123456", "expires_in_minutes": 5},
		"send_at": nowPlus(3600)}
	sr := e.do("POST", "/v1/emails", k.live, "docs-sched-0001", sched)
	if sr.Code != 202 || sr.JSON["status"] != "scheduled" {
		t.Fatalf("scheduled send: %d %s", sr.Code, sr.Raw)
	}
	cr := e.do("POST", "/v1/emails/"+str(sr.JSON, "id")+"/cancel", k.live, "", nil)
	if cr.Code != 200 || cr.JSON["status"] != "cancelled" {
		t.Fatalf("cancel: %d %s", cr.Code, cr.Raw)
	}
	if c2 := e.do("POST", "/v1/emails/"+str(sr.JSON, "id")+"/cancel", k.live, "", nil); c2.Code != 409 || errCode(c2) != "not_cancellable" {
		t.Fatalf("second cancel: %d %s", c2.Code, c2.Raw)
	}

	// Suppressions.
	sp := e.do("POST", "/v1/suppressions", supKey, "", map[string]any{"email": "blocked@example.org", "reason": "user request"})
	if sp.Code != 200 || str(sp.JSON, "id") == "" {
		t.Fatalf("suppress: %d %s", sp.Code, sp.Raw)
	}
	ls := e.do("GET", "/v1/suppressions", supKey, "", nil)
	if ls.Code != 200 {
		t.Fatalf("list: %d %s", ls.Code, ls.Raw)
	}
	for _, it := range ls.JSON["items"].([]any) {
		for key := range it.(map[string]any) {
			if _, ok := dig(g, "components", "schemas", "SuppressionPage", "allOf").([]any)[1].(map[string]any)["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)[key]; !ok {
				t.Errorf("suppression field %q undocumented", key)
			}
		}
	}
	if strings.Contains(ls.Raw, "blocked@example.org") {
		t.Fatal("suppressed addresses must be masked")
	}
	for _, key := range []string{"has_more", "limit", "offset", "items"} {
		if _, ok := ls.JSON[key]; !ok {
			t.Errorf("page envelope missing %s", key)
		}
	}
	if del := e.do("DELETE", "/v1/suppressions/blocked@example.org", supKey, "", nil); del.Code != 200 || del.JSON["ok"] != true {
		t.Fatalf("delete by address: %d %s", del.Code, del.Raw)
	}
	if in := e.do("GET", "/v1/inbound", supKey, "", nil); in.Code != 200 || in.JSON["items"] == nil {
		t.Fatalf("inbound list: %d %s", in.Code, in.Raw)
	}
	// Scope enforcement matches the spec: a send-only key cannot read, and a read key cannot send.
	if r := e.do("GET", "/v1/suppressions", k.live, "", nil); r.Code != 403 || errCode(r) != "missing_scope" {
		t.Fatalf("scope: %d %s", r.Code, r.Raw)
	}
	if r := e.do("POST", "/v1/emails", k.liveRead, "docs-ro-00001", ex); r.Code != 403 || errCode(r) != "missing_scope" {
		t.Fatalf("read-only key sent mail: %d %s", r.Code, r.Raw)
	}

	for _, h := range []struct{ path, status string }{{"/health/live", "ok"}, {"/health/ready", "ready"}} {
		r := e.do("GET", h.path, "", "", nil)
		if r.Code != 200 || r.JSON["status"] != h.status {
			t.Fatalf("%s: %d %s", h.path, r.Code, r.Raw)
		}
		validate(t, g, "Health", r.JSON, h.path)
	}
}

func nowPlus(sec int) string {
	return time.Now().Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339)
}
