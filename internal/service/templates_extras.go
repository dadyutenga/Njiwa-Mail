package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/tmpl"
)

// Template extras: shared stylesheets, export and import of a template as a JSON bundle, and
// import of a plain HTML file as a new draft.

const (
	maxTemplateStyles = 30
	BundleFormat      = "njiwa-template/1"
	maxImportHTML     = tmpl.MaxBodyBytes
)

var styleNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ---------------------------------------------------------------- stylesheets

func (s *Service) ListTemplateStyles(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, name, css, updated_at FROM template_styles WHERE project_id=$1 ORDER BY name`, sc.ProjectID)
}

type StyleInput struct {
	Name string `json:"name"`
	CSS  string `json:"css"`
}

func (in *StyleInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if !styleNameRe.MatchString(in.Name) {
		return badRequest("a stylesheet name is lowercase letters, digits and dashes")
	}
	if err := tmpl.CheckCSS(in.CSS); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

func (s *Service) CreateTemplateStyle(ctx context.Context, sc *Scope, in StyleInput) (string, error) {
	if err := in.check(); err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM template_styles WHERE project_id=$1`, sc.ProjectID).Scan(&n); err != nil {
		return "", unavailable("template_styles", err)
	}
	if n >= maxTemplateStyles {
		return "", badRequest("at most 30 stylesheets per project")
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO template_styles(project_id, name, css) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING RETURNING id::text`,
		sc.ProjectID, in.Name, in.CSS).Scan(&id)
	if err == db.ErrNoRows {
		return "", newErr(409, "style_exists")
	}
	if err != nil {
		return "", unavailable("template_styles", err)
	}
	return id, nil
}

// UpdateTemplateStyle changes the CSS. Versions already saved keep the CSS they were saved with;
// the next draft that names the stylesheet picks up the change.
func (s *Service) UpdateTemplateStyle(ctx context.Context, sc *Scope, id string, in StyleInput) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	if err := in.check(); err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE template_styles SET css=$3, updated_at=now() WHERE id=$1 AND project_id=$2 AND name=$4`, id, sc.ProjectID, in.CSS, in.Name)
	if err != nil {
		return unavailable("template_styles", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) DeleteTemplateStyle(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM template_styles WHERE id=$1 AND project_id=$2`, id, sc.ProjectID)
	if err != nil {
		return unavailable("template_styles", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// ---------------------------------------------------------------- export and import

type Bundle struct {
	Format      string                  `json:"format"`
	Slug        string                  `json:"slug"`
	Description string                  `json:"description"`
	Schema      tmpl.Schema             `json:"variable_schema"`
	Content     map[string]tmpl.Content `json:"content"`
	Style       *StyleInput             `json:"style,omitempty"`
}

// ExportTemplate returns the newest published version (or the draft when nothing is published)
// as a bundle another installation, or another project, can import.
func (s *Service) ExportTemplate(ctx context.Context, sc *Scope, id string) (*Bundle, error) {
	if _, err := s.TemplateInScope(ctx, sc, id); err != nil {
		return nil, err
	}
	t, err := s.GetTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	vers, _ := t["versions"].([]map[string]any)
	if len(vers) == 0 {
		return nil, badRequest("the template has no version to export yet")
	}
	pick := vers[0]
	for _, v := range vers { // newest first: take the first published one
		if v["published_at"] != nil {
			pick = v
			break
		}
	}
	raw, _ := json.Marshal(map[string]any{"variable_schema": pick["variable_schema"], "content": pick["content"]})
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, unavailable("template_export", err)
	}
	b.Format, b.Slug = BundleFormat, fmt.Sprint(t["slug"])
	b.Description, _ = t["description"].(string)
	if name, _ := pick["style"].(string); name != "" {
		var css string
		if err := s.Pool.QueryRow(ctx, `SELECT style_css FROM template_versions WHERE id=$1`, pick["id"]).Scan(&css); err == nil && css != "" {
			b.Style = &StyleInput{Name: name, CSS: css}
		}
	}
	return &b, nil
}

// ImportTemplate creates a template from a bundle, as a draft: nothing is published until you
// look at it. slug overrides the bundle's slug. A stylesheet in the bundle is added to the
// project when no stylesheet of that name exists (an existing one is never overwritten).
func (s *Service) ImportTemplate(ctx context.Context, sc *Scope, b Bundle, slug string) (map[string]any, error) {
	if b.Format != BundleFormat {
		return nil, badRequest("this is not a Njiwa-Mail template bundle (format " + BundleFormat + ")")
	}
	if slug == "" {
		slug = b.Slug
	}
	norm := map[string]tmpl.Content{}
	for l, c := range b.Content {
		norm[tmpl.NormalizeLocale(l)] = c
	}
	in := VersionInput{Schema: b.Schema, Content: norm}
	var warnings []string
	if b.Style != nil && b.Style.Name != "" {
		st := *b.Style
		if err := st.check(); err != nil {
			return nil, err
		}
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM template_styles WHERE project_id=$1 AND name=$2)`, sc.ProjectID, st.Name).Scan(&exists); err != nil {
			return nil, unavailable("template_styles", err)
		}
		if exists {
			warnings = append(warnings, "a stylesheet named "+st.Name+" already exists and was kept; check that it suits this template")
		} else if _, err := s.CreateTemplateStyle(ctx, sc, st); err != nil {
			return nil, err
		}
		in.Style = st.Name
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	id, err := s.CreateTemplate(ctx, TemplateInput{Project: sc.Project, Slug: slug, Description: b.Description})
	if err != nil {
		return nil, err
	}
	v, err := s.SaveDraft(ctx, id, in)
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM templates WHERE id=$1`, id)
		return nil, err
	}
	return map[string]any{"id": id, "slug": slug, "version": v, "warnings": warnings}, nil
}

// ---------------------------------------------------------------- import from HTML

type HTMLImport struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	HTML        string `json:"html"`
	Locale      string `json:"locale"`
}

var (
	reDangerBlock = regexp.MustCompile(`(?is)<(script|iframe|object|embed|applet|form)\b.*?</\s*(script|iframe|object|embed|applet|form)\s*>`)
	reDangerTag   = regexp.MustCompile(`(?is)</?\s*(script|iframe|object|embed|applet|form|base|meta\s+http-equiv)\b[^>]*>`)
	reEventAttr   = regexp.MustCompile(`(?is)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	reJSURL       = regexp.MustCompile(`(?is)(href|src|action)\s*=\s*("\s*javascript:[^"]*"|'\s*javascript:[^']*'|javascript:[^\s>]*)`)
	reMustache    = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]{0,39})\s*\}\}`)
	reGoAction    = regexp.MustCompile(`\{\{\s*\.([a-z][a-z0-9_]{0,39})\s*\}\}`)
	reTitle       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reStyleHead   = regexp.MustCompile(`(?is)<(style|head)\b.*?</(style|head)\s*>`)
	reBreaks      = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|tr|li|h[1-6]|table)\s*>`)
	reAnchor      = regexp.MustCompile(`(?is)<a\b[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a\s*>`)
	reTags        = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank       = regexp.MustCompile(`\n[ \t]*\n[ \t\n]*`)
)

// sanitizeHTML removes active content from an imported file. It is a best-effort clean-up of a
// document you chose to import, not a security boundary: mail clients ignore scripts anyway.
func sanitizeHTML(h string) string {
	for i := 0; i < 3; i++ {
		h = reDangerBlock.ReplaceAllString(h, "")
	}
	h = reDangerTag.ReplaceAllString(h, "")
	h = reEventAttr.ReplaceAllString(h, "")
	return reJSURL.ReplaceAllString(h, `$1="#"`)
}

// importedHTMLToText derives the plain-text alternative of an HTML body.
func importedHTMLToText(h string) string {
	h = reStyleHead.ReplaceAllString(h, "")
	h = reAnchor.ReplaceAllStringFunc(h, func(m string) string {
		sm := reAnchor.FindStringSubmatch(m)
		label := strings.TrimSpace(reTags.ReplaceAllString(sm[2], ""))
		u, err := url.Parse(html.UnescapeString(sm[1]))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") || label == "" || strings.Contains(label, sm[1]) {
			return label
		}
		return label + " (" + sm[1] + ")"
	})
	h = reBreaks.ReplaceAllString(h, "\n")
	h = reTags.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	lines := strings.Split(strings.ReplaceAll(h, "\r", ""), "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.TrimSpace(reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// ImportHTMLTemplate turns an HTML file into a new template with a draft version. Placeholders
// written as {{name}} become variables; the text body is derived from the HTML.
func (s *Service) ImportHTMLTemplate(ctx context.Context, sc *Scope, in HTMLImport) (map[string]any, error) {
	if len(in.HTML) == 0 || len(in.HTML) > maxImportHTML {
		return nil, badRequest("paste an HTML document of up to 256 KB")
	}
	locale := tmpl.NormalizeLocale(in.Locale)
	if locale == "" {
		locale = "en"
	}
	if !tmpl.ValidLocale(locale) {
		return nil, badRequest("a language is a lower-case tag such as en, sw, fr or pt-br")
	}
	body := sanitizeHTML(in.HTML)
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		if m := reTitle.FindStringSubmatch(body); m != nil {
			subject = strings.Join(strings.Fields(html.UnescapeString(reTags.ReplaceAllString(m[1], ""))), " ")
		}
	}
	if subject == "" {
		return nil, badRequest("give the message a subject (the file has no <title>)")
	}
	// {{name}} -> {{.name}}; any other template action in the file is refused rather than guessed at.
	conv := func(t string) (string, error) {
		t = reMustache.ReplaceAllString(t, "{{.$1}}")
		rest := reGoAction.ReplaceAllString(t, "")
		if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
			return "", badRequest("the file contains a {{ }} that is not a plain variable such as {{name}}; remove or rename it")
		}
		return t, nil
	}
	var err error
	if body, err = conv(body); err != nil {
		return nil, err
	}
	if subject, err = conv(subject); err != nil {
		return nil, err
	}
	text := importedHTMLToText(body)
	if text == "" {
		return nil, badRequest("the file has no visible text")
	}
	vars := map[string]bool{}
	for _, src := range []string{subject, body} {
		for _, m := range reGoAction.FindAllStringSubmatch(src, -1) {
			vars[m[1]] = true
		}
	}
	names := make([]string, 0, len(vars))
	for v := range vars {
		names = append(names, v)
	}
	sort.Strings(names)
	if len(names) > 20 {
		return nil, badRequest("at most 20 variables")
	}
	schema := make(tmpl.Schema, 0, len(names))
	for _, n := range names {
		schema = append(schema, tmpl.VarSpec{Name: n, Type: tmpl.TypeString, MaxLen: 200})
	}
	content := map[string]tmpl.Content{locale: {Subject: subject, HTML: body, Text: text}}
	if locale != "en" {
		content["en"] = content[locale] // English is the fallback and must exist; edit it afterwards
	}
	v := VersionInput{Schema: schema, Content: content}
	if err := v.check(); err != nil {
		return nil, err
	}
	id, err := s.CreateTemplate(ctx, TemplateInput{Project: sc.Project, Slug: in.Slug, Description: in.Description})
	if err != nil {
		return nil, err
	}
	ver, err := s.SaveDraft(ctx, id, v)
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM templates WHERE id=$1`, id)
		return nil, err
	}
	return map[string]any{"id": id, "slug": in.Slug, "version": ver, "variables": names}, nil
}
