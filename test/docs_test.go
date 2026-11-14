package test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

// A01: docs/ENDPOINTS.md is generated from the route table and the live schema,
// so it cannot drift. `UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc`
// rewrites it; without the variable the test fails when the file is stale.

var tableDocs = map[string]string{
	"alert_channels":        "Where operator alerts are sent (webhook or email), per alert kind.",
	"alerts":                "Operator alerts (policy bounces, disabled webhooks, failing checks) with acknowledgement.",
	"api_keys":              "API keys per project environment: prefix, secret hash, scopes, IP allowlist, expiry, revocation.",
	"audit_log":             "Append-only record of every change made by a person, an API key or the CLI.",
	"automation_rules":      "Rules that act on inbound mail (auto-reply, forward, webhook, archive), ordered.",
	"automation_runs":       "One row per rule evaluation, with the outcome.",
	"autoreply_log":         "Limits auto-replies to one per sender per day.",
	"blocked_senders":       "Inbound senders an operator blocked in an environment.",
	"bounces":               "Bounce and complaint reports, correlated to a message by signed return path or header.",
	"bulk_unsubscribes":     "Recipients who unsubscribed from bulk announcements.",
	"campaign_recipients":   "Recipients of a bulk announcement (address sealed, keyed hash for lookups).",
	"campaigns":             "Bulk announcements: template, schedule, state, health counters.",
	"delivery_events":       "Delivery evidence per message: SMTP reply, mail server log, bounce report, operator note.",
	"dns_checks":            "History of DNS check results per record.",
	"dns_connections":       "A project's connection to a DNS host (Cloudflare): sealed credential, status, expiry. Never returned by any API.",
	"dns_oauth_states":      "Single-use, hashed OAuth state with a sealed PKCE verifier, bound to the user who started the flow.",
	"dns_setups":            "One DNS setup run for a domain: zone, reviewed plan and digest, approval, status.",
	"dns_changes":           "Append-only ledger of DNS record changes: previous and new value, actor, outcome (used for rollback).",
	"domain_claims":         "Installation-wide ownership of a sending domain: one project holds a name.",
	"domains":               "Sending and receiving domains of a project with DNS check results, DKIM proof and return-path domain.",
	"drafts":                "Compose drafts (content sealed).",
	"environments":          "Sandbox and Live environment of a project: enabled flag, approved origins, recipient allowlist.",
	"inbound_addresses":     "Addresses that receive mail into a project (mailboxes, temporary inboxes, catch-all).",
	"inbound_attachments":   "Metadata of inbound attachments; the files are sealed on disk.",
	"monthly_usage":         "Live messages accepted per project and UTC month, for the plan's monthly quota.",
	"usage_plans":           "Named sets of project limits (sending, bulk, monthly messages, contacts).",
	"announcements":         "Operator announcements shown in every user's notifications until retracted.",
	"daily_reports":         "One summary per UTC day (all projects), shown in every user's notifications.",
	"inbox_state":           "Per-user read and dismissed state of notification items.",
	"campaign_links":        "Links of a click-tracked campaign by position; a click redirects only to these.",
	"contact_list_members":  "Contacts on a subscriber list, with a list-scoped unsubscribe time.",
	"contact_lists":         "Subscriber lists (static audiences) per environment.",
	"contact_segments":      "Dynamic audiences: rules on status, lists, domain and attributes.",
	"contacts":              "Marketing contacts per environment: address and name encrypted, keyed hash, masked form, attributes.",
	"tracking_events":       "Opens and clicks of tracked campaigns (no IP address or user agent).",
	"inbound_ingest":        "Per-environment secret (encrypted) for signed HTTP ingest of inbound mail.",
	"inbound_messages":      "Parsed inbound mail (text and HTML sealed) with retention timestamps.",
	"inbound_spool":         "Raw inbound mail accepted by the listener, waiting to be parsed or correlated as bounces.",
	"installation_checks":   "Connection and readiness check results bound to a transport profile revision.",
	"installation_settings": "Single row: confirmed delivery mode, previous profile, DKIM rotation state.",
	"installer_receipts":    "What the host installer did: plan digest, approver, result, rollback state.",
	"mail_server":           "Legacy single mail server row, mirrored from the active existing-SMTP profile.",
	"message_attachments":   "Attachments of outbound messages (sealed).",
	"message_events":        "State changes of a message (queued, submitted, bounced, resolved…).",
	"messages":              "Every outbound message: state machine, payload (sealed), recipient hash, transport pin, queue id.",
	"password_resets":       "Single-use tokens for admin password reset (hashed).",
	"projects":              "Projects, with send limits.",
	"rate_limit_buckets":    "Sliding-window counters for rate limits and login throttling.",
	"sandbox_captures":      "Sandbox messages captured instead of sent (sealed, short-lived).",
	"schema_migrations":     "Applied SQL migrations.",
	"sender_identities":     "Addresses an environment may send from.",
	"sent_copies":           "Copy of what an operator composed (sealed).",
	"sessions":              "Admin sessions (token hash, IP, user agent, expiry).",
	"settings":              "Operator-tunable settings (retention, limits, require_mfa…).",
	"suppressions":          "Recipients mail is not sent to (hard bounce, complaint, manual).",
	"template_contents":     "Localised content of a template version.",
	"template_versions":     "Draft and published versions of a template.",
	"templates":             "Reusable message templates with variables.",
	"transport_profiles":    "Ways of getting mail out: managed Postfix or an existing SMTP server (secrets sealed), one active.",
	"users":                 "Admin users: password hash, installation-operator flag, TOTP state (secret sealed), recovery code hashes.",
	"webhook_deliveries":    "Outbox of webhook events with retry state and schema version.",
	"web_views":             "Encrypted \"view in browser\" copies of sent HTML, deleted with the message or after 30 days.",
	"webhook_endpoints":     "Webhook endpoints per environment (signing secret sealed, schema version).",
	"worker_heartbeats":     "Liveness of worker, scheduler and inbound processes.",
}

func routeAuth(path string) string {
	switch {
	case strings.HasPrefix(path, "/health/"), path == "/metrics":
		return "public / token"
	case strings.HasPrefix(path, "/v1/unsubscribe/"), path == "/admin/v1/auth/login", path == "/admin/v1/auth/forgot", path == "/admin/v1/auth/reset", path == "/console/login":
		return "public"
	case strings.HasPrefix(path, "/v1/"):
		return "API key"
	case strings.HasPrefix(path, "/console/"):
		return "console session"
	case strings.HasPrefix(path, "/admin/v1/installation"), strings.HasPrefix(path, "/admin/v1/domain-claims"), strings.HasPrefix(path, "/admin/v1/mail-server"),
		strings.HasPrefix(path, "/admin/v1/users"), strings.HasPrefix(path, "/admin/v1/mta"):
		return "admin session, operator"
	}
	return "admin session"
}

func TestEndpointsAndSchemaDoc(t *testing.T) {
	e := setup(t)
	ctx := t.Context()

	type route struct{ method, path string }
	seen := map[route]bool{}
	var routes []route
	for _, r := range e.h.GetRoutes(true) {
		if r.Method == "HEAD" || r.Method == "OPTIONS" || r.Method == "USE" {
			continue
		}
		p := paramRe.ReplaceAllStringFunc(r.Path, func(s string) string { return "{" + s[1:] + "}" })
		rt := route{r.Method, p}
		if !seen[rt] {
			seen[rt] = true
			routes = append(routes, rt)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})

	var b strings.Builder
	b.WriteString("# Endpoints and schema\n\n")
	b.WriteString("Generated by `UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc`; a test fails when this file is stale. ")
	b.WriteString("Request and response shapes: [openapi.yaml](openapi.yaml) (application API) and [openapi-admin.yaml](openapi-admin.yaml) (admin API).\n\n")
	b.WriteString("## Routes\n\n")
	groups := []struct{ title, prefix string }{
		{"Application API (`/v1`, API key)", "/v1/"},
		{"Admin API: installation, profiles, claims, mail server, users, queue (operators)", "/admin/v1/"},
		{"Console (legacy)", "/console/"},
		{"Health and metrics", ""},
	}
	used := map[route]bool{}
	section := func(title string, match func(route) bool) {
		fmt.Fprintf(&b, "### %s\n\n| Method | Path | Access |\n|---|---|---|\n", title)
		n := 0
		for _, r := range routes {
			if used[r] || !match(r) {
				continue
			}
			used[r] = true
			n++
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", r.method, r.path, routeAuth(r.path))
		}
		fmt.Fprintf(&b, "\n%d routes.\n\n", n)
	}
	section("Admin API: workspace (per project and mode)", func(r route) bool { return strings.HasPrefix(r.path, "/admin/v1/w/{project}/{mode}") })
	for _, g := range groups[:3] {
		g := g
		section(g.title, func(r route) bool { return strings.HasPrefix(r.path, g.prefix) })
	}
	section(groups[3].title, func(r route) bool { return true })

	// Schema.
	b.WriteString("## Tables\n\n`(sealed)` columns hold AES-GCM ciphertext (see `njiwa-mail rekey`); `*_hash` columns are keyed HMACs or SHA-256 digests.\n\n")
	rows, err := e.pool.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name, ordinal_position`)
	e.must(err)
	sealed := map[string]bool{}
	for _, c := range service.SealedColumnNames() {
		sealed[c] = true
	}
	cols := map[string][]string{}
	var tables []string
	for rows.Next() {
		var tbl, col string
		e.must(rows.Scan(&tbl, &col))
		if _, ok := cols[tbl]; !ok {
			tables = append(tables, tbl)
		}
		if sealed[tbl+"."+col] {
			col += " (sealed)"
		}
		cols[tbl] = append(cols[tbl], col)
	}
	rows.Close()
	var undocumented []string
	for _, tbl := range tables {
		d, ok := tableDocs[tbl]
		if !ok {
			undocumented = append(undocumented, tbl)
			continue
		}
		fmt.Fprintf(&b, "- **`%s`**: %s  \n  Columns: %s\n", tbl, d, strings.Join(cols[tbl], ", "))
	}
	if len(undocumented) > 0 {
		t.Fatalf("tables without a description in tableDocs (add one): %v", undocumented)
	}
	for tbl := range tableDocs {
		if _, ok := cols[tbl]; !ok {
			t.Errorf("tableDocs describes %s, which no longer exists", tbl)
		}
	}

	path := filepath.Join("..", "docs", "ENDPOINTS.md")
	if os.Getenv("UPDATE_DOCS") != "" {
		e.must(os.WriteFile(path, []byte(b.String()), 0o644))
		return
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != b.String() {
		t.Fatalf("docs/ENDPOINTS.md is missing or stale: run UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc")
	}
}
