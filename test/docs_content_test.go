package test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// The public documentation (documentation/content/docs) makes claims. These tests tie
// the claims to the running code, so the portal cannot publish a contradiction.

const docsDir = "../documentation/content/docs"

type fence struct {
	file, lang string
	meta       []string
	code       string
	line       int
}

func docFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".mdx") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) < 20 {
		t.Fatalf("only %d documentation pages found: is the content tree intact?", len(files))
	}
	return files
}

var fenceRe = regexp.MustCompile("(?m)^```([^\n]*)\n((?s:.*?))^```[ \t]*$")

func fencesOf(t *testing.T, file string) []fence {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out []fence
	for _, m := range fenceRe.FindAllStringSubmatchIndex(string(raw), -1) {
		head := strings.Fields(string(raw[m[2]:m[3]]))
		f := fence{file: filepath.ToSlash(strings.TrimPrefix(file, docsDir+"/")), code: strings.TrimSuffix(string(raw[m[4]:m[5]]), "\n"), line: 1 + strings.Count(string(raw[:m[0]]), "\n")}
		if len(head) > 0 {
			f.lang, f.meta = head[0], head[1:]
		}
		out = append(out, f)
	}
	return out
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type curl struct {
	method, path string
	headers      map[string]string
	body         string
}

var (
	curlMethod = regexp.MustCompile(`-X\s+([A-Z]+)`)
	curlURL    = regexp.MustCompile(`"\$NJIWA_MAIL_URL([^"]*)"`)
	curlHeader = regexp.MustCompile(`-H\s+"([^"]+)"`)
	curlHere   = regexp.MustCompile(`(?s)<<'(\w+)'\n(.*?)\n(\w+)\s*$`)
	curlData   = regexp.MustCompile(`-d\s+'([^']*)'`)
)

func parseCurl(t *testing.T, f fence) curl {
	t.Helper()
	flat := strings.ReplaceAll(f.code, "\\\n", " ")
	first := strings.SplitN(flat, "\n", 2)[0]
	c := curl{method: "GET", headers: map[string]string{}}
	if m := curlMethod.FindStringSubmatch(first); m != nil {
		c.method = m[1]
	} else if strings.Contains(first, " -d ") {
		c.method = "POST"
	}
	m := curlURL.FindStringSubmatch(first)
	if m == nil {
		t.Fatalf("%s:%d: curl must target \"$NJIWA_MAIL_URL/…\"", f.file, f.line)
	}
	c.path = m[1]
	for _, h := range curlHeader.FindAllStringSubmatch(first, -1) {
		k, v, _ := strings.Cut(h[1], ": ")
		c.headers[k] = v
	}
	if m := curlHere.FindStringSubmatch(flat); m != nil && m[1] == m[3] {
		c.body = m[2]
	} else if m := curlData.FindStringSubmatch(first); m != nil {
		c.body = m[1]
	}
	return c
}

// Every curl-check snippet in the docs is sent to a real handler with a real key, in page
// order, and the real answer must match what the docs and the published schema say.
func TestDocumentedCurlSnippetsWorkAgainstTheHandlers(t *testing.T) {
	e := setup(t)
	_, g := loadSpec(t)
	ctx := context.Background()
	const sender = "noreply@notify.example.com"
	e.provisionFrom("docs", sender)
	e.must(e.svc.SetOrigins(ctx, "docs", "sandbox", []string{"https://app.example.com"}))
	key, _, err := e.svc.CreateKey(ctx, "docs", "sandbox", []string{"emails:send", "emails:send_raw", "emails:read", "inbound:read", "suppressions:manage"}, "docs-replay", 0)
	e.must(err)

	n, seq := 0, 0
	freshID := func() string {
		seq++
		r := e.do("POST", "/v1/emails", key, "docs-fixture-"+strconv.Itoa(seq)+"-0000", map[string]any{
			"from": sender, "to": "fixture@example.org", "template": "otp", "variables": map[string]any{"code": "111111", "expires_in_minutes": 5}})
		if r.Code != 202 {
			t.Fatalf("fixture message: %d %s", r.Code, r.Raw)
		}
		return str(r.JSON, "id")
	}
	for _, file := range docFiles(t) {
		for _, f := range fencesOf(t, file) {
			if !has(f.meta, "curl-check") {
				continue
			}
			n++
			c := parseCurl(t, f)
			path := c.path
			if strings.Contains(path, "$MESSAGE_ID") {
				path = strings.ReplaceAll(path, "$MESSAGE_ID", freshID())
			}
			if strings.Contains(path, "$") {
				t.Fatalf("%s:%d: unknown variable in %s", f.file, f.line, path)
			}
			if c.headers["Authorization"] != "Bearer $NJIWA_MAIL_API_KEY" {
				t.Errorf("%s:%d: the key must be sent as Authorization: Bearer $NJIWA_MAIL_API_KEY", f.file, f.line)
			}
			idemKey := ""
			if v, ok := c.headers["Idempotency-Key"]; ok {
				if v != "$(uuidgen)" {
					t.Errorf("%s:%d: use $(uuidgen) for the example idempotency key", f.file, f.line)
				}
				idemKey = "docs-replay-" + strconv.Itoa(n) + "-0000"
			}
			var body any
			if c.body != "" {
				body = []byte(c.body)
			}
			r := e.do(c.method, path, key, idemKey, body)
			want := 200
			if c.method == "POST" && path == "/v1/emails" {
				want = 202
			}
			if r.Code != want {
				t.Errorf("%s:%d: %s %s answered %d (want %d): %s", f.file, f.line, c.method, path, r.Code, want, r.Raw)
				continue
			}
			switch {
			case c.method == "POST" && path == "/v1/emails":
				validate(t, g, "Accepted", r.JSON, f.file)
			case c.method == "POST" && path == "/v1/emails/batch":
				validate(t, g, "BatchResponse", r.JSON, f.file)
				for _, it := range r.JSON["results"].([]any) {
					if it.(map[string]any)["status"] != float64(202) {
						t.Errorf("%s:%d: a documented batch item was rejected: %v", f.file, f.line, it)
					}
				}
			case c.method == "GET" && strings.HasPrefix(path, "/v1/emails/"):
				validate(t, g, "Status", r.JSON, f.file)
			case c.method == "POST" && strings.HasSuffix(path, "/cancel"):
				if r.JSON["status"] != "cancelled" {
					t.Errorf("%s:%d: cancel answered %s", f.file, f.line, r.Raw)
				}
			}
		}
	}
	if n < 7 {
		t.Fatalf("only %d curl-check snippets were replayed: did the docs lose their examples?", n)
	}
	t.Logf("%d documented curl snippets replayed against the handlers", n)
}

// Go snippets in the docs must compile (go vet type-checks them against the real client).
func TestDocumentedGoSnippetsCompile(t *testing.T) {
	root := filepath.Join("..", "documentation", "zz_snippets_tmp")
	_ = os.RemoveAll(root)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	n := 0
	for _, file := range docFiles(t) {
		for _, f := range fencesOf(t, file) {
			if f.lang == "go" && has(f.meta, "check") {
				dir := filepath.Join(root, "s"+strconv.Itoa(n))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(f.code+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				n++
			}
		}
	}
	if n < 3 {
		t.Fatalf("only %d checked Go snippets", n)
	}
	cmd := exec.Command("go", "vet", "./documentation/zz_snippets_tmp/...")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("documented Go code does not compile:\n%s", out)
	}
}

var codeCell = regexp.MustCompile("^\\|\\s*`([^`]+)`")

func tableCodes(page string) []string {
	var out []string
	for _, line := range strings.Split(page, "\n") {
		if m := codeCell.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func TestDocsWebhookEventsMatchTheSource(t *testing.T) {
	var docs []string
	for _, c := range tableCodes(readDoc(t, "webhooks.mdx")) {
		if strings.HasPrefix(c, "email.") || strings.HasPrefix(c, "inbound.") {
			docs = append(docs, c)
		}
	}
	src := append([]string(nil), service.WebhookEvents...)
	sort.Strings(docs)
	sort.Strings(src)
	if strings.Join(docs, ",") != strings.Join(src, ",") {
		t.Fatalf("webhooks.mdx lists %v; the service supports %v", docs, src)
	}
	if strings.Contains(readDoc(t, "webhooks.mdx"), "`email.delivered`") && !strings.Contains(readDoc(t, "webhooks.mdx"), "no `email.delivered`") {
		t.Fatal("there is no email.delivered event")
	}
}

func TestDocsScopesStatesAndErrorsMatchTheSource(t *testing.T) {
	_, g := loadSpec(t)
	// Scopes.
	svc := goSource(t, "internal/service/service.go")
	var scopes []string
	for _, m := range regexp.MustCompile(`Scope[A-Za-z]* += "([a-z_]+:[a-z_]+)"`).FindAllStringSubmatch(svc, -1) {
		scopes = append(scopes, m[1])
	}
	auth := readDoc(t, "authentication.mdx")
	for _, s := range scopes {
		if !strings.Contains(auth, "`"+s+"`") {
			t.Errorf("authentication.mdx does not list scope %s", s)
		}
	}
	for _, c := range tableCodes(auth) {
		if strings.Contains(c, ":") && !has(scopes, c) {
			t.Errorf("authentication.mdx lists a scope that does not exist: %s", c)
		}
	}
	// States.
	mig, _ := os.ReadFile(filepath.Join("..", "internal", "db", "migrations", "0003_complete.sql"))
	m := regexp.MustCompile(`state IN \(([^)]*)\)`).FindStringSubmatch(string(mig))
	var states []string
	for _, s := range strings.Split(m[1], ",") {
		states = append(states, strings.Trim(strings.TrimSpace(s), "'"))
	}
	st := readDoc(t, "api/status-and-cancel.mdx")
	got := tableCodes(st)
	for _, s := range states {
		if !has(got, s) {
			t.Errorf("status-and-cancel.mdx does not describe state %s", s)
		}
	}
	for _, s := range got {
		if !has(states, s) {
			t.Errorf("status-and-cancel.mdx describes a state that does not exist: %s", s)
		}
	}
	// Error codes: every code the spec lists is in the errors page, and the page invents none.
	page := readDoc(t, "api/rate-limits-and-errors.mdx")
	pageCodes := tableCodes(page)
	src := goSource(t, "internal/service/accept.go", "internal/service/auth.go", "internal/service/status.go", "internal/service/claims.go", "internal/service/product.go",
		"internal/service/adminviews.go", "internal/tmpl/tmpl.go", "internal/api/api.go", "internal/service/verifyaddr.go")
	known := map[string]bool{"body_too_large": true}
	for _, r := range []*regexp.Regexp{regexp.MustCompile(`invalid\("([a-z_]+)"\)`), regexp.MustCompile(`newErr\(\d+, "([a-z_]+)"\)`), regexp.MustCompile(`Code: "([a-z_]+)"`), regexp.MustCompile(`bad\("([a-z_]+)"\)`)} {
		for _, mm := range r.FindAllStringSubmatch(src, -1) {
			known[mm[1]] = true
		}
	}
	for _, c := range pageCodes {
		if !known[c] {
			t.Errorf("the errors page lists %q, which no handler returns", c)
		}
	}
	for _, item := range g["paths"].(map[string]any) {
		for mth, op := range item.(map[string]any) {
			if httpMethods[mth] {
				for _, c := range strs(op.(map[string]any)["x-error-codes"]) {
					if !has(pageCodes, c) {
						t.Errorf("openapi.yaml lists error code %s but the errors page does not explain it", c)
					}
				}
			}
		}
	}
}

// Documentation and the agent skill may only name operations that exist.
func TestDocsAndSkillNameOnlyRealEndpoints(t *testing.T) {
	_, g := loadSpec(t)
	var templates []*regexp.Regexp
	for p, item := range g["paths"].(map[string]any) {
		for m := range item.(map[string]any) {
			if httpMethods[m] {
				templates = append(templates, regexp.MustCompile("^"+strings.ToUpper(m)+" "+regexp.MustCompile(`\\\{[^\\]+\\\}`).ReplaceAllString(regexp.QuoteMeta(p), "[^/]+")+"$"))
			}
		}
	}
	ref := regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\s+` + "`?" + `(/(?:v1|health|admin)[A-Za-z0-9_\-/{}.$]*)`)
	var files []string
	files = append(files, docFiles(t)...)
	_ = filepath.WalkDir(filepath.Join("..", "skills"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return nil
	})
	seen := 0
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		text := string(raw)
		if strings.Contains(f, "skills") && strings.Contains(text, "/admin") {
			t.Errorf("%s mentions the operator API; the skill must only use the public API", f)
		}
		for _, m := range ref.FindAllStringSubmatch(text, -1) {
			seen++
			path := strings.SplitN(m[2], "?", 2)[0]
			path = regexp.MustCompile(`/\$[A-Z_]+`).ReplaceAllString(path, "/x")
			ok := false
			for _, re := range templates {
				if re.MatchString(m[1] + " " + path) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s names %s %s, which is not a public operation", filepath.Base(f), m[1], m[2])
			}
		}
	}
	if seen < 20 {
		t.Fatalf("found only %d endpoint mentions: is the scan broken?", seen)
	}
}

// Plain-text rules that protect against claims the product cannot keep.
func TestDocsNeverClaimDeliveryOrRequireMailcow(t *testing.T) {
	for _, f := range docFiles(t) {
		raw, _ := os.ReadFile(f)
		text := string(raw)
		for _, bad := range []string{"ditronics", "njiwa.co.tz"} {
			if strings.Contains(strings.ToLower(text), bad) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
		if regexp.MustCompile(`(?i)(requires?|must use|needs?)\s+mailcow`).MatchString(text) {
			t.Errorf("%s makes Mailcow a requirement", f)
		}
	}
}
