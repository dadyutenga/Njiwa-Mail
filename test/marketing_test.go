package test

import (
	"context"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/service"
)

var (
	clickRe = regexp.MustCompile(`/v1/t/c/[A-Za-z0-9._-]+`)
	openRe  = regexp.MustCompile(`/v1/t/o/[A-Za-z0-9._-]+`)
	unsubRe = regexp.MustCompile(`/v1/unsubscribe/[A-Za-z0-9._-]+`)
)

// A campaign to a list: only active members get it, merge fields come from
// the contact, opt-in tracking rewrites links to stored URLs only, and the
// unsubscribe link leaves that list (or, by choice, all marketing mail),
// with an email.unsubscribed event.
func TestCampaignToListTrackingAndScopedUnsubscribe(t *testing.T) {
	e := setup(t)
	e.srv.Resolver = newDNS()
	e.svc.Opts.PublicURL = "https://mail.example.com"
	e.provision("sacas")
	ctx := context.Background()
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.verifiedDomain(admin, "sacas", "sandbox", "news.example.com", true)
	e.admOK(admin, "POST", wsBase+"/senders", map[string]any{"email": "news@news.example.com"})
	rc := newReceiver(t)
	e.endpoint(rc, "sandbox", []string{"email.unsubscribed"})
	sc, err := e.svc.ResolveScope(ctx, "sacas", "sandbox")
	e.must(err)
	listID, err := e.svc.CreateList(ctx, sc, "News", "")
	e.must(err)
	for _, c := range []service.ContactInput{
		{Email: "asha@example.org", Name: "Asha", Attributes: map[string]any{"plan": "pro"}, Lists: []string{listID}},
		{Email: "baraka@example.org", Name: "Baraka", Lists: []string{listID}},
		{Email: "outside@example.org", Name: "Not on the list"},
	} {
		_, _, err := e.svc.UpsertContact(ctx, sc, c)
		e.must(err)
	}
	// A template cannot be tracked: it is sent as it is.
	if r := e.adm(admin, "POST", wsBase+"/campaigns", map[string]any{"name": "T", "sender_email": "news@news.example.com",
		"template_id": "00000000-0000-4000-8000-000000000000", "list_id": listID, "track_opens": true}); r.Code != 400 {
		t.Fatalf("tracked template: %d %s", r.Code, r.Raw)
	}
	body := map[string]any{"name": "November", "sender_email": "news@news.example.com", "subject": "Hi {{name}}",
		"html":    `<html><body><p>Hello {{name}}, see <a href="https://example.com/pricing?x=1&amp;y=2">pricing</a>.</p></body></html>`,
		"list_id": listID, "track_opens": true, "track_clicks": true}
	c := e.admOK(admin, "POST", wsBase+"/campaigns", body)
	cid := str(c.JSON, "id")
	if c.JSON["report"] != nil && !strings.Contains(c.Raw, `"valid":2`) {
		t.Fatalf("audience: %s", c.Raw)
	}
	e.admOK(admin, "POST", wsBase+"/campaigns/"+cid+"/start", nil)
	if n, err := e.svc.PumpCampaigns(ctx); err != nil || n != 2 {
		t.Fatalf("pumped %d %v", n, err)
	}
	e.runWorker()
	var mid string
	e.must(e.pool.QueryRow(ctx, `SELECT m.id::text FROM messages m JOIN campaign_recipients r ON r.message_id=m.id
JOIN contacts k ON k.id=r.contact_id WHERE m.campaign_id=$1 AND k.email_masked='a***@example.org'`, cid).Scan(&mid))
	raw, err := e.svc.AdminCapture(ctx, mid)
	e.must(err)
	msg := strings.ReplaceAll(strings.ReplaceAll(string(raw), "=\r\n", ""), "=3D", "=")
	if !strings.Contains(msg, "Hello Asha") || strings.Contains(msg, "example.com/pricing") {
		t.Fatalf("message:\n%s", msg)
	}
	click, open, unsub := clickRe.FindString(msg), openRe.FindString(msg), unsubRe.FindString(msg)
	if click == "" || open == "" || unsub == "" {
		t.Fatalf("links missing:\n%s", msg)
	}
	// Open pixel: always a GIF; counted once.
	for i := 0; i < 2; i++ {
		if r := e.raw("GET", open, nil, nil); r.Code != 200 || r.Hdr.Get("Content-Type") != "image/gif" {
			t.Fatalf("pixel: %d", r.Code)
		}
	}
	// Click: redirects to the stored link only; a forged token is a 404.
	r := e.raw("GET", click, nil, nil)
	if r.Code != 302 || r.Hdr.Get("Location") != "https://example.com/pricing?x=1&y=2" {
		t.Fatalf("click: %d %v", r.Code, r.Hdr)
	}
	if r := e.raw("GET", click[:len(click)-3]+"AAA", nil, nil); r.Code != 404 {
		t.Fatalf("forged click: %d", r.Code)
	}
	det := e.admOK(admin, "GET", wsBase+"/campaigns/"+cid, nil)
	tr := det.JSON["tracking"].(map[string]any)
	if tr["unique_opens"] != float64(1) || tr["unique_clicks"] != float64(1) || str(det.JSON, "list_name") != "News" {
		t.Fatalf("campaign: %s", det.Raw)
	}
	// The page names the list and offers "all marketing mail"; nothing changes on GET.
	page := e.raw("GET", unsub, nil, nil)
	if !strings.Contains(page.Raw, "News") || !strings.Contains(page.Raw, `value="all"`) {
		t.Fatalf("page: %s", page.Raw)
	}
	// One-click (RFC 8058): leaves the list only.
	if r := e.roundTrip(httptest.NewRequest("POST", unsub, strings.NewReader("List-Unsubscribe=One-Click"))); r.Code != 200 {
		t.Fatalf("unsubscribe: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM contact_list_members WHERE unsubscribed_at IS NOT NULL`); n != 1 {
		t.Fatalf("list unsubscribes: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM bulk_unsubscribes`); n != 0 {
		t.Fatalf("a list unsubscribe became global: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries WHERE event_type='email.unsubscribed' AND payload::text LIKE '%"scope": "list"%'`); n != 1 {
		t.Fatalf("webhook: %d", n)
	}
	// The next campaign to the list skips her; transactional mail is not affected.
	c2 := e.admOK(admin, "POST", wsBase+"/campaigns", map[string]any{"name": "December", "sender_email": "news@news.example.com",
		"subject": "Again", "text": "Again", "list_id": listID})
	if !strings.Contains(c2.Raw, `"valid":1`) {
		t.Fatalf("second audience: %s", c2.Raw)
	}
	// Choosing "all marketing mail" is global.
	form := httptest.NewRequest("POST", unsub, strings.NewReader("scope=all"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if r := e.roundTrip(form); r.Code != 200 || !strings.Contains(r.Raw, "all marketing mail") {
		t.Fatalf("unsubscribe all: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM bulk_unsubscribes`); n != 1 {
		t.Fatalf("global unsubscribe: %d", n)
	}
	ct, _, err := e.svc.ListContacts(ctx, sc, service.ContactFilter{Status: "unsubscribed"}, service.Page{})
	e.must(err)
	if len(ct) != 1 {
		t.Fatalf("unsubscribed contacts: %v", ct)
	}
}
