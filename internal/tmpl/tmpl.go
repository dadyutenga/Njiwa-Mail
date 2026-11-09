// Package tmpl holds the code-owned, versioned email templates and their
// strict variable schemas. Customer input is only ever substituted into
// escaped placeholders; it is never evaluated as a template.
package tmpl

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"regexp"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode"
)

//go:embed files/*
var files embed.FS

type Kind int

const (
	KindAuth   Kind = iota // OTP / password reset: short TTL
	KindNotice             // account notice: longer TTL
)

type VarType int

const (
	TString VarType = iota
	TInt
	TURL
)

type VarSpec struct {
	Name      string
	Type      VarType
	Required  bool
	MaxLen    int
	Multiline bool
	Pattern   *regexp.Regexp
	Min, Max  int
}

type Template struct {
	Name       string
	Version    int
	Kind       Kind
	DefaultTTL time.Duration
	MaxTTL     time.Duration
	Vars       []VarSpec
	subject    func(vars map[string]string) string
	html       *htmltemplate.Template
	text       *texttemplate.Template
}

// ValidationError carries a safe, stable reason code (never user content).
type ValidationError struct{ Code string }

func (e *ValidationError) Error() string { return e.Code }

func bad(code string) *ValidationError { return &ValidationError{Code: code} }

var registry = map[string]*Template{}

func key(name string, v int) string { return fmt.Sprintf("%s.v%d", name, v) }

// Lookup returns the pinned template version, or nil.
func Lookup(name string, version int) *Template { return registry[key(name, version)] }

func mustLoad(t *Template) {
	base := fmt.Sprintf("files/%s.v%d", t.Name, t.Version)
	h, err := files.ReadFile(base + ".html.tmpl")
	if err != nil {
		panic(err)
	}
	x, err := files.ReadFile(base + ".txt.tmpl")
	if err != nil {
		panic(err)
	}
	t.html = htmltemplate.Must(htmltemplate.New(t.Name + "-html").Option("missingkey=error").Parse(string(h)))
	t.text = texttemplate.Must(texttemplate.New(t.Name + "-text").Option("missingkey=error").Parse(string(x)))
	registry[key(t.Name, t.Version)] = t
}

var codeRe = regexp.MustCompile(`^[A-Za-z0-9]{4,12}$`)

func init() {
	name := VarSpec{Name: "name", Type: TString, MaxLen: 100}
	mustLoad(&Template{
		Name: "otp", Version: 1, Kind: KindAuth, DefaultTTL: 5 * time.Minute, MaxTTL: 15 * time.Minute,
		Vars: []VarSpec{name,
			{Name: "code", Type: TString, Required: true, MaxLen: 12, Pattern: codeRe},
			{Name: "expires_in_minutes", Type: TInt, Required: true, Min: 1, Max: 60}},
		subject: func(map[string]string) string { return "Your verification code" },
	})
	mustLoad(&Template{
		Name: "password-reset", Version: 1, Kind: KindAuth, DefaultTTL: 5 * time.Minute, MaxTTL: 15 * time.Minute,
		Vars: []VarSpec{name,
			{Name: "reset_url", Type: TURL, Required: true, MaxLen: 2048},
			{Name: "expires_in_minutes", Type: TInt, Required: true, Min: 1, Max: 1440}},
		subject: func(map[string]string) string { return "Reset your password" },
	})
	mustLoad(&Template{
		Name: "account-notice", Version: 1, Kind: KindNotice, DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour,
		Vars: []VarSpec{name,
			{Name: "title", Type: TString, Required: true, MaxLen: 100},
			{Name: "message", Type: TString, Required: true, MaxLen: 1000, Multiline: true}},
		subject: func(v map[string]string) string { return v["title"] },
	})
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
// control-character values are rejected with a safe reason code.
func (t *Template) Validate(in map[string]any, origins []string) (map[string]string, *ValidationError) {
	known := map[string]*VarSpec{}
	for i := range t.Vars {
		known[t.Vars[i].Name] = &t.Vars[i]
	}
	for k := range in {
		if known[k] == nil {
			return nil, bad("unknown_variable")
		}
	}
	out := map[string]string{}
	for _, spec := range t.Vars {
		v, present := in[spec.Name]
		if !present || v == nil {
			if spec.Required {
				return nil, bad("missing_variable")
			}
			continue
		}
		var s string
		switch spec.Type {
		case TInt:
			n, ok := v.(interface{ Int64() (int64, error) })
			if !ok {
				return nil, bad("invalid_variable_type")
			}
			i, err := n.Int64()
			if err != nil || int(i) < spec.Min || int(i) > spec.Max {
				return nil, bad("variable_out_of_range")
			}
			s = fmt.Sprint(i)
		default:
			str, ok := v.(string)
			if !ok {
				return nil, bad("invalid_variable_type")
			}
			if str == "" && spec.Required {
				return nil, bad("missing_variable")
			}
			if len(str) > spec.MaxLen {
				return nil, bad("variable_too_long")
			}
			if hasControl(str, spec.Multiline) {
				return nil, bad("invalid_characters")
			}
			if spec.Pattern != nil && !spec.Pattern.MatchString(str) {
				return nil, bad("invalid_variable_format")
			}
			if spec.Type == TURL {
				if e := validateURL(str, origins); e != nil {
					return nil, e
				}
			}
			s = str
		}
		out[spec.Name] = s
	}
	return out, nil
}

// Render produces the subject and the plain-text and HTML alternatives.
func (t *Template) Render(vars map[string]string) (subject, text, html string, err error) {
	data := map[string]string{}
	for _, s := range t.Vars {
		data[s.Name] = vars[s.Name] // missing optional vars render as empty
	}
	var tb, hb bytes.Buffer
	if err = t.text.Execute(&tb, data); err != nil {
		return
	}
	if err = t.html.Execute(&hb, data); err != nil {
		return
	}
	return t.subject(vars), tb.String(), hb.String(), nil
}
