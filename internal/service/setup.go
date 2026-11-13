package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/installation"
)

// The setup journey (installation steps 1-10). It is derived entirely from
// what the installation already holds, so it resumes from any device or
// after a restart, and never claims a step it has no evidence for. Both
// delivery modes share every step except step 4.

type SetupStep struct {
	N       int        `json:"n"`
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Status  string     `json:"status"` // ready | needs_action | not_started | skipped_optional
	Detail  string     `json:"detail"`
	Command string     `json:"command,omitempty"` // a host command for this step
	Route   string     `json:"route,omitempty"`   // a dashboard page, relative to /p/{project}/
	At      *time.Time `json:"at,omitempty"`
}

type SetupJourney struct {
	Project string      `json:"project"` // the project the per-project steps describe ("" when none)
	Mode    string      `json:"mode"`    // chosen delivery mode
	Next    int         `json:"next"`    // first step that is not ready (0 when all are)
	Done    int         `json:"done"`
	Total   int         `json:"total"`
	Steps   []SetupStep `json:"steps"`
}

func (s *Service) SetupJourney(ctx context.Context) (*SetupJourney, error) {
	st, err := s.InstallationState(ctx)
	if err != nil {
		return nil, err
	}
	w, _ := st["wizard"].(WizardState)
	rd, _ := st["readiness"].(installation.Readiness)
	j := &SetupJourney{Mode: w.SelectedMode}
	add := func(id, title, status, detail string) *SetupStep {
		j.Steps = append(j.Steps, SetupStep{N: len(j.Steps) + 1, ID: id, Title: title, Status: status, Detail: detail})
		return &j.Steps[len(j.Steps)-1]
	}

	add("install", "Install Njiwa-Mail", "ready", "The API and dashboard are running.")

	var discAt *time.Time
	var recMode string
	_ = s.Pool.QueryRow(ctx, `SELECT discovered_at, COALESCE(discovery->'recommendation'->>'mode','') FROM installation_settings WHERE id=1`).Scan(&discAt, &recMode)
	d := add("detect", "Detect what is already on the server", "needs_action",
		"Run the read-only scan on the server. It finds mail servers (Mailcow, Postfix, Exim), busy ports, proxies and certificates, and changes nothing.")
	d.Command, d.Route = "njiwa-mail discover -record", "mail-server"
	switch {
	case discAt != nil:
		d.Status, d.At, d.Detail = "ready", discAt, "The server was scanned; the recommendation is shown below."
	case w.SelectedMode != "":
		d.Status, d.Detail = "skipped_optional", "Skipped: a delivery mode was chosen without a scan. Run it any time to check for conflicts."
	}

	c := add("choose_mode", "Choose how mail is sent", "needs_action", "Choose \"Send from my VPS\" or \"Use my existing mail server\".")
	c.Route = "mail-server"
	if recMode != "" && w.SelectedMode == "" {
		c.Detail = "Recommended for this server: " + modeLabel(recMode) + "."
	}
	if w.SelectedMode != "" {
		c.Status, c.At, c.Detail = "ready", w.ConfirmedAt, modeLabel(w.SelectedMode)+" was chosen."
		if recMode != "" && recMode != w.SelectedMode {
			c.Detail += " The scan recommended " + modeLabel(recMode) + "; check the reasons below."
		}
	}

	t := add("connect", "Connect the mail server", "not_started", "Choose a delivery mode first.")
	t.Route = "mail-server"
	switch w.SelectedMode {
	case installation.ModeExisting:
		t.Title = "Connect your existing mail server"
		t.Detail = "Enter the SMTP host, port, encryption and a login, then run the connection test and a test send."
	case installation.ModeManaged:
		t.Title = "Install the Njiwa-managed mail server"
		t.Detail = "On the server, as root: njiwa-mail preflight -record, then installer plan, review it, and installer apply -approve <digest> -record. Then run the connection test."
		t.Command = "njiwa-mail installer plan"
	}
	if w.SelectedMode != "" {
		if rd.Status == installation.Ready {
			t.Status, t.Detail = "ready", "Delivery is configured and its checks pass."
		} else {
			t.Status = "needs_action"
			for _, x := range rd.Steps {
				if x.Required && x.Status != installation.Ready {
					t.Detail += " Next: " + x.Title + "."
					break
				}
			}
		}
	}

	// Per-project steps: the project furthest along (the one an operator is
	// working on), else the first one.
	var pid string
	_ = s.Pool.QueryRow(ctx, `SELECT p.id::text, p.name FROM projects p WHERE p.enabled
ORDER BY EXISTS (SELECT 1 FROM domains d WHERE d.project_id=p.id AND d.verified_at IS NOT NULL) DESC, p.created_at LIMIT 1`).Scan(&pid, &j.Project)
	var verified, dnsOK, viaProvider, sender, key, sent bool
	if pid != "" {
		_ = s.Pool.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM domains WHERE project_id=$1 AND purpose IN ('sending','both') AND verified_at IS NOT NULL),
 EXISTS (SELECT 1 FROM domains WHERE project_id=$1 AND purpose IN ('sending','both') AND verified_at IS NOT NULL AND spf_ok AND dkim_ok AND dmarc_ok),
 EXISTS (SELECT 1 FROM dns_setups WHERE project_id=$1),
 EXISTS (SELECT 1 FROM sender_identities si JOIN environments e ON e.id=si.environment_id WHERE si.project_id=$1 AND e.mode='live'),
 EXISTS (SELECT 1 FROM api_keys k JOIN environments e ON e.id=k.environment_id WHERE k.project_id=$1 AND e.mode='live'
           AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())),
 EXISTS (SELECT 1 FROM messages m JOIN environments e ON e.id=m.environment_id WHERE e.project_id=$1 AND e.mode='live' AND m.submitted_at IS NOT NULL)`, pid).
			Scan(&verified, &dnsOK, &viaProvider, &sender, &key, &sent)
	}
	step := func(id, title, todo, done, route string, ok bool) {
		x := add(id, title, "needs_action", todo)
		x.Route = route
		if pid == "" {
			x.Status, x.Detail = "not_started", "Create a project first (Projects page)."
		} else if ok {
			x.Status, x.Detail = "ready", done
		}
	}
	step("domain", "Add and verify a sending domain", "Add your domain and publish its ownership TXT record.",
		"A sending domain is verified and claimed by this project.", "domains", verified)
	dnsDone := "SPF, DKIM and DMARC pass."
	if viaProvider {
		dnsDone += " Records were applied through your DNS provider."
	}
	step("dns", "Publish the DNS records", "Publish SPF, DKIM and DMARC by hand, or connect your DNS provider to apply them after you review them. Existing MX records are never changed without your approval.",
		dnsDone, "domains", dnsOK)
	// Managed mode: a domain verified after the install has no DKIM key on
	// the host until the installer runs again (task T7.2).
	if w.SelectedMode == installation.ModeManaged {
		if missing, err := s.DomainsMissingKey(ctx); err == nil && len(missing) > 0 {
			x := &j.Steps[len(j.Steps)-1]
			if x.Status == "ready" {
				x.Status = "needs_action"
			}
			x.Detail = "The mail host has no DKIM key yet for " + strings.Join(missing, ", ") + ". Run the installer again on the host (the Mail host page shows the exact commands), then publish the DKIM record it prints."
			x.Command, x.Route = "njiwa-mail installer plan", "mail-host"
		}
	}
	step("sender", "Create a sender address", "Add a Live sender on the verified domain (for example noreply@your-domain).",
		"A Live sender address exists.", "senders", sender)
	step("api_key", "Create an API key", "Create a Live API key for your application.", "A Live API key exists.", "keys", key)
	testTodo := "Send a test message from the dashboard and check it in the recipient's inbox (headers show SPF, DKIM and DMARC)."
	var perMin, perHour int
	if pid != "" && s.Pool.QueryRow(ctx, `SELECT recipient_per_minute, recipient_per_hour FROM projects WHERE id=$1`, pid).Scan(&perMin, &perHour) == nil {
		// Abuse protection that surprises a first test (task T3.6).
		testTodo += fmt.Sprintf(" This project accepts at most %d messages per minute and %d per hour to the same recipient; change it on the Projects page.", perMin, perHour)
	}
	step("test_send", "Send a test email", testTodo,
		"A Live message was handed to the mail server. Delivery is proven by the received message, not by this step.", "setup", sent)

	dash := add("dashboard", "Open the dashboard", "not_started", "Finish the steps above.")
	dash.Route = "dashboard"
	all := true
	for _, x := range j.Steps[:len(j.Steps)-1] {
		if x.Status != "ready" && x.Status != "skipped_optional" {
			all = false
		}
	}
	if all {
		dash.Status, dash.Detail = "ready", "Setup is complete. Messages, delivery evidence and analytics are on the dashboard."
	}
	for _, x := range j.Steps {
		if x.Status == "ready" || x.Status == "skipped_optional" {
			j.Done++
		} else if j.Next == 0 {
			j.Next = x.N
		}
	}
	j.Total = len(j.Steps)
	return j, nil
}

func modeLabel(m string) string {
	switch m {
	case installation.ModeManaged:
		return "Send from my VPS"
	case installation.ModeExisting:
		return "Use my existing mail server"
	}
	return m
}
