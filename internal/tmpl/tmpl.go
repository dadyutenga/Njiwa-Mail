// Package tmpl is the template engine: variable schemas, validation and
// rendering. Templates themselves live in the database as immutable published
// versions (see service/templates.go); this package also carries the seed
// content for the built-in templates in English and Kiswahili.
//
// Caller input is only ever substituted into auto-escaped placeholders. It is
// never evaluated as a template, and templates get no functions that touch
// files, the network or the environment.
package tmpl

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"
	"unicode"
)

//go:embed files/*
var files embed.FS

const (
	TypeString = "string"
	TypeInt    = "int"
	TypeURL    = "url"
	// TypeFields is a list of label/value rows ("Order: 1042", "Amount: TZS 25,000"): any
	// details a caller wants to show. Templates range over it: {{range .details}}{{.label}}{{.value}}{{end}}.
	TypeFields = "fields"

	// Field-list limits. VarSpec.Max is the row limit and VarSpec.MaxLen the value length;
	// zero means these defaults.
	DefaultFieldRows   = 30
	MaxFieldRows       = 50
	DefaultFieldValue  = 500
	MaxFieldValue      = 2000
	MaxFieldLabelBytes = 100

	MaxBodyBytes    = 256 << 10
	MaxSubjectBytes = 300
)

// Locales are the languages the built-in templates are written in. Any other language is
// allowed for your own templates (see ValidLocale); "en" is always the fallback.
var Locales = []string{"en", "sw"}

var localeRe = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)

// ValidLocale accepts a lower-case language tag such as en, sw, fr or pt-br.
func ValidLocale(l string) bool { return localeRe.MatchString(l) }

// NormalizeLocale lower-cases a tag and turns an underscore into a dash (pt_BR -> pt-br).
func NormalizeLocale(l string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(l), "_", "-"))
}

const MaxStyleBytes = 50 << 10

var badCSS = regexp.MustCompile(`(?i)@import|expression\s*\(|javascript:|behavior\s*:|-moz-binding|<|>|\{\{|\}\}|\\`)

// CheckCSS accepts plain CSS only: no imports, scripts, template actions or markup.
func CheckCSS(css string) error {
	if strings.TrimSpace(css) == "" {
		return errors.New("the stylesheet is empty")
	}
	if len(css) > MaxStyleBytes {
		return errors.New("the stylesheet is too large (50 KB at most)")
	}
	if badCSS.MatchString(css) {
		return errors.New("the stylesheet may not contain @import, scripts, escapes, < > or template actions")
	}
	if strings.Count(css, "{") != strings.Count(css, "}") {
		return errors.New("the stylesheet has unbalanced braces")
	}
	return nil
}

var headClose = regexp.MustCompile(`(?i)</head\s*>`)

// InjectStyle puts a <style> block in the HTML: before </head> when the document has one,
// otherwise at the start. An empty css leaves the HTML alone.
func InjectStyle(html, css string) string {
	if strings.TrimSpace(css) == "" {
		return html
	}
	block := "<style>" + css + "</style>"
	if loc := headClose.FindStringIndex(html); loc != nil {
		return html[:loc[0]] + block + html[loc[0]:]
	}
	return block + html
}

// VarSpec describes one allowed variable. It is stored as JSON with a
// template version.
type VarSpec struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Required  bool   `json:"required,omitempty"`
	MaxLen    int    `json:"max_len,omitempty"`
	Multiline bool   `json:"multiline,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Min       int    `json:"min,omitempty"`
	Max       int    `json:"max,omitempty"`
}

type Schema []VarSpec

// Content is one locale of a template version.
type Content struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// ValidationError carries a safe, stable reason code (never user content).
type ValidationError struct{ Code string }

func (e *ValidationError) Error() string { return e.Code }

func bad(code string) *ValidationError { return &ValidationError{Code: code} }

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	patterns sync.Map // pattern string -> *regexp.Regexp
)

func pattern(p string) (*regexp.Regexp, error) {
	if v, ok := patterns.Load(p); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(p) // RE2: linear time, safe for operator-supplied patterns
	if err != nil {
		return nil, err
	}
	patterns.Store(p, re)
	return re, nil
}

// Check validates the schema itself (done when a draft is saved or published).
func (s Schema) Check() error {
	if len(s) > 40 {
		return errors.New("too many variables")
	}
	seen := map[string]bool{}
	for _, v := range s {
		if !nameRe.MatchString(v.Name) {
			return fmt.Errorf("invalid variable name %q", v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("duplicate variable %q", v.Name)
		}
		seen[v.Name] = true
		switch v.Type {
		case TypeInt:
			if v.Min > v.Max {
				return fmt.Errorf("variable %q: min exceeds max", v.Name)
			}
		case TypeString, TypeURL:
			if v.MaxLen <= 0 || v.MaxLen > 4096 {
				return fmt.Errorf("variable %q: max_len must be between 1 and 4096", v.Name)
			}
			if v.Pattern != "" {
				if len(v.Pattern) > 200 {
					return fmt.Errorf("variable %q: pattern too long", v.Name)
				}
				if _, err := pattern(v.Pattern); err != nil {
					return fmt.Errorf("variable %q: invalid pattern", v.Name)
				}
			}
		case TypeFields:
			if v.Max < 0 || v.Max > MaxFieldRows {
				return fmt.Errorf("variable %q: max (rows) must be between 0 and %d", v.Name, MaxFieldRows)
			}
			if v.MaxLen < 0 || v.MaxLen > MaxFieldValue {
				return fmt.Errorf("variable %q: max_len must be between 0 and %d", v.Name, MaxFieldValue)
			}
		default:
			return fmt.Errorf("variable %q: type must be string, int, url or fields", v.Name)
		}
	}
	return nil
}

func hasControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if r == '\n' && allowNewline {
			continue
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return true
		}
	}
	return false
}

// NormalizeOrigin returns scheme://host[:port] for an https URL, lowercased,
// with default port removed. Used both when provisioning and when validating.
func NormalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("origin must be an https URL without credentials")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("origin must not contain a path, query or fragment")
	}
	h := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		return "https://" + h + ":" + p, nil
	}
	return "https://" + h, nil
}

func validateURL(raw string, origins []string) *ValidationError {
	if strings.ContainsAny(raw, " \t\r\n\\") || hasControl(raw, false) {
		return bad("invalid_url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return bad("invalid_url")
	}
	origin := "https://" + strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		origin += ":" + p
	}
	for _, o := range origins {
		if o == origin {
			return nil
		}
	}
	return bad("url_origin_not_approved")
}

// Validate checks variables against the schema and returns them normalised to
// strings. Unknown variables, missing variables, wrong types and oversize or
// control-character values are rejected with a safe reason code. URL variables
// must match one of the environment's approved origins.
func (s Schema) Validate(in map[string]any, origins []string) (map[string]string, *ValidationError) {
	known := map[string]bool{}
	for _, v := range s {
		known[v.Name] = true
	}
	for k := range in {
		if !known[k] {
			return nil, bad("unknown_variable")
		}
	}
	out := map[string]string{}
	for _, spec := range s {
		v, present := in[spec.Name]
		if !present || v == nil {
			if spec.Required {
				return nil, bad("missing_variable")
			}
			continue
		}
		var str string
		switch spec.Type {
		case TypeFields:
			rows, e := fieldRows(v, spec)
			if e != nil {
				return nil, e
			}
			if len(rows) == 0 {
				if spec.Required {
					return nil, bad("missing_variable")
				}
				continue
			}
			b, _ := json.Marshal(rows)
			str = string(b)
		case TypeInt:
			var i int64
			switch n := v.(type) {
			case interface{ Int64() (int64, error) }: // json.Number
				var err error
				if i, err = n.Int64(); err != nil {
					return nil, bad("variable_out_of_range")
				}
			case float64:
				if n != float64(int64(n)) {
					return nil, bad("invalid_variable_type")
				}
				i = int64(n)
			case int:
				i = int64(n)
			default:
				return nil, bad("invalid_variable_type")
			}
			if i < int64(spec.Min) || i > int64(spec.Max) {
				return nil, bad("variable_out_of_range")
			}
			str = fmt.Sprint(i)
		default:
			sv, ok := v.(string)
			if !ok {
				return nil, bad("invalid_variable_type")
			}
			if sv == "" && spec.Required {
				return nil, bad("missing_variable")
			}
			if len(sv) > spec.MaxLen {
				return nil, bad("variable_too_long")
			}
			if hasControl(sv, spec.Multiline) {
				return nil, bad("invalid_characters")
			}
			if spec.Pattern != "" {
				re, err := pattern(spec.Pattern)
				if err != nil || !re.MatchString(sv) {
					return nil, bad("invalid_variable_format")
				}
			}
			if spec.Type == TypeURL {
				if e := validateURL(sv, origins); e != nil {
					return nil, e
				}
			}
			str = sv
		}
		out[spec.Name] = str
	}
	return out, nil
}

// fieldRows reads a fields variable: a list of {"label","value"} objects (kept in order), or
// an object of label: value (rows sorted by label). Values may be strings, numbers or booleans.
func fieldRows(v any, spec VarSpec) ([]map[string]string, *ValidationError) {
	maxRows, maxLen := spec.Max, spec.MaxLen
	if maxRows == 0 {
		maxRows = DefaultFieldRows
	}
	if maxLen == 0 {
		maxLen = DefaultFieldValue
	}
	var rows []map[string]string
	add := func(label string, val any) *ValidationError {
		var s string
		switch x := val.(type) {
		case string:
			s = x
		case bool:
			s = strconv.FormatBool(x)
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		case int:
			s = strconv.Itoa(x)
		case interface{ String() string }: // json.Number
			s = x.String()
		case nil:
			s = ""
		default:
			return bad("invalid_variable_type")
		}
		label = strings.TrimSpace(label)
		if label == "" {
			return bad("missing_variable")
		}
		if len(label) > MaxFieldLabelBytes || len(s) > maxLen {
			return bad("variable_too_long")
		}
		if hasControl(label, false) || hasControl(s, true) {
			return bad("invalid_characters")
		}
		rows = append(rows, map[string]string{"label": label, "value": s})
		return nil
	}
	switch x := v.(type) {
	case []any:
		if len(x) > maxRows {
			return nil, bad("variable_too_long")
		}
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, bad("invalid_variable_type")
			}
			for k := range m {
				if k != "label" && k != "value" {
					return nil, bad("invalid_variable_type")
				}
			}
			label, ok := m["label"].(string)
			if !ok {
				return nil, bad("invalid_variable_type")
			}
			if e := add(label, m["value"]); e != nil {
				return nil, e
			}
		}
	case map[string]any:
		if len(x) > maxRows {
			return nil, bad("variable_too_long")
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if e := add(k, x[k]); e != nil {
				return nil, e
			}
		}
	default:
		return nil, bad("invalid_variable_type")
	}
	return rows, nil
}

// Sample returns placeholder values for previews. URL variables use the first
// approved origin when one is given.
func (s Schema) Sample(origins []string) map[string]string {
	out := map[string]string{}
	for _, v := range s {
		switch v.Type {
		case TypeInt:
			n := v.Min
			if n == 0 && v.Max >= 10 {
				n = 10
			}
			out[v.Name] = fmt.Sprint(n)
		case TypeFields:
			out[v.Name] = `[{"label":"Reference","value":"INV-1042"},{"label":"Amount","value":"TZS 25,000"}]`
		case TypeURL:
			o := "https://app.example.com"
			if len(origins) > 0 {
				o = origins[0]
			}
			out[v.Name] = o + "/sample"
		default:
			val := "Sample " + strings.ReplaceAll(v.Name, "_", " ")
			if v.Name == "code" {
				val = "123456"
			}
			if len(val) > v.MaxLen {
				val = val[:v.MaxLen]
			}
			out[v.Name] = val
		}
	}
	return out
}

func parse(c Content) (*texttemplate.Template, *texttemplate.Template, *htmltemplate.Template, error) {
	if len(c.Subject) == 0 || len(c.Subject) > MaxSubjectBytes {
		return nil, nil, nil, errors.New("subject must be between 1 and 300 bytes")
	}
	if len(c.HTML) == 0 || len(c.Text) == 0 {
		return nil, nil, nil, errors.New("html and text bodies are both required")
	}
	if len(c.HTML) > MaxBodyBytes || len(c.Text) > MaxBodyBytes {
		return nil, nil, nil, errors.New("template body too large")
	}
	st, err := texttemplate.New("subject").Option("missingkey=error").Parse(c.Subject)
	if err != nil {
		return nil, nil, nil, errors.New("subject does not parse")
	}
	tt, err := texttemplate.New("text").Option("missingkey=error").Parse(c.Text)
	if err != nil {
		return nil, nil, nil, errors.New("text body does not parse")
	}
	ht, err := htmltemplate.New("html").Option("missingkey=error").Parse(c.HTML)
	if err != nil {
		return nil, nil, nil, errors.New("html body does not parse")
	}
	return st, tt, ht, nil
}

// Compile checks that content parses and renders with sample values, so a
// version that cannot render is rejected before it is saved or published.
func Compile(c Content, s Schema) error {
	_, _, _, err := Render(c, s, s.Sample(nil))
	return err
}

// Render produces the subject and the plain-text and HTML alternatives.
// Variables not present render as empty strings; variables outside the schema
// are not visible to the template at all.
func Render(c Content, s Schema, vars map[string]string) (subject, text, html string, err error) {
	st, tt, ht, err := parse(c)
	if err != nil {
		return "", "", "", err
	}
	data := map[string]any{}
	for _, v := range s {
		if v.Type != TypeFields {
			data[v.Name] = vars[v.Name]
			continue
		}
		var rows []map[string]string
		if raw := vars[v.Name]; raw != "" {
			if json.Unmarshal([]byte(raw), &rows) != nil {
				return "", "", "", errors.New("a fields variable is not a list of rows")
			}
		}
		data[v.Name] = rows
	}
	var sb, tb, hb bytes.Buffer
	if err = st.Execute(&sb, data); err != nil {
		return "", "", "", errors.New("subject does not render")
	}
	if err = tt.Execute(&tb, data); err != nil {
		return "", "", "", errors.New("text body does not render")
	}
	if err = ht.Execute(&hb, data); err != nil {
		return "", "", "", errors.New("html body does not render")
	}
	// A subject is a single header line whatever the variables contain.
	subject = strings.Join(strings.Fields(sb.String()), " ")
	if subject == "" {
		return "", "", "", errors.New("subject renders empty")
	}
	if len(subject) > 500 {
		subject = subject[:500]
	}
	return subject, tb.String(), hb.String(), nil
}

// Seed is a built-in template installed as a shared, published version 1.
type Seed struct {
	Slug        string
	Description string
	DefaultTTL  time.Duration
	MaxTTL      time.Duration
	Schema      Schema
	Content     map[string]Content // by locale
	// Replaces lists earlier built-in contents of this template. An installation whose newest
	// published version is exactly one of them gets the current content as a new published
	// version; a version an operator changed is never replaced.
	Replaces []map[string]Content
}

// SameContent reports whether two versions have identical content in every locale.
func SameContent(a, b map[string]Content) bool {
	if len(a) != len(b) {
		return false
	}
	for l, x := range a {
		if y, ok := b[l]; !ok || x != y {
			return false
		}
	}
	return true
}

func legacyContent(file string, subjects map[string]string) map[string]Content {
	out := map[string]Content{}
	for _, l := range Locales {
		h, err := files.ReadFile(fmt.Sprintf("files/legacy/%s.%s.html.tmpl", file, l))
		if err != nil {
			panic(err)
		}
		x, err := files.ReadFile(fmt.Sprintf("files/legacy/%s.%s.txt.tmpl", file, l))
		if err != nil {
			panic(err)
		}
		out[l] = Content{Subject: subjects[l], HTML: string(h), Text: string(x)}
	}
	return out
}

func seedContent(slug string, subjects map[string]string) map[string]Content {
	out := map[string]Content{}
	for _, l := range Locales {
		h, err := files.ReadFile(fmt.Sprintf("files/%s.%s.html.tmpl", slug, l))
		if err != nil {
			panic(err)
		}
		x, err := files.ReadFile(fmt.Sprintf("files/%s.%s.txt.tmpl", slug, l))
		if err != nil {
			panic(err)
		}
		out[l] = Content{Subject: subjects[l], HTML: string(h), Text: string(x)}
	}
	return out
}

// Seeds returns the built-in templates: otp, password-reset, account-notice and notification.
func Seeds() []Seed {
	name := VarSpec{Name: "name", Type: TypeString, MaxLen: 100}
	return []Seed{
		{Slug: "otp", Description: "One-time verification code", DefaultTTL: 5 * time.Minute, MaxTTL: 15 * time.Minute,
			Schema: Schema{name,
				{Name: "code", Type: TypeString, Required: true, MaxLen: 12, Pattern: `^[A-Za-z0-9]{4,12}$`},
				{Name: "expires_in_minutes", Type: TypeInt, Required: true, Min: 1, Max: 60},
				{Name: "app_name", Type: TypeString, MaxLen: 60}},
			// The code leads the subject ("821580 is your verification code") so mail apps such as
			// Gmail recognise it and offer to copy it.
			Content: seedContent("otp", map[string]string{
				"en": "{{.code}} is your {{if .app_name}}{{.app_name}} {{end}}verification code",
				"sw": "{{.code}} ni namba yako ya uthibitisho{{if .app_name}} ya {{.app_name}}{{end}}",
			}),
			Replaces: []map[string]Content{legacyContent("otp.v1", map[string]string{"en": "Your verification code", "sw": "Namba yako ya uthibitisho"})}},
		{Slug: "password-reset", Description: "Password reset link", DefaultTTL: 5 * time.Minute, MaxTTL: 15 * time.Minute,
			Schema: Schema{name,
				{Name: "reset_url", Type: TypeURL, Required: true, MaxLen: 2048},
				{Name: "expires_in_minutes", Type: TypeInt, Required: true, Min: 1, Max: 1440}},
			Content: seedContent("password-reset", map[string]string{"en": "Reset your password", "sw": "Badilisha nenosiri lako"})},
		{Slug: "account-notice", Description: "General account notice", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour,
			Schema: Schema{name,
				{Name: "title", Type: TypeString, Required: true, MaxLen: 100},
				{Name: "message", Type: TypeString, Required: true, MaxLen: 1000, Multiline: true}},
			Content: seedContent("account-notice", map[string]string{"en": "{{.title}}", "sw": "{{.title}}"})},
		// A general-purpose message: a title, free text, any label/value details (order numbers,
		// amounts, dates, references), an optional button and footer.
		{Slug: "notification", Description: "General message with any details", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour,
			Schema: Schema{name,
				{Name: "title", Type: TypeString, Required: true, MaxLen: 150},
				{Name: "message", Type: TypeString, MaxLen: 2000, Multiline: true},
				{Name: "details", Type: TypeFields, Max: DefaultFieldRows, MaxLen: DefaultFieldValue},
				{Name: "action_url", Type: TypeURL, MaxLen: 2048},
				{Name: "action_label", Type: TypeString, MaxLen: 40},
				{Name: "footer", Type: TypeString, MaxLen: 500, Multiline: true},
				{Name: "app_name", Type: TypeString, MaxLen: 60},
				{Name: "preheader", Type: TypeString, MaxLen: 150}},
			Content: seedContent("notification", map[string]string{
				"en": "{{.title}}",
				"sw": "{{.title}}",
			})},
	}
}
