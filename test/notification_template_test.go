package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/tmpl"
)

func seedBySlug(t *testing.T, slug string) tmpl.Seed {
	t.Helper()
	for _, sd := range tmpl.Seeds() {
		if sd.Slug == slug {
			return sd
		}
	}
	t.Fatalf("no %s seed", slug)
	return tmpl.Seed{}
}

func decodeVars(t *testing.T, s string) map[string]any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber() // as the API decodes requests
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

// details accepts a list of rows (kept in order) or an object (sorted by label), with
// string, number and boolean values; everything is escaped in the HTML.
func TestNotificationDetailsRender(t *testing.T) {
	sd := seedBySlug(t, "notification")
	origins := []string{"https://app.example.com"}

	vars, verr := sd.Schema.Validate(decodeVars(t, `{"title":"Order shipped","message":"Your order is on its way.\nThank you.",
		"details":[{"label":"Order","value":"#1042"},{"label":"Total","value":25000},{"label":"Gift","value":true},{"label":"Note","value":"<b>fragile</b>"}],
		"action_url":"https://app.example.com/orders/1042","action_label":"Track order"}`), origins)
	if verr != nil {
		t.Fatal(verr)
	}
	for _, l := range tmpl.Locales {
		subject, text, html, err := tmpl.Render(sd.Content[l], sd.Schema, vars)
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		if subject != "Order shipped" {
			t.Errorf("%s subject %q", l, subject)
		}
		if !strings.Contains(text, "Order: #1042\nTotal: 25000\nGift: true\nNote: <b>fragile</b>\n") {
			t.Errorf("%s text rows wrong:\n%s", l, text)
		}
		if !strings.Contains(text, "Track order: https://app.example.com/orders/1042") {
			t.Errorf("%s text has no action link:\n%s", l, text)
		}
		if strings.Contains(html, "<b>fragile") || !strings.Contains(html, "&lt;b&gt;fragile") {
			t.Errorf("%s html did not escape a value", l)
		}
		if strings.Index(html, ">Order<") > strings.Index(html, ">Total<") || !strings.Contains(html, ">25000<") {
			t.Errorf("%s html rows out of order or missing", l)
		}
		if !strings.Contains(html, `href="https://app.example.com/orders/1042"`) {
			t.Errorf("%s html has no button", l)
		}
	}

	// An object works too; rows come out sorted by label.
	vars, verr = sd.Schema.Validate(decodeVars(t, `{"title":"Receipt","details":{"b_amount":"TZS 5,000","a_ref":"R-9"}}`), origins)
	if verr != nil {
		t.Fatal(verr)
	}
	_, text, _, err := tmpl.Render(sd.Content["en"], sd.Schema, vars)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "a_ref: R-9\nb_amount: TZS 5,000\n") {
		t.Errorf("object rows wrong:\n%s", text)
	}

	// Only a title: no empty details table, button or footer.
	vars, verr = sd.Schema.Validate(decodeVars(t, `{"title":"Hi"}`), origins)
	if verr != nil {
		t.Fatal(verr)
	}
	_, _, html, err := tmpl.Render(sd.Content["en"], sd.Schema, vars)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<a href") || strings.Contains(html, "aria-label=\"Details\"") {
		t.Errorf("optional parts shown when empty:\n%s", html)
	}
}

func TestNotificationDetailsRejected(t *testing.T) {
	sd := seedBySlug(t, "notification")
	origins := []string{"https://app.example.com"}
	many := make([]string, tmpl.DefaultFieldRows+1)
	for i := range many {
		many[i] = `{"label":"x","value":"y"}`
	}
	cases := map[string]struct{ body, code string }{
		"not a list":       {`{"title":"T","details":"Order: 1"}`, "invalid_variable_type"},
		"nested object":    {`{"title":"T","details":[{"label":"a","value":{"x":1}}]}`, "invalid_variable_type"},
		"extra key":        {`{"title":"T","details":[{"label":"a","value":"b","style":"x"}]}`, "invalid_variable_type"},
		"no label":         {`{"title":"T","details":[{"label":" ","value":"b"}]}`, "missing_variable"},
		"too many rows":    {`{"title":"T","details":[` + strings.Join(many, ",") + `]}`, "variable_too_long"},
		"long value":       {`{"title":"T","details":{"a":"` + strings.Repeat("v", tmpl.DefaultFieldValue+1) + `"}}`, "variable_too_long"},
		"control in label": {`{"title":"T","details":{"a\u0007":"b"}}`, "invalid_characters"},
		"foreign origin":   {`{"title":"T","action_url":"https://evil.example.net/x"}`, "url_origin_not_approved"},
		"no title":         {`{"details":{"a":"b"}}`, "missing_variable"},
	}
	for name, c := range cases {
		_, verr := sd.Schema.Validate(decodeVars(t, c.body), origins)
		if verr == nil || verr.Code != c.code {
			t.Errorf("%s: got %v, want %s", name, verr, c.code)
		}
	}
}

// The new template is seeded and sends end to end through the API.
func TestNotificationSendsThroughTheAPI(t *testing.T) {
	e := setup(t)
	k := e.provision("sacas")
	r := e.post(k.sbx, idem("notif"), map[string]any{"from": liveFrom, "to": testRcpt, "template": "notification",
		"variables": map[string]any{"title": "Payment received", "details": []map[string]any{
			{"label": "Reference", "value": "INV-1042"}, {"label": "Amount", "value": "TZS 25,000"}}}})
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	e.runWorker()
	raw, err := e.svc.ShowCapture(context.Background(), str(r.JSON, "id"))
	e.must(err)
	s := strings.ReplaceAll(string(raw), "=\r\n", "")
	if !strings.Contains(s, "Subject: Payment received") || !strings.Contains(s, "Reference: INV-1042") || !strings.Contains(s, "TZS 25,000") {
		t.Fatalf("notification not rendered:\n%s", s)
	}
}
