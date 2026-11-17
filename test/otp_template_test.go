package test

import (
	"context"
	"strings"
	"testing"

	"github.com/njiwa-tz/mail/internal/tmpl"
)

func otpSeed(t *testing.T) tmpl.Seed {
	t.Helper()
	for _, sd := range tmpl.Seeds() {
		if sd.Slug == "otp" {
			return sd
		}
	}
	t.Fatal("no otp seed")
	return tmpl.Seed{}
}

// The code leads the subject, so mail apps (Gmail's "Copy code") recognise it.
func TestOTPSubjectLeadsWithTheCode(t *testing.T) {
	sd := otpSeed(t)
	cases := []struct {
		locale string
		vars   map[string]string
		want   string
	}{
		{"en", map[string]string{"code": "821580", "expires_in_minutes": "5"}, "821580 is your verification code"},
		{"en", map[string]string{"code": "821580", "expires_in_minutes": "5", "app_name": "Acme"}, "821580 is your Acme verification code"},
		{"sw", map[string]string{"code": "821580", "expires_in_minutes": "5"}, "821580 ni namba yako ya uthibitisho"},
		{"sw", map[string]string{"code": "821580", "expires_in_minutes": "5", "app_name": "Acme"}, "821580 ni namba yako ya uthibitisho ya Acme"},
	}
	for _, c := range cases {
		subject, text, html, err := tmpl.Render(sd.Content[c.locale], sd.Schema, c.vars)
		if err != nil {
			t.Fatalf("%s: %v", c.locale, err)
		}
		if subject != c.want {
			t.Errorf("%s subject = %q, want %q", c.locale, subject, c.want)
		}
		if !strings.HasPrefix(text, map[string]string{"en": "Your verification code is 821580", "sw": "Namba yako ya uthibitisho ni 821580"}[c.locale]) {
			t.Errorf("%s text does not open with the code:\n%s", c.locale, text)
		}
		// One unbroken text node, so copying the code copies exactly the digits.
		if !strings.Contains(html, ">821580</div>") {
			t.Errorf("%s html has no single code block", c.locale)
		}
	}
}

func publishShared(t *testing.T, e *env, slug string, content map[string]tmpl.Content) {
	t.Helper()
	ctx := context.Background()
	var tid, vid string
	e.must(e.svc.Pool.QueryRow(ctx, `INSERT INTO templates(project_id, slug) VALUES (NULL,$1) RETURNING id::text`, slug).Scan(&tid))
	e.must(e.svc.Pool.QueryRow(ctx, `INSERT INTO template_versions(template_id, version) VALUES ($1,1) RETURNING id::text`, tid).Scan(&vid))
	for l, c := range content {
		_, err := e.svc.Pool.Exec(ctx, `INSERT INTO template_contents(version_id, locale, subject, html_body, text_body) VALUES ($1,$2,$3,$4,$5)`, vid, l, c.Subject, c.HTML, c.Text)
		e.must(err)
	}
	_, err := e.svc.Pool.Exec(ctx, `UPDATE template_versions SET published_at=now() WHERE id=$1`, vid)
	e.must(err)
}

func sharedVersions(t *testing.T, e *env, slug string) (latest int, subject string) {
	t.Helper()
	e.must(e.svc.Pool.QueryRow(context.Background(), `SELECT v.version, c.subject FROM templates t
JOIN template_versions v ON v.template_id=t.id JOIN template_contents c ON c.version_id=v.id AND c.locale='en'
WHERE t.project_id IS NULL AND t.slug=$1 AND v.published_at IS NOT NULL ORDER BY v.version DESC LIMIT 1`, slug).Scan(&latest, &subject))
	return
}

// An installation still on the original built-in OTP content gets the new design as
// version 2; an operator's own content is never replaced.
func TestBuiltInOTPUpgradesOnlyUntouchedVersions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sd := otpSeed(t)

	if v, subject := sharedVersions(t, e, "otp"); v != 1 || !strings.HasPrefix(subject, "{{.code}}") {
		t.Fatalf("fresh install: version %d subject %q", v, subject)
	}

	legacy := sd
	legacy.Slug = "otp-legacy"
	publishShared(t, e, "otp-legacy", sd.Replaces[0])
	e.must(e.svc.UpgradeSeed(ctx, legacy))
	if v, subject := sharedVersions(t, e, "otp-legacy"); v != 2 || subject != sd.Content["en"].Subject {
		t.Fatalf("legacy content was not upgraded: version %d subject %q", v, subject)
	}
	e.must(e.svc.UpgradeSeed(ctx, legacy)) // idempotent
	if v, _ := sharedVersions(t, e, "otp-legacy"); v != 2 {
		t.Fatalf("second upgrade added version %d", v)
	}

	custom := sd
	custom.Slug = "otp-custom"
	mine := map[string]tmpl.Content{}
	for l, c := range sd.Replaces[0] {
		c.Subject = "Our own subject"
		mine[l] = c
	}
	publishShared(t, e, "otp-custom", mine)
	e.must(e.svc.UpgradeSeed(ctx, custom))
	if v, subject := sharedVersions(t, e, "otp-custom"); v != 1 || subject != "Our own subject" {
		t.Fatalf("an operator's version was replaced: version %d subject %q", v, subject)
	}
}
