package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/tmpl"
	"github.com/jackc/pgx/v5"
)

// Templates live in the database. A version is a draft until published, and
// immutable afterwards (also enforced by triggers). Messages pin the version
// id at accept time, so later edits never change mail already queued.

type tplRef struct {
	VersionID  string
	Slug       string
	Version    int
	Schema     tmpl.Schema
	DefaultTTL time.Duration
	MaxTTL     time.Duration
}

// SeedTemplates installs the built-in shared templates (otp, password-reset,
// account-notice) as published version 1 in English and Kiswahili. Idempotent.
func (s *Service) SeedTemplates(ctx context.Context) error {
	for _, sd := range tmpl.Seeds() {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		var tid string
		err = tx.QueryRow(ctx, `INSERT INTO templates(project_id, slug, description, default_ttl_seconds, max_ttl_seconds)
VALUES (NULL,$1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING id::text`,
			sd.Slug, sd.Description, int(sd.DefaultTTL.Seconds()), int(sd.MaxTTL.Seconds())).Scan(&tid)
		if errors.Is(err, pgx.ErrNoRows) { // already seeded
			tx.Rollback(ctx)
			continue
		}
		if err == nil {
			err = insertVersion(ctx, tx, tid, 1, sd.Schema, sd.Content, true)
		}
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func insertVersion(ctx context.Context, tx pgx.Tx, templateID string, version int, schema tmpl.Schema, content map[string]tmpl.Content, publish bool) error {
	sj, _ := json.Marshal(schema)
	var vid string
	if err := tx.QueryRow(ctx, `INSERT INTO template_versions(template_id, version, variable_schema) VALUES ($1,$2,$3) RETURNING id::text`,
		templateID, version, sj).Scan(&vid); err != nil {
		return err
	}
	for l, c := range content {
		if _, err := tx.Exec(ctx, `INSERT INTO template_contents(version_id, locale, subject, html_body, text_body) VALUES ($1,$2,$3,$4,$5)`,
			vid, l, c.Subject, c.HTML, c.Text); err != nil {
			return err
		}
	}
	if publish {
		_, err := tx.Exec(ctx, `UPDATE template_versions SET published_at = now() WHERE id=$1`, vid)
		return err
	}
	return nil
}

// resolveTemplate finds the published version to pin: the requested one, or
// the newest when version is nil. A project's own template shadows the shared
// template with the same slug.
func (s *Service) resolveTemplate(ctx context.Context, projectID, slug string, version *int) (*tplRef, error) {
	var r tplRef
	var schema []byte
	var def, max int
	err := s.Pool.QueryRow(ctx, `
SELECT v.id::text, t.slug, v.version, v.variable_schema, t.default_ttl_seconds, t.max_ttl_seconds
FROM templates t JOIN template_versions v ON v.template_id = t.id
WHERE t.slug = $2 AND (t.project_id = $1 OR t.project_id IS NULL) AND NOT t.archived
  AND v.published_at IS NOT NULL AND ($3::int IS NULL OR v.version = $3)
ORDER BY (t.project_id IS NULL), v.version DESC LIMIT 1`, projectID, slug, version).
		Scan(&r.VersionID, &r.Slug, &r.Version, &schema, &def, &max)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalid("unknown_template")
	}
	if err != nil {
		return nil, unavailable("template_lookup", err)
	}
	if err := json.Unmarshal(schema, &r.Schema); err != nil {
		return nil, unavailable("template_schema", err)
	}
	r.DefaultTTL, r.MaxTTL = time.Duration(def)*time.Second, time.Duration(max)*time.Second
	return &r, nil
}

// loadContent returns the schema and the content for locale, falling back to
// English when that locale was not written for the version.
func (s *Service) loadContent(ctx context.Context, versionID, locale string) (tmpl.Schema, tmpl.Content, error) {
	var schema tmpl.Schema
	var c tmpl.Content
	var sj []byte
	err := s.Pool.QueryRow(ctx, `
SELECT v.variable_schema, c.subject, c.html_body, c.text_body
FROM template_versions v JOIN template_contents c ON c.version_id = v.id
WHERE v.id = $1 AND c.locale IN ($2, 'en') ORDER BY (c.locale = 'en') LIMIT 1`, versionID, locale).
		Scan(&sj, &c.Subject, &c.HTML, &c.Text)
	if err != nil {
		return nil, c, err
	}
	return schema, c, json.Unmarshal(sj, &schema)
}

// RenderPayload renders a queued message from its pinned template version.
func (s *Service) RenderPayload(ctx context.Context, pl *Payload) (subject, text, html string, err error) {
	if pl.Raw {
		return pl.Subject, pl.Text, pl.HTML, nil
	}
	vid := pl.VersionID
	if vid == "" { // queued before template versions were stored on the message: shared template by slug and number
		if err = s.Pool.QueryRow(ctx, `SELECT v.id::text FROM templates t JOIN template_versions v ON v.template_id=t.id
WHERE t.project_id IS NULL AND t.slug=$1 AND v.version=$2 AND v.published_at IS NOT NULL`, pl.Template, pl.Version).Scan(&vid); err != nil {
			return
		}
	}
	locale := pl.Locale
	if locale == "" {
		locale = "en"
	}
	schema, c, err := s.loadContent(ctx, vid, locale)
	if err != nil {
		return
	}
	return tmpl.Render(c, schema, pl.Vars)
}

// ---------------------------------------------------------------- admin

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type TemplateInput struct {
	Project           string `json:"project"` // empty = shared across projects
	Slug              string `json:"slug"`
	Description       string `json:"description"`
	DefaultTTLSeconds int    `json:"default_ttl_seconds"`
	MaxTTLSeconds     int    `json:"max_ttl_seconds"`
}

type VersionInput struct {
	Schema  tmpl.Schema             `json:"variable_schema"`
	Content map[string]tmpl.Content `json:"content"`
}

func (in *VersionInput) check() error {
	if err := in.Schema.Check(); err != nil {
		return badRequest(err.Error())
	}
	if _, ok := in.Content["en"]; !ok {
		return badRequest("english (en) content is required; it is the fallback locale")
	}
	for l, c := range in.Content {
		if !tmpl.ValidLocale(l) {
			return badRequest("locale must be en or sw")
		}
		if err := tmpl.Compile(c, in.Schema); err != nil {
			return badRequest(l + ": " + err.Error())
		}
	}
	return nil
}

func ttlBounds(def, max int) (int, int, error) {
	if def == 0 {
		def = 3600
	}
	if max == 0 {
		max = 86400
	}
	if def < 30 || max < def || max > 7*86400 {
		return 0, 0, badRequest("ttl must satisfy 30 <= default <= max <= 604800 seconds")
	}
	return def, max, nil
}

func (s *Service) CreateTemplate(ctx context.Context, in TemplateInput) (string, error) {
	if !slugRe.MatchString(in.Slug) {
		return "", badRequest("slug must be lowercase letters, digits and dashes")
	}
	def, max, err := ttlBounds(in.DefaultTTLSeconds, in.MaxTTLSeconds)
	if err != nil {
		return "", err
	}
	var pid *string
	if in.Project != "" {
		var id string
		if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name=$1`, in.Project).Scan(&id); err != nil {
			return "", notFound(err)
		}
		pid = &id
	}
	var id string
	err = s.Pool.QueryRow(ctx, `INSERT INTO templates(project_id, slug, description, default_ttl_seconds, max_ttl_seconds)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING id::text`, pid, in.Slug, strings.TrimSpace(in.Description), def, max).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", newErr(409, "template_exists")
	}
	if err != nil {
		return "", unavailable("template_create", err)
	}
	return id, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, id string, description *string, archived *bool, defTTL, maxTTL *int) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var curDef, curMax int
	if err := s.Pool.QueryRow(ctx, `SELECT default_ttl_seconds, max_ttl_seconds FROM templates WHERE id=$1`, id).Scan(&curDef, &curMax); err != nil {
		return notFound(err)
	}
	if defTTL != nil {
		curDef = *defTTL
	}
	if maxTTL != nil {
		curMax = *maxTTL
	}
	def, max, err := ttlBounds(curDef, curMax)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE templates SET description=COALESCE($2, description), archived=COALESCE($3, archived),
default_ttl_seconds=$4, max_ttl_seconds=$5 WHERE id=$1`, id, description, archived, def, max)
	if err != nil {
		return unavailable("template_update", err)
	}
	return nil
}

func (s *Service) ListTemplates(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `
SELECT t.id::text AS id, t.slug, t.description, p.name AS project, t.archived, t.default_ttl_seconds, t.max_ttl_seconds, t.created_at,
       (SELECT max(version) FROM template_versions v WHERE v.template_id=t.id AND v.published_at IS NOT NULL) AS published_version,
       (SELECT max(version) FROM template_versions v WHERE v.template_id=t.id AND v.published_at IS NULL) AS draft_version
FROM templates t LEFT JOIN projects p ON p.id=t.project_id ORDER BY t.slug, p.name NULLS FIRST`)
}

// GetTemplate returns the template with its version history and content.
func (s *Service) GetTemplate(ctx context.Context, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	t, err := s.one(ctx, `SELECT t.id::text AS id, t.slug, t.description, p.name AS project, t.archived, t.default_ttl_seconds, t.max_ttl_seconds, t.created_at
FROM templates t LEFT JOIN projects p ON p.id=t.project_id WHERE t.id=$1`, id)
	if err != nil {
		return nil, err
	}
	vs, err := s.maps(ctx, `SELECT v.id::text AS id, v.version, v.variable_schema, v.published_at, v.created_at,
  COALESCE((SELECT jsonb_object_agg(c.locale, jsonb_build_object('subject', c.subject, 'html', c.html_body, 'text', c.text_body))
            FROM template_contents c WHERE c.version_id=v.id), '{}'::jsonb) AS content
FROM template_versions v WHERE v.template_id=$1 ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, err
	}
	t["versions"] = vs
	return t, nil
}

// SaveDraft creates the draft version, or replaces it when one exists. There
// is at most one draft per template: the version after the newest published.
func (s *Service) SaveDraft(ctx context.Context, templateID string, in VersionInput) (int, error) {
	if !uuidRe.MatchString(templateID) {
		return 0, newErr(404, "not_found")
	}
	if err := in.check(); err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM templates WHERE id=$1 FOR UPDATE`, templateID).Scan(&one); err != nil {
		return 0, notFound(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM template_versions WHERE template_id=$1 AND published_at IS NULL`, templateID); err != nil {
		return 0, unavailable("draft_replace", err)
	}
	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM template_versions WHERE template_id=$1`, templateID).Scan(&next); err != nil {
		return 0, unavailable("draft_version", err)
	}
	if err := insertVersion(ctx, tx, templateID, next, in.Schema, in.Content, false); err != nil {
		return 0, unavailable("draft_insert", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, unavailable("commit", err)
	}
	return next, nil
}

// PublishVersion makes a draft immutable and available for sending.
func (s *Service) PublishVersion(ctx context.Context, templateID string, version int) error {
	if !uuidRe.MatchString(templateID) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE template_versions SET published_at=now() WHERE template_id=$1 AND version=$2 AND published_at IS NULL`, templateID, version)
	if err != nil {
		return unavailable("publish", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "not_a_draft")
	}
	return nil
}

type Rendered struct {
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

func (s *Service) versionID(ctx context.Context, templateID string, version int) (string, error) {
	if !uuidRe.MatchString(templateID) {
		return "", newErr(404, "not_found")
	}
	var vid string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM template_versions WHERE template_id=$1 AND version=$2`, templateID, version).Scan(&vid)
	return vid, notFound(err)
}

// PreviewVersion renders a version (draft or published) with the given or
// sample variables. Nothing is queued or sent.
func (s *Service) PreviewVersion(ctx context.Context, templateID string, version int, locale string, vars map[string]any) (*Rendered, error) {
	if locale == "" {
		locale = "en"
	}
	if !tmpl.ValidLocale(locale) {
		return nil, badRequest("locale must be en or sw")
	}
	vid, err := s.versionID(ctx, templateID, version)
	if err != nil {
		return nil, err
	}
	schema, c, err := s.loadContent(ctx, vid, locale)
	if err != nil {
		return nil, notFound(err)
	}
	values := schema.Sample(nil)
	for k, v := range vars { // previews are lenient: any scalar, no origin check
		if str, ok := v.(string); ok {
			values[k] = str
		} else if v != nil {
			b, _ := json.Marshal(v)
			values[k] = string(b)
		}
	}
	sub, text, html, err := tmpl.Render(c, schema, values)
	if err != nil {
		return nil, badRequest(err.Error())
	}
	return &Rendered{Locale: locale, Subject: sub, HTML: html, Text: text}, nil
}

type TestSend struct {
	Project   string         `json:"project"`
	Mode      string         `json:"mode"`
	From      string         `json:"from"`
	To        string         `json:"to"`
	Locale    string         `json:"locale"`
	Variables map[string]any `json:"variables"`
}

// TestSendVersion queues one message from the admin UI through the normal
// accept pipeline (sender approval, suppression, quotas). In sandbox it works
// for any version including drafts and is only captured. In live it requires a
// published version and a recipient on the environment's approved list,
// whether or not the environment otherwise restricts recipients.
func (s *Service) TestSendVersion(ctx context.Context, templateID string, version int, in TestSend) (*Accepted, error) {
	if validMode(in.Mode) != nil {
		return nil, badRequest("mode must be sandbox or live")
	}
	p, err := s.principalFor(ctx, in.Project, in.Mode)
	if err != nil {
		return nil, err
	}
	var ref tplRef
	var schema []byte
	var def, max int
	var published *time.Time
	if !uuidRe.MatchString(templateID) {
		return nil, newErr(404, "not_found")
	}
	err = s.Pool.QueryRow(ctx, `SELECT v.id::text, t.slug, v.version, v.variable_schema, t.default_ttl_seconds, t.max_ttl_seconds, v.published_at
FROM templates t JOIN template_versions v ON v.template_id=t.id
WHERE t.id=$1 AND v.version=$2 AND (t.project_id IS NULL OR t.project_id=$3)`, templateID, version, p.ProjectID).
		Scan(&ref.VersionID, &ref.Slug, &ref.Version, &schema, &def, &max, &published)
	if err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal(schema, &ref.Schema); err != nil {
		return nil, unavailable("template_schema", err)
	}
	ref.DefaultTTL, ref.MaxTTL = time.Duration(def)*time.Second, time.Duration(max)*time.Second
	if in.Mode == "live" {
		if published == nil {
			return nil, newErr(409, "draft_cannot_be_sent_live")
		}
		p.RestrictRecipients = true // live test sends always need an approved test address
	}
	v, err := s.validateRequest(ctx, p, SendRequest{From: in.From, To: in.To, Template: ref.Slug, Locale: in.Locale, Variables: in.Variables}, &ref)
	if err != nil {
		return nil, err
	}
	return s.acceptValidated(ctx, p, v, "test-"+secure.NewUUID())
}
