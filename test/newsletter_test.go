package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/service"
)

func TestNewsletterRenderingEscapesAndMakesText(t *testing.T) {
	h, text := service.RenderNewsletter("Peek <b>", []service.NewsletterBlock{
		{Type: "heading", Text: "News <script>"},
		{Type: "text", Text: "Line one\nLine two"},
		{Type: "button", Text: "Read", URL: "https://example.com/a?x=1&y=2"},
		{Type: "image", URL: "https://example.com/i.png", Alt: "A \"bird\""},
		{Type: "divider"},
		{Type: "footer", Text: "Acme"},
	})
	if strings.Contains(h, "<script>") || strings.Contains(h, "<b>") || !strings.Contains(h, "Line one<br>Line two") || !strings.Contains(h, "x=1&amp;y=2") {
		t.Fatal(h)
	}
	for _, want := range []string{"NEWS <SCRIPT>", "Read: https://example.com/a?x=1&y=2", "[A \"bird\"]", "Acme"} {
		if !strings.Contains(text, want) {
			t.Errorf("text version lacks %q: %s", want, text)
		}
	}
}

// Drafts, a test to oneself, then sending: only contacts who may receive
// marketing mail get it, every copy carries the unsubscribe link, and it can
// be sent once.
func TestNewsletterDraftTestSendAndResults(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.svc.Opts.PublicURL = "https://mail.example.com"
	e.provision("sacas")
	_, err := e.pool.Exec(ctx, `UPDATE domains SET bulk=true WHERE name='notify.example.com'`)
	e.must(err)
	admin := e.user("admin@example.org", "admin")
	list := str(e.admOK(admin, "POST", wsBase+"/lists", map[string]any{"name": "Readers"}).JSON, "id")
	ids := map[string]string{}
	for _, email := range []string{"ok@example.org", "gone@example.org", "blocked@example.org"} {
		r := e.admOK(admin, "POST", wsBase+"/contacts", map[string]any{"email": email, "lists": []string{list}})
		ids[email] = str(r.JSON, "id")
	}
	_, err = e.pool.Exec(ctx, `UPDATE contacts SET unsubscribed_at=now() WHERE id=$1`, ids["gone@example.org"])
	e.must(err)
	e.must(e.svc.Suppress(ctx, "sacas", "sandbox", "blocked@example.org", "manual"))

	body := map[string]any{"name": "November", "subject": "", "preview_text": "Inside", "sender_email": liveFrom, "list_id": list,
		"blocks": []map[string]any{{"type": "heading", "text": "Hello"}, {"type": "text", "text": "Our news."}}}
	id := str(e.admOK(admin, "POST", wsBase+"/newsletters", body).JSON, "id")
	if r := e.adm(admin, "POST", wsBase+"/newsletters/"+id+"/send", map[string]any{}); r.Code != 400 {
		t.Fatalf("without a subject: %d %s", r.Code, r.Raw)
	}
	body["subject"] = "November news"
	e.admOK(admin, "PUT", wsBase+"/newsletters/"+id, body)
	if r := e.admOK(admin, "GET", wsBase+"/newsletters/"+id, nil); r.JSON["state"] != "draft" || !strings.Contains(r.Raw, "Our news.") {
		t.Fatalf("draft: %s", r.Raw)
	}
	test := e.admOK(admin, "POST", wsBase+"/newsletters/"+id+"/test", nil)
	if n := e.count(`SELECT count(*) FROM messages WHERE id=$1 AND recipient_hash=$2`, str(test.JSON, "id"), e.box.MAC("recipient", []byte("admin@example.org"))); n != 1 {
		t.Fatal("the test did not go to the signed-in person")
	}

	later := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	sent := e.admOK(admin, "POST", wsBase+"/newsletters/"+id+"/send", map[string]any{"send_at": later})
	campaign := str(sent.JSON, "campaign_id")
	if r := e.adm(admin, "POST", wsBase+"/newsletters/"+id+"/send", map[string]any{}); r.Code != 409 {
		t.Fatalf("second send: %d", r.Code)
	}
	if r := e.adm(admin, "PUT", wsBase+"/newsletters/"+id, body); r.Code != 409 {
		t.Fatalf("edit after scheduling: %d", r.Code)
	}
	if r := e.admOK(admin, "GET", wsBase+"/newsletters/"+id, nil); r.JSON["state"] != "scheduled" {
		t.Fatalf("%s", r.Raw)
	}
	e.admOK(admin, "POST", wsBase+"/campaigns/"+campaign+"/reschedule", map[string]any{"send_at": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)})
	// Its time comes.
	_, err = e.pool.Exec(ctx, `UPDATE campaigns SET send_at=now() - interval '1 minute' WHERE id=$1`, campaign)
	e.must(err)
	for range 3 {
		_, err = e.svc.PumpCampaigns(ctx)
		e.must(err)
	}
	if n := e.count(`SELECT count(*) FROM messages WHERE campaign_id=$1`, campaign); n != 1 {
		t.Fatalf("messages: %d (only the one contact who may receive it)", n)
	}
	pl := e.payloadOf("sacas", "ok@example.org")
	if !strings.Contains(pl.HTML, "/v1/unsubscribe/") || !strings.Contains(pl.Text, "/v1/unsubscribe/") || pl.System["List-Unsubscribe"] == "" {
		t.Fatalf("no unsubscribe link: %+v", pl)
	}
	c := e.admOK(admin, "GET", wsBase+"/contacts/"+ids["ok@example.org"], nil)
	if !strings.Contains(c.Raw, campaign) || !strings.Contains(c.Raw, id) {
		t.Fatalf("the contact does not list the newsletter: %s", c.Raw)
	}
}
