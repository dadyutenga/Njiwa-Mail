package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Contact forms: a website posts a form to Njiwa-Mail, each submission is
// scored for spam, kept encrypted, and answered from the dashboard through
// the normal sending pipeline (no auto-responder).

const (
	formHoneypotField = "hp_website" // hidden in the page; a person leaves it empty
	formNonceField    = "_nonce"
	maxFormFields     = 30
	maxFormValue      = 5000
	maxFormTotal      = 20000
	maxFormFiles      = 3
	maxFormFileBytes  = 2 << 20
	formNonceTTL      = time.Hour
)

var (
	formKeyRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,60}$`)
	formStates   = map[string]bool{"new": true, "open": true, "replied": true, "closed": true, "spam": true}
	formDigests  = map[string]bool{"none": true, "immediate": true, "hourly": true, "daily": true}
	errFormsOff  = newErr(404, "forms_disabled")
	errFormFound = newErr(404, "not_found")
)

func (s *Service) formsOn() error {
	if !s.Opts.FormsEnabled {
		return errFormsOff
	}
	return nil
}

// ---------------------------------------------------------------- forms

type FormInput struct {
	Name           string   `json:"name"`
	AllowedOrigins []string `json:"allowed_origins"`
	NotifyEmails   []string `json:"notify_emails"`
	Digest         string   `json:"digest"`
	RedirectURL    string   `json:"redirect_url"`
	ReplyFrom      string   `json:"reply_from"`
	RequireNonce   bool     `json:"require_nonce"`
	Enabled        *bool    `json:"enabled"`
}

// NormalizeOrigin reduces an address to scheme://host[:port], or "" if it is
// not an http(s) origin.
func NormalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func (in *FormInput) check() error {
	in.Name = clean(strings.TrimSpace(in.Name), 80)
	if in.Name == "" {
		return badRequest("give the form a name")
	}
	origins := []string{}
	for _, o := range in.AllowedOrigins {
		if strings.TrimSpace(o) == "" {
			continue
		}
		n := NormalizeOrigin(o)
		if n == "" {
			return badRequest("origins look like https://www.example.com")
		}
		origins = append(origins, n)
	}
	in.AllowedOrigins = origins
	emails := []string{}
	for _, e := range in.NotifyEmails {
		if strings.TrimSpace(e) == "" {
			continue
		}
		n, ok := NormalizeEmail(e)
		if !ok {
			return badRequest("notification addresses must be valid email addresses")
		}
		emails = append(emails, n)
	}
	if len(emails) > 10 {
		return badRequest("at most 10 notification addresses")
	}
	in.NotifyEmails = emails
	if in.Digest == "" {
		in.Digest = "none"
	}
	if !formDigests[in.Digest] {
		return badRequest("notifications are none, immediate, hourly or daily")
	}
	if in.Digest != "none" && len(emails) == 0 {
		return badRequest("add the addresses that should be notified")
	}
	in.RedirectURL = strings.TrimSpace(in.RedirectURL)
	if in.RedirectURL != "" && NormalizeOrigin(in.RedirectURL) == "" {
		return badRequest("the thank-you page must be an http(s) address")
	}
	if in.ReplyFrom = strings.ToLower(strings.TrimSpace(in.ReplyFrom)); in.ReplyFrom != "" {
		n, ok := NormalizeEmail(in.ReplyFrom)
		if !ok {
			return badRequest("the reply sender must be a valid email address")
		}
		in.ReplyFrom = n
	}
	return nil
}

const formCols = `name, allowed_origins, notify_emails, digest, redirect_url, reply_from, require_nonce, enabled`

func (s *Service) CreateForm(ctx context.Context, sc *Scope, in FormInput) (string, error) {
	if err := s.formsOn(); err != nil {
		return "", err
	}
	if err := in.check(); err != nil {
		return "", err
	}
	enabled := in.Enabled == nil || *in.Enabled
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO forms(project_id, environment_id, `+formCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (environment_id, name) DO NOTHING RETURNING id::text`,
		sc.ProjectID, sc.EnvID, in.Name, in.AllowedOrigins, in.NotifyEmails, in.Digest, in.RedirectURL, in.ReplyFrom, in.RequireNonce, enabled).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "form_exists", Message: "a form with this name exists"}
	}
	if err != nil {
		return "", unavailable("form_create", err)
	}
	return id, nil
}

func (s *Service) UpdateForm(ctx context.Context, sc *Scope, id string, in FormInput) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	if err := in.check(); err != nil {
		return err
	}
	enabled := in.Enabled == nil || *in.Enabled
	tag, err := s.Pool.Exec(ctx, `UPDATE forms SET (`+formCols+`) = ($3,$4,$5,$6,$7,$8,$9,$10) WHERE id=$1 AND environment_id=$2`,
		id, sc.EnvID, in.Name, in.AllowedOrigins, in.NotifyEmails, in.Digest, in.RedirectURL, in.ReplyFrom, in.RequireNonce, enabled)
	if err != nil {
		if strings.Contains(err.Error(), "forms_environment_id_name_key") {
			return &Error{Status: 409, Code: "form_exists", Message: "a form with this name exists"}
		}
		return unavailable("form_update", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

func (s *Service) DeleteForm(ctx context.Context, sc *Scope, id string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM forms WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("form_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

func (s *Service) ListForms(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	return s.maps(ctx, `SELECT f.id::text AS id, `+formCols+`, f.created_at,
  (SELECT count(*) FROM form_submissions x WHERE x.form_id=f.id) AS submissions,
  (SELECT count(*) FROM form_submissions x WHERE x.form_id=f.id AND x.state='new' AND x.action<>'quarantine') AS unread
FROM forms f WHERE f.environment_id=$1 ORDER BY f.name`, sc.EnvID)
}

// ---------------------------------------------------------------- nonces

// IssueFormNonce hands a page a single-use nonce for its form. It reports
// whether the form exists and which origins may use it.
func (s *Service) IssueFormNonce(ctx context.Context, formID string) (string, []string, error) {
	if err := s.formsOn(); err != nil {
		return "", nil, err
	}
	origins, err := s.formOrigins(ctx, formID)
	if err != nil {
		return "", nil, err
	}
	n, err := randToken(24)
	if err != nil {
		return "", nil, unavailable("form_nonce", err)
	}
	h := sha256.Sum256([]byte(n))
	if _, err := s.Pool.Exec(ctx, `DELETE FROM form_nonces WHERE expires_at < now()`); err != nil {
		return "", nil, unavailable("form_nonce", err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO form_nonces(nonce_hash, form_id, expires_at) VALUES ($1,$2,$3)`, h[:], formID, time.Now().Add(formNonceTTL)); err != nil {
		return "", nil, unavailable("form_nonce", err)
	}
	return n, origins, nil
}

// FormOrigins is the allowed origins of an enabled form, for CORS answers.
func (s *Service) FormOrigins(ctx context.Context, formID string) ([]string, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	return s.formOrigins(ctx, formID)
}

func (s *Service) formOrigins(ctx context.Context, formID string) ([]string, error) {
	if !uuidRe.MatchString(formID) {
		return nil, errFormFound
	}
	var origins []string
	err := s.Pool.QueryRow(ctx, `SELECT allowed_origins FROM forms WHERE id=$1 AND enabled AND NOT EXISTS (SELECT 1 FROM projects ap WHERE ap.id=forms.project_id AND ap.archived_at IS NOT NULL)`, formID).Scan(&origins)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errFormFound
	}
	if err != nil {
		return nil, unavailable("form", err)
	}
	return origins, nil
}

// ---------------------------------------------------------------- submit

type FormFile struct {
	Filename, ContentType string
	Data                  []byte
}

// FormPost is one submission as the endpoint received it.
type FormPost struct {
	FormID, Origin, Referer, IP string
	Fields                      map[string]string
	Files                       []FormFile
}

// FormResult is what the visitor's page may learn: that it was received.
type FormResult struct {
	ID          string `json:"id,omitempty"`
	Status      string `json:"status"`
	RedirectURL string `json:"-"`
}

func (s *Service) formLimit(ctx context.Context, key string, window time.Duration, limit int) error {
	wait, ok, err := s.hitNow(ctx, key, window, limit)
	if err != nil {
		return unavailable("form_rate", err)
	}
	if !ok {
		return &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
	}
	return nil
}

// SubmitForm checks the origin and the limits, validates the fields, scores
// them against spam rules and the workspace filters, and keeps the
// submission unless it is rejected. A filled honeypot is answered as if it
// worked and nothing is kept, so bots learn nothing.
func (s *Service) SubmitForm(ctx context.Context, p FormPost) (*FormResult, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(p.FormID) {
		return nil, errFormFound
	}
	var f struct {
		Name, EnvID, ProjectID, Redirect string
		Origins                          []string
		RequireNonce                     bool
	}
	err := s.Pool.QueryRow(ctx, `SELECT name, environment_id::text, project_id::text, redirect_url, allowed_origins, require_nonce FROM forms WHERE id=$1 AND enabled AND NOT EXISTS (SELECT 1 FROM projects ap WHERE ap.id=forms.project_id AND ap.archived_at IS NOT NULL)`, p.FormID).
		Scan(&f.Name, &f.EnvID, &f.ProjectID, &f.Redirect, &f.Origins, &f.RequireNonce)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errFormFound
	}
	if err != nil {
		return nil, unavailable("form", err)
	}
	// Browsers send Origin (or at least Referer); a page elsewhere is refused.
	// A client that sends neither is not a browser, and is held by the limits.
	origin := NormalizeOrigin(p.Origin)
	if origin == "" {
		origin = NormalizeOrigin(p.Referer)
	}
	if origin != "" {
		ok := false
		for _, o := range f.Origins {
			ok = ok || o == origin
		}
		if !ok {
			return nil, newErr(403, "origin_not_allowed")
		}
	}
	ip := cleanIP(p.IP)
	ipHash := s.Box.MAC("form-ip", []byte(ip))
	for _, b := range []struct {
		key    string
		window time.Duration
		limit  int
	}{
		{"fm:ip:" + p.FormID + ":" + hex.EncodeToString(ipHash[:8]) + ":min", time.Minute, 5},
		{"fm:ip:" + p.FormID + ":" + hex.EncodeToString(ipHash[:8]) + ":hour", time.Hour, 30},
		{"fm:form:" + p.FormID + ":min", time.Minute, 60},
		{"fm:ws:" + f.EnvID + ":min", time.Minute, 300},
	} {
		if err := s.formLimit(ctx, b.key, b.window, b.limit); err != nil {
			return nil, err
		}
	}
	// Fields.
	fields := map[string]string{}
	total := 0
	for k, v := range p.Fields {
		if k == formHoneypotField || k == formNonceField {
			continue
		}
		if !formKeyRe.MatchString(k) {
			return nil, &Error{Status: 422, Code: "invalid_field", Message: "field names are letters, digits, _ . -"}
		}
		if len(v) > maxFormValue {
			return nil, &Error{Status: 422, Code: "field_too_long", Message: "a field is longer than 5000 characters"}
		}
		if total += len(v); total > maxFormTotal {
			return nil, &Error{Status: 422, Code: "form_too_large", Message: "the form is too large"}
		}
		fields[k] = strings.TrimSpace(v)
	}
	if len(fields) > maxFormFields {
		return nil, &Error{Status: 422, Code: "too_many_fields", Message: "at most 30 fields"}
	}
	if len(p.Files) > maxFormFiles {
		return nil, &Error{Status: 422, Code: "too_many_files", Message: "at most 3 files"}
	}
	fileBytes := 0
	for _, fl := range p.Files {
		fileBytes += len(fl.Data)
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(fl.ContentType, ";", 2)[0]))
		if !allowedAttachmentTypes[ct] {
			return nil, &Error{Status: 422, Code: "file_type_not_allowed", Message: "files may be PDF, PNG, JPEG or plain text"}
		}
	}
	if fileBytes > maxFormFileBytes {
		return nil, &Error{Status: 422, Code: "files_too_large", Message: "files may be 2 MiB in all"}
	}
	email, ok := NormalizeEmail(fields["email"])
	if !ok {
		return nil, &Error{Status: 422, Code: "invalid_email", Message: "enter a valid email address in the email field"}
	}
	fields["email"] = email
	// A filled honeypot: answer as if it worked, keep nothing.
	if strings.TrimSpace(p.Fields[formHoneypotField]) != "" {
		return &FormResult{Status: "received", RedirectURL: f.Redirect}, nil
	}
	if err := s.formLimit(ctx, "fm:sender:"+p.FormID+":"+hex.EncodeToString(s.Box.MAC("recipient", []byte(email))[:8])+":min", time.Minute, 3); err != nil {
		return nil, err
	}
	// Nonce.
	tooFast := false
	if nonce := p.Fields[formNonceField]; nonce != "" || f.RequireNonce {
		if nonce == "" {
			return nil, &Error{Status: 422, Code: "nonce_required", Message: "reload the page and try again"}
		}
		h := sha256.Sum256([]byte(nonce))
		var issued time.Time
		err := s.Pool.QueryRow(ctx, `UPDATE form_nonces SET used_at=now() WHERE nonce_hash=$1 AND form_id=$2 AND used_at IS NULL AND expires_at > now() RETURNING issued_at`,
			h[:], p.FormID).Scan(&issued)
		if errors.Is(err, db.ErrNoRows) {
			return nil, &Error{Status: 422, Code: "nonce_invalid", Message: "reload the page and try again"}
		}
		if err != nil {
			return nil, unavailable("form_nonce", err)
		}
		tooFast = time.Since(issued) < 2*time.Second
	}
	// Score and filters.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "email" {
			values = append(values, fields[k])
		}
	}
	v, err := s.getVerifier()
	if err != nil {
		return nil, unavailable("form_verify", err)
	}
	res := scoreSubmission(values, email, v.IsDisposable, tooFast)
	action := actionForScore(res.Score)
	filters, err := s.loadFilters(ctx, f.EnvID)
	if err != nil {
		return nil, err
	}
	for _, fl := range filters {
		if matchFilter(fl, append(append([]string{}, values...), email), email, ip) {
			res.Reasons = append(res.Reasons, "filter:"+fl.Kind)
			action = maxAction(action, fl.Action)
		}
	}
	if action == "reject" {
		return nil, &Error{Status: 422, Code: "spam_rejected", Message: "the message looks like spam and was not accepted"}
	}
	// Keep it.
	id := secure.NewUUID()
	data, _ := json.Marshal(fields)
	sealed, err := s.Box.Seal(data, []byte("form-sub:"+id))
	if err != nil {
		return nil, unavailable("seal", err)
	}
	state := "new"
	if action == "quarantine" {
		state = "spam"
	}
	reasons, _ := json.Marshal(res.Reasons)
	// Routing rules decide priority, tags and assignee. Quarantined messages are left alone.
	outcome := RuleOutcome{Priority: "normal", Tags: []string{}}
	if state != "spam" {
		rules, err := s.loadFormRules(ctx, p.FormID)
		if err != nil {
			return nil, err
		}
		outcome = applyFormRules(rules, fields)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO form_submissions(id, form_id, environment_id, data_enc, sender_hash, sender_masked, ip_hash, spam_score, spam_reasons, action, state, priority, tags, assigned_to)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::uuid)`, id, p.FormID, f.EnvID, sealed, s.Box.MAC("recipient", []byte(email)), maskEmail(email), ipHash, res.Score, reasons, action, state, outcome.Priority, outcome.Tags, outcome.Assignee); err != nil {
		return nil, unavailable("form_submit", err)
	}
	for _, fl := range p.Files {
		fid := secure.NewUUID()
		fs, err := s.Box.Seal(fl.Data, []byte("form-file:"+fid))
		if err != nil {
			return nil, unavailable("seal", err)
		}
		name := clean(strings.TrimSpace(fl.Filename), 120)
		if name == "" {
			name = "file"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO form_files(id, submission_id, filename, content_type, size, data_enc) VALUES ($1,$2,$3,$4,$5,$6)`,
			fid, id, name, strings.ToLower(strings.TrimSpace(strings.SplitN(fl.ContentType, ";", 2)[0])), len(fl.Data), fs); err != nil {
			return nil, unavailable("form_file", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("commit", err)
	}
	return &FormResult{ID: id, Status: "received", RedirectURL: f.Redirect}, nil
}

// ---------------------------------------------------------------- filters

func (s *Service) loadFilters(ctx context.Context, envID string) ([]formFilter, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text, kind, pattern, action FROM form_filters WHERE environment_id=$1 AND enabled`, envID)
	if err != nil {
		return nil, unavailable("form_filters", err)
	}
	defer rows.Close()
	var out []formFilter
	for rows.Next() {
		var f formFilter
		if err := rows.Scan(&f.ID, &f.Kind, &f.Pattern, &f.Action); err != nil {
			return nil, unavailable("form_filters", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) ListFormFilters(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	return s.maps(ctx, `SELECT id::text AS id, kind, pattern, action, enabled, created_at FROM form_filters WHERE environment_id=$1 ORDER BY created_at`, sc.EnvID)
}

type FormFilterInput struct {
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

func (s *Service) CreateFormFilter(ctx context.Context, sc *Scope, in FormFilterInput) (string, error) {
	if err := s.formsOn(); err != nil {
		return "", err
	}
	p, err := validFilter(in.Kind, in.Pattern, in.Action)
	if err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM form_filters WHERE environment_id=$1`, sc.EnvID).Scan(&n); err != nil {
		return "", unavailable("form_filters", err)
	}
	if n >= 200 {
		return "", badRequest("at most 200 filters")
	}
	var id string
	err = s.Pool.QueryRow(ctx, `INSERT INTO form_filters(project_id, environment_id, kind, pattern, action) VALUES ($1,$2,$3,$4,$5) RETURNING id::text`,
		sc.ProjectID, sc.EnvID, in.Kind, p, in.Action).Scan(&id)
	if err != nil {
		return "", unavailable("form_filters", err)
	}
	return id, nil
}

func (s *Service) DeleteFormFilter(ctx context.Context, sc *Scope, id string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM form_filters WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("form_filters", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

func (s *Service) SetFormFilterEnabled(ctx context.Context, sc *Scope, id string, enabled bool) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE form_filters SET enabled=$3 WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, enabled)
	if err != nil {
		return unavailable("form_filters", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

// TestFormFilter is the dry run: how many of the kept submissions (up to the
// latest 500) the filter would have caught, with a masked sample. Nothing is
// changed. IP filters can only be tried against exact addresses, because
// only a keyed hash of each visitor's address is stored.
func (s *Service) TestFormFilter(ctx context.Context, sc *Scope, in FormFilterInput) (map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	p, err := validFilter(in.Kind, in.Pattern, in.Action)
	if err != nil {
		return nil, err
	}
	fl := formFilter{Kind: in.Kind, Pattern: p, Action: in.Action}
	rows, err := s.Pool.Query(ctx, `SELECT id::text, data_enc, sender_masked, ip_hash FROM form_submissions WHERE environment_id=$1 ORDER BY received_at DESC LIMIT 500`, sc.EnvID)
	if err != nil {
		return nil, unavailable("form_filter_test", err)
	}
	defer rows.Close()
	checked, matched := 0, 0
	sample := []map[string]any{}
	var exactIP []byte
	if in.Kind == "ip" {
		if _, err := parseAddr(p); err == nil {
			exactIP = s.Box.MAC("form-ip", []byte(p))
		}
	}
	for rows.Next() {
		var id, masked string
		var enc, iph []byte
		if err := rows.Scan(&id, &enc, &masked, &iph); err != nil {
			return nil, unavailable("form_filter_test", err)
		}
		pt, err := s.Box.Open(enc, []byte("form-sub:"+id))
		if err != nil {
			continue
		}
		var fields map[string]string
		_ = json.Unmarshal(pt, &fields)
		checked++
		var hit bool
		if in.Kind == "ip" {
			hit = exactIP != nil && hmacEqual(exactIP, iph)
		} else {
			var values []string
			for k, v := range fields {
				if k != "email" {
					values = append(values, v)
				}
			}
			hit = matchFilter(fl, append(values, fields["email"]), fields["email"], "")
		}
		if hit {
			matched++
			if len(sample) < 5 {
				sample = append(sample, map[string]any{"id": id, "sender": masked})
			}
		}
	}
	return map[string]any{"checked": checked, "would_match": matched, "action": in.Action, "sample": sample}, rows.Err()
}

// ---------------------------------------------------------------- inbox

type FormSubmissionFilter struct {
	FormID, State, Tag, Priority string
}

func (s *Service) ListFormSubmissions(ctx context.Context, sc *Scope, f FormSubmissionFilter, pg Page) ([]map[string]any, bool, error) {
	if err := s.formsOn(); err != nil {
		return nil, false, err
	}
	if f.FormID != "" && !uuidRe.MatchString(f.FormID) {
		return nil, false, errFormFound
	}
	items, more, err := s.page(ctx, pg, `SELECT x.id::text AS id, x.form_id::text AS form_id, f.name AS form, x.received_at, x.sender_masked, x.spam_score,
  x.spam_reasons, x.action, x.state, x.priority, x.tags, u.email AS assigned_to, x.replied_at,
  (SELECT count(*) FROM form_files ff WHERE ff.submission_id=x.id) AS files
FROM form_submissions x JOIN forms f ON f.id=x.form_id LEFT JOIN users u ON u.id=x.assigned_to
WHERE x.environment_id=$1 AND ($2='' OR x.form_id=NULLIF($2,'')::uuid)
  AND (($3='' AND x.state<>'spam') OR ($3<>'' AND x.state=$3))
  AND ($4='' OR $4=ANY(x.tags)) AND ($5='' OR x.priority=$5)
ORDER BY x.received_at DESC`, sc.EnvID, f.FormID, f.State, strings.ToLower(f.Tag), f.Priority)
	if err != nil {
		return nil, false, err
	}
	for _, it := range items {
		fields, _ := s.submissionFields(ctx, fmt.Sprint(it["id"]))
		msg := fields["message"]
		if len(msg) > 120 {
			msg = msg[:120] + "…"
		}
		it["preview"] = msg
	}
	return items, more, nil
}

func (s *Service) submissionFields(ctx context.Context, id string) (map[string]string, error) {
	var enc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT data_enc FROM form_submissions WHERE id=$1`, id).Scan(&enc); err != nil {
		return nil, err
	}
	pt, err := s.Box.Open(enc, []byte("form-sub:"+id))
	if err != nil {
		return nil, err
	}
	var fields map[string]string
	return fields, json.Unmarshal(pt, &fields)
}

func (s *Service) GetFormSubmission(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(id) {
		return nil, errFormFound
	}
	m, err := s.one(ctx, `SELECT x.id::text AS id, x.form_id::text AS form_id, f.name AS form, f.reply_from, x.received_at, x.sender_masked, x.spam_score,
  x.spam_reasons, x.action, x.state, x.priority, x.tags, x.assigned_to::text AS assigned_to_id, u.email AS assigned_to, x.replied_at
FROM form_submissions x JOIN forms f ON f.id=x.form_id LEFT JOIN users u ON u.id=x.assigned_to
WHERE x.id=$1 AND x.environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	fields, err := s.submissionFields(ctx, id)
	if err != nil {
		return nil, unavailable("form_submission", err)
	}
	m["fields"] = fields
	if m["files"], err = s.maps(ctx, `SELECT id::text AS id, filename, content_type, size FROM form_files WHERE submission_id=$1 ORDER BY filename`, id); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateFormSubmission sets the state and/or the assignee ("" unassigns).
func (s *Service) UpdateFormSubmission(ctx context.Context, sc *Scope, id string, state *string, assignee *string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var cur string
	if err := tx.QueryRow(ctx, `SELECT state FROM form_submissions WHERE id=$1 AND environment_id=$2 FOR UPDATE`, id, sc.EnvID).Scan(&cur); err != nil {
		return notFound(err)
	}
	if state != nil {
		if !formStates[*state] {
			return badRequest("the state is new, open, replied, closed or spam")
		}
		if _, err := tx.Exec(ctx, `UPDATE form_submissions SET state=$2 WHERE id=$1`, id, *state); err != nil {
			return unavailable("form_submission", err)
		}
	}
	if assignee != nil {
		if *assignee == "" {
			if _, err := tx.Exec(ctx, `UPDATE form_submissions SET assigned_to=NULL WHERE id=$1`, id); err != nil {
				return unavailable("form_submission", err)
			}
		} else {
			if !uuidRe.MatchString(*assignee) {
				return badRequest("unknown user")
			}
			tag, err := tx.Exec(ctx, `UPDATE form_submissions SET assigned_to=$2 WHERE id=$1 AND EXISTS (SELECT 1 FROM users WHERE id=$2 AND NOT disabled)`, id, *assignee)
			if err != nil {
				return unavailable("form_submission", err)
			}
			if tag.RowsAffected() == 0 {
				return badRequest("unknown user")
			}
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) DeleteFormSubmission(ctx context.Context, sc *Scope, id string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM form_submissions WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("form_submission", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

// OpenFormFile returns an uploaded file's bytes; callers must serve it as an
// opaque download.
func (s *Service) OpenFormFile(ctx context.Context, sc *Scope, submissionID, fileID string) (*AttachmentFile, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(submissionID) || !uuidRe.MatchString(fileID) {
		return nil, errFormFound
	}
	var name string
	var enc []byte
	err := s.Pool.QueryRow(ctx, `SELECT ff.filename, ff.data_enc FROM form_files ff JOIN form_submissions x ON x.id=ff.submission_id
WHERE ff.id=$1 AND x.id=$2 AND x.environment_id=$3`, fileID, submissionID, sc.EnvID).Scan(&name, &enc)
	if err != nil {
		return nil, notFound(err)
	}
	data, err := s.Box.Open(enc, []byte("form-file:"+fileID))
	if err != nil {
		return nil, unavailable("form_file", err)
	}
	return &AttachmentFile{Filename: name, ContentType: "application/octet-stream", Data: data}, nil
}

// ReplyToFormSubmission answers the visitor from the form's reply sender
// through the normal sending pipeline: approved sender, suppressions, quotas
// and the Live recipient rules all apply. There is no auto-responder.
func (s *Service) ReplyToFormSubmission(ctx context.Context, sc *Scope, user, id, subject, text string) (*Accepted, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(id) {
		return nil, errFormFound
	}
	text = strings.TrimSpace(text)
	subject = clean(strings.TrimSpace(subject), 300)
	if text == "" || subject == "" {
		return nil, badRequest("write a subject and a message")
	}
	var from string
	if err := s.Pool.QueryRow(ctx, `SELECT f.reply_from FROM form_submissions x JOIN forms f ON f.id=x.form_id WHERE x.id=$1 AND x.environment_id=$2`, id, sc.EnvID).Scan(&from); err != nil {
		return nil, notFound(err)
	}
	if from == "" {
		return nil, &Error{Status: 409, Code: "reply_from_missing", Message: "choose the sender for replies in the form's settings"}
	}
	fields, err := s.submissionFields(ctx, id)
	if err != nil {
		return nil, unavailable("form_submission", err)
	}
	key := sha256.Sum256([]byte(id + "\n" + subject + "\n" + text))
	acc, err := s.Compose(ctx, sc, user, "form-reply-"+hex.EncodeToString(key[:12]), ComposeInput{From: from, To: fields["email"], Subject: subject, Text: text})
	if err != nil {
		return nil, err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE form_submissions SET state='replied', replied_at=now() WHERE id=$1`, id); err != nil {
		return nil, unavailable("form_submission", err)
	}
	return acc, nil
}

// FormEvent is one new submission for the live stream.
type FormEvent struct {
	ID         string    `json:"id"`
	Form       string    `json:"form"`
	Sender     string    `json:"sender"`
	Action     string    `json:"action"`
	ReceivedAt time.Time `json:"received_at"`
}

// FormSubmissionsSince returns submissions after a (time, id) cursor, oldest
// first. Quarantined ones are not announced.
func (s *Service) FormSubmissionsSince(ctx context.Context, envID string, after time.Time, afterID string, limit int) ([]FormEvent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT x.id::text, f.name, x.sender_masked, x.action, x.received_at FROM form_submissions x JOIN forms f ON f.id=x.form_id
WHERE x.environment_id=$1 AND x.action<>'quarantine' AND (x.received_at, x.id::text) > ($2, $3) ORDER BY x.received_at, x.id LIMIT $4`, envID, after, afterID, limit)
	if err != nil {
		return nil, unavailable("form_stream", err)
	}
	defer rows.Close()
	var out []FormEvent
	for rows.Next() {
		var e FormEvent
		if err := rows.Scan(&e.ID, &e.Form, &e.Sender, &e.Action, &e.ReceivedAt); err != nil {
			return nil, unavailable("form_stream", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
