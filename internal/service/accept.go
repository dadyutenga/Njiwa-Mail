package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/njiwa-tz/mail/internal/db"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/njiwa-tz/mail/internal/secure"
	"github.com/njiwa-tz/mail/internal/tmpl"
)

type SendRequest struct {
	From            string         `json:"from"`
	To              string         `json:"to"`
	Template        string         `json:"template"`
	TemplateVersion *int           `json:"template_version"` // optional: omitted pins the newest published version
	Locale          string         `json:"locale"`           // "en" (default) or "sw"
	Variables       map[string]any `json:"variables"`
	TTLSeconds      *int           `json:"ttl_seconds"`
	Metadata        map[string]any `json:"metadata"`
	// Raw sends (emails:send_raw) carry subject and a body instead of a template.
	Subject     string            `json:"subject"`
	HTML        string            `json:"html"`
	Text        string            `json:"text"`
	ReplyTo     string            `json:"reply_to"`
	CC          []string          `json:"cc"`
	BCC         []string          `json:"bcc"`
	Priority    string            `json:"priority"` // high or normal; OTP and password-reset default to high
	SendAt      string            `json:"send_at"`  // RFC3339, at most 7 days ahead
	Attachments []AttachmentIn    `json:"attachments"`
	Headers     map[string]string `json:"headers"`
}

// AttachmentIn is one outbound file. Content is standard base64.
type AttachmentIn struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

var allowedAttachmentTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"text/plain":      true,
}

// Payload is the sensitive content stored encrypted until a terminal outcome.
type Payload struct {
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	Template string `json:"template"`
	Version  int    `json:"version"`
	// VersionID pins the exact template version; Locale selects its content.
	VersionID string            `json:"version_id,omitempty"`
	Locale    string            `json:"locale,omitempty"`
	Vars      map[string]string `json:"vars"`
	Raw       bool              `json:"raw,omitempty"`
	// Confidential bodies (password resets) must never be written to sandbox captures.
	Confidential bool              `json:"confidential,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	HTML         string            `json:"html,omitempty"`
	Text         string            `json:"text,omitempty"`
	ReplyTo      string            `json:"reply_to,omitempty"`
	CC           []string          `json:"cc,omitempty"`
	BCC          []string          `json:"bcc,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	// System headers set by Njiwa-Mail itself (threading, unsubscribe). Never caller input.
	System map[string]string `json:"system,omitempty"`
	// Personal marks mail an operator wrote by hand: no Auto-Submitted header.
	Personal bool `json:"personal,omitempty"`
}

type storedAttachment struct {
	Filename, ContentType string
	Data                  []byte
}

type Accepted struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

var (
	idemRe      = regexp.MustCompile(`^[A-Za-z0-9._:\-]{8,128}$`)
	requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._:\-]{1,128}$`)
)

type validated struct {
	from, fromName, to           string
	tpl                          *tplRef
	locale                       string
	vars                         map[string]string
	ttl                          time.Duration
	requestID                    string
	canonical                    []byte
	raw                          bool
	subject, html, text, replyTo string
	cc, bcc                      []string
	priority                     string
	sendAt                       time.Time
	attachments                  []storedAttachment
	recipients                   []string
	headers                      map[string]string

	// Set by workspace callers (compose, bulk, automations), never by API input.
	source         string
	system         map[string]string
	personal       bool
	campaignID     string
	replyInboundID string
	skipQuota      bool // bulk mail has its own rate; it never uses the transactional quota
	copy           []byte
	transportTest  bool // controlled installation test (C04); server-set only
}

var customHeaderRe = regexp.MustCompile(`^X-[A-Za-z0-9-]{1,40}$`)

func invalid(code string) error { return newErr(422, code) }

func (s *Service) validate(ctx context.Context, p *Principal, body []byte) (*validated, error) {
	var req SendRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, invalid("invalid_request")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("invalid_request")
	}
	return s.validateRequest(ctx, p, req, nil)
}

// validateRequest validates a decoded request. pinned, when set, is a template
// version chosen by the caller (admin test-send) instead of resolved by slug.
func (s *Service) validateRequest(ctx context.Context, p *Principal, req SendRequest, pinned *tplRef) (*validated, error) {
	v := &validated{}
	var ok bool
	if v.to, ok = NormalizeEmail(req.To); !ok {
		return nil, invalid("invalid_recipient")
	}
	if v.from, ok = NormalizeEmail(req.From); !ok {
		return nil, invalid("invalid_sender")
	}
	if v.locale = req.Locale; v.locale == "" {
		v.locale = "en"
	}
	if !tmpl.ValidLocale(v.locale) {
		return nil, invalid("invalid_locale")
	}
	if req.TemplateVersion != nil && *req.TemplateVersion < 1 {
		return nil, invalid("unknown_template")
	}
	if req.Template != "" && (req.Subject != "" || req.HTML != "" || req.Text != "") {
		return nil, invalid("invalid_request")
	}
	raw := pinned == nil && req.Template == "" && (req.Subject != "" || req.HTML != "" || req.Text != "")
	if raw {
		if strings.TrimSpace(req.Subject) == "" || (req.HTML == "" && req.Text == "") || strings.ContainsAny(req.Subject, "\r\n\x00") {
			return nil, invalid("invalid_request")
		}
		v.raw = true
		v.subject, v.html, v.text = req.Subject, req.HTML, req.Text
		v.tpl = &tplRef{Slug: "raw", DefaultTTL: time.Hour, MaxTTL: 7 * 24 * time.Hour}
	} else {
		if v.tpl = pinned; v.tpl == nil {
			var err error
			if v.tpl, err = s.resolveTemplate(ctx, p.ProjectID, req.Template, req.TemplateVersion); err != nil {
				return nil, err
			}
		}
		vars, verr := v.tpl.Schema.Validate(req.Variables, p.Origins)
		if verr != nil {
			return nil, invalid(verr.Code)
		}
		v.vars = vars
	}
	if err := s.extraRecipients(req, v); err != nil {
		return nil, err
	}

	v.ttl = v.tpl.DefaultTTL
	if req.TTLSeconds != nil {
		d := time.Duration(*req.TTLSeconds) * time.Second
		if d < 30*time.Second || d > v.tpl.MaxTTL {
			return nil, invalid("invalid_ttl")
		}
		v.ttl = d
	}
	for k, val := range req.Metadata {
		str, isStr := val.(string)
		if k != "request_id" || !isStr || !requestIDRe.MatchString(str) {
			return nil, invalid("invalid_metadata")
		}
		v.requestID = str
	}

	// Approved sender enforcement.
	var name string
	err := s.Pool.QueryRow(ctx, `SELECT display_name FROM sender_identities WHERE environment_id=$1 AND project_id=$2 AND email=$3`,
		p.EnvironmentID, p.ProjectID, v.from).Scan(&name)
	if errors.Is(err, db.ErrNoRows) {
		return nil, newErr(403, "sender_not_approved")
	}
	if err != nil {
		return nil, unavailable("sender_lookup", err)
	}
	v.fromName = name

	// Keyed canonical fingerprint of the validated request (not a plain digest:
	// OTP-bearing payloads would otherwise be offline-guessable). It records
	// the version the caller ASKED for (0 = newest), so a retry of the same
	// request still matches after a newer version is published.
	asked, locale := 0, ""
	if req.TemplateVersion != nil {
		asked = *req.TemplateVersion
	}
	if v.locale != "en" {
		locale = v.locale
	}
	type canonDoc struct {
		From, To, Template string
		Version            int
		Vars               map[string]string
		TTL                int64
		RequestID          string
		Locale             string   `json:",omitempty"`
		Priority           string   `json:",omitempty"`
		ReplyTo            string   `json:",omitempty"`
		CC, BCC            []string `json:",omitempty"`
		SendAt             int64    `json:",omitempty"`
		Headers            string   `json:",omitempty"`
		RawSubject         string   `json:",omitempty"`
		BodySHA            string   `json:",omitempty"`
		AttachmentSHA      []string `json:",omitempty"`
	}
	doc := canonDoc{From: v.from, To: v.to, Template: v.tpl.Slug, Version: asked, Vars: v.vars,
		TTL: int64(v.ttl / time.Second), RequestID: v.requestID, Locale: locale}
	if v.priority != "" && v.priority != "normal" {
		doc.Priority = v.priority
	}
	doc.ReplyTo, doc.CC, doc.BCC = v.replyTo, v.cc, v.bcc
	if len(v.headers) > 0 {
		doc.Headers = headerCanon(v.headers)
	}
	if !v.sendAt.IsZero() {
		doc.SendAt = v.sendAt.Unix()
	}
	if v.raw {
		sum := sha256.Sum256([]byte(v.subject + "\n" + v.html + "\n" + v.text))
		doc.RawSubject, doc.BodySHA = v.subject, hex.EncodeToString(sum[:])
	}
	for _, a := range v.attachments {
		sum := sha256.Sum256(a.Data)
		doc.AttachmentSHA = append(doc.AttachmentSHA, hex.EncodeToString(sum[:]))
	}
	canon, _ := json.Marshal(doc)
	v.canonical = canon
	return v, nil
}

func (s *Service) extraRecipients(req SendRequest, v *validated) error {
	var err error
	if req.ReplyTo != "" {
		if v.replyTo, err = oneAddr(req.ReplyTo); err != nil {
			return err
		}
	}
	if v.cc, err = manyAddr(req.CC); err != nil {
		return err
	}
	if v.bcc, err = manyAddr(req.BCC); err != nil {
		return err
	}
	if v.headers, err = customHeaders(req.Headers); err != nil {
		return err
	}
	v.recipients = append([]string{v.to}, append(append([]string{}, v.cc...), v.bcc...)...)
	n := len(v.recipients)
	if v.replyTo != "" {
		n++
	}
	if n > 10 {
		return invalid("too_many_recipients")
	}
	switch req.Priority {
	case "":
		if v.tpl.Slug == "otp" || v.tpl.Slug == "password-reset" {
			v.priority = "high"
		} else {
			v.priority = "normal"
		}
	case "high", "normal":
		v.priority = req.Priority
	default:
		return invalid("invalid_priority")
	}
	if req.SendAt != "" {
		t, perr := time.Parse(time.RFC3339, req.SendAt)
		if perr != nil || t.Before(time.Now().Add(-time.Minute)) || t.After(time.Now().Add(7*24*time.Hour)) {
			return invalid("invalid_send_at")
		}
		v.sendAt = t.UTC()
	}
	if len(req.Attachments) > 5 {
		return invalid("too_many_attachments")
	}
	var total int
	for _, a := range req.Attachments {
		name, err := attachmentName(a.Filename, a.ContentType)
		if err != nil {
			return err
		}
		data, derr := base64.StdEncoding.DecodeString(a.Content)
		if derr != nil || !attachmentBytesMatch(a.ContentType, data) {
			return invalid("invalid_attachment")
		}
		if len(data) > 5<<20 {
			return invalid("attachments_too_large")
		}
		total += len(data)
		if total > 5<<20 {
			return invalid("attachments_too_large")
		}
		v.attachments = append(v.attachments, storedAttachment{Filename: name, ContentType: a.ContentType, Data: data})
	}
	return nil
}

// attachmentName accepts one extension that matches the declared type.
// A second suffix such as .pdf.exe or .html is rejected.
func attachmentName(filename, contentType string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "/\\\r\n\x00") || strings.Contains(name, "..") {
		return "", invalid("invalid_attachment")
	}
	if !allowedAttachmentTypes[contentType] {
		return "", invalid("invalid_attachment")
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch contentType {
	case "application/pdf":
		if ext != ".pdf" {
			return "", invalid("invalid_attachment")
		}
	case "image/png":
		if ext != ".png" {
			return "", invalid("invalid_attachment")
		}
	case "image/jpeg":
		if ext != ".jpg" && ext != ".jpeg" {
			return "", invalid("invalid_attachment")
		}
	case "text/plain":
		if ext != ".txt" {
			return "", invalid("invalid_attachment")
		}
	default:
		return "", invalid("invalid_attachment")
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, part := range strings.Split(base, ".") {
		switch strings.ToLower(part) {
		case "html", "htm", "js", "svg", "exe", "sh", "php", "msi", "bat", "cmd":
			return "", invalid("invalid_attachment")
		}
	}
	return name, nil
}

func attachmentBytesMatch(contentType string, data []byte) bool {
	switch contentType {
	case "application/pdf":
		return bytes.HasPrefix(data, []byte("%PDF-"))
	case "image/png":
		return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	case "image/jpeg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "text/plain":
		return len(data) > 0 && !bytes.Contains(data, []byte{0}) && utf8.Valid(data)
	default:
		return false
	}
}

func customHeaders(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > 8 {
		return nil, invalid("invalid_header")
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		if !customHeaderRe.MatchString(name) || strings.ContainsAny(value, "\r\n\x00") || len(value) > 200 {
			return nil, invalid("invalid_header")
		}
		out[name] = value
	}
	return out, nil
}

func headerCanon(h map[string]string) string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(h[name])
		b.WriteByte('\n')
	}
	return b.String()
}

func oneAddr(s string) (string, error) {
	e, ok := NormalizeEmail(s)
	if !ok {
		return "", invalid("invalid_recipient")
	}
	return e, nil
}

func manyAddr(in []string) ([]string, error) {
	var out []string
	for _, s := range in {
		e, err := oneAddr(s)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func allowlisted(list []string, email string) bool {
	at := strings.LastIndexByte(email, '@')
	for _, e := range list {
		if e == email || (strings.HasPrefix(e, "@") && e == email[at:]) {
			return true
		}
	}
	return false
}

// Accept validates, then atomically (quota + message + event) persists a
// message. It returns only after the transaction has committed.
func (s *Service) Accept(ctx context.Context, p *Principal, idemKey string, body []byte) (*Accepted, error) {
	var probe struct {
		Template, Subject, HTML, Text string
	}
	_ = json.Unmarshal(body, &probe)
	raw := probe.Template == "" && (probe.Subject != "" || probe.HTML != "" || probe.Text != "")
	if raw {
		if !p.HasScope(ScopeSendRaw) {
			return nil, newErr(403, "missing_scope")
		}
	} else if !p.HasScope(ScopeSend) {
		return nil, newErr(403, "missing_scope")
	}
	if !idemRe.MatchString(idemKey) {
		return nil, invalid("idempotency_key_invalid")
	}
	v, err := s.validate(ctx, p, body)
	if err != nil {
		return nil, err
	}
	return s.acceptValidated(ctx, p, v, idemKey)
}

func (s *Service) acceptValidated(ctx context.Context, p *Principal, v *validated, idemKey string) (*Accepted, error) {
	reqMAC := s.Box.MAC("request", v.canonical)
	rcptHash := s.Box.MAC("recipient", []byte(v.to))

	for attempt := 0; attempt < 3; attempt++ {
		res, retry, err := s.acceptTx(ctx, p, v, idemKey, reqMAC, rcptHash)
		if err != nil {
			return nil, err
		}
		if !retry {
			return res, nil
		}
	}
	return nil, unavailable("accept_retry", errors.New("idempotency race did not settle"))
}

func (s *Service) acceptTx(ctx context.Context, p *Principal, v *validated, idemKey string, reqMAC, rcptHash []byte) (*Accepted, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, false, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)

	// 1. Idempotent replay is resolved before any quota is charged.
	var exID, exState string
	var exMAC []byte
	err = tx.QueryRow(ctx, `SELECT id::text, state, request_hmac FROM messages WHERE project_id=$1 AND environment_id=$2 AND idempotency_key=$3`,
		p.ProjectID, p.EnvironmentID, idemKey).Scan(&exID, &exState, &exMAC)
	if err == nil {
		if hmac.Equal(exMAC, reqMAC) {
			return &Accepted{ID: exID, Status: exState}, false, nil
		}
		return nil, false, newErr(409, "idempotency_conflict")
	}
	if !errors.Is(err, db.ErrNoRows) {
		return nil, false, unavailable("idem_lookup", err)
	}

	// 2. Recipient policy and suppression, for every To, Cc and Bcc address.
	for _, rcpt := range v.recipients {
		if p.Mode == "live" && p.RestrictRecipients && !allowlisted(p.Allowlist, rcpt) {
			return nil, false, invalid("recipient_not_allowed")
		}
		var one int
		err = tx.QueryRow(ctx, `SELECT 1 FROM suppressions WHERE environment_id=$1 AND recipient_hash=$2`, p.EnvironmentID, s.Box.MAC("recipient", []byte(rcpt))).Scan(&one)
		if err == nil {
			return nil, false, invalid("recipient_suppressed")
		}
		if !errors.Is(err, db.ErrNoRows) {
			return nil, false, unavailable("suppression_lookup", err)
		}
		if v.source == "bulk" {
			err = tx.QueryRow(ctx, `SELECT 1 FROM bulk_unsubscribes WHERE environment_id=$1 AND recipient_hash=$2`, p.EnvironmentID, s.Box.MAC("recipient", []byte(rcpt))).Scan(&one)
			if err == nil {
				return nil, false, invalid("recipient_unsubscribed")
			}
			if !errors.Is(err, db.ErrNoRows) {
				return nil, false, unavailable("unsubscribe_lookup", err)
			}
		}
	}
	if p.Mode == "live" {
		if err := s.domainAllowsLive(ctx, tx, p.ProjectID, v.from); err != nil {
			return nil, false, err
		}
	}

	// 3. Quotas: atomic increments in this transaction. Rolling back on any
	// later failure releases the reservation deterministically.
	scope := p.ProjectID + ":" + p.EnvironmentID
	rk := scope + ":" + string(hexOf(rcptHash))
	quotas := []struct {
		key    string
		window time.Duration
		limit  int
	}{
		{"p:" + scope + ":min", time.Minute, p.PerMinute},
		{"p:" + scope + ":day", 24 * time.Hour, p.PerDay},
		{"r:" + rk + ":min", time.Minute, p.RcptPerMinute},
		{"r:" + rk + ":hour", time.Hour, p.RcptPerHour},
	}
	if v.skipQuota {
		quotas = quotas[2:]
	}
	for _, b := range quotas {
		wait, ok, err := hit(ctx, tx, b.key, b.window, b.limit)
		if err != nil {
			return nil, false, unavailable("rate_limit", err)
		}
		if !ok {
			return nil, false, &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
		}
	}
	// Installation-wide caps protect the server's reputation from a single
	// compromised key or a parallel flood across projects. They apply to all
	// Live mail, bulk included; Sandbox never sends and is not counted.
	if p.Mode == "live" {
		dom := v.from[strings.LastIndexByte(v.from, '@')+1:]
		for _, b := range []struct {
			key, scope string
			window     time.Duration
			limit      int
		}{
			{"g:live:min", "installation", time.Minute, s.Opts.LiveGlobalPerMinute},
			{"g:live:hour", "installation", time.Hour, s.Opts.LiveGlobalPerHour},
			{"d:" + dom + ":min", "sending domain", time.Minute, s.Opts.LiveDomainPerMinute},
			{"d:" + dom + ":hour", "sending domain", time.Hour, s.Opts.LiveDomainPerHour},
		} {
			if b.limit <= 0 {
				continue
			}
			wait, ok, err := hit(ctx, tx, b.key, b.window, b.limit)
			if err != nil {
				return nil, false, unavailable("rate_limit", err)
			}
			if !ok {
				return nil, false, &Error{Status: 429, Code: "rate_limited", RetryAfter: wait,
					Message: "the " + b.scope + " limit for Live mail is reached; try again later"}
			}
		}
	}
	if p.KeyPerMinute > 0 {
		wait, ok, err := hit(ctx, tx, "k:"+p.KeyID+":min", time.Minute, p.KeyPerMinute)
		if err != nil {
			return nil, false, unavailable("rate_limit", err)
		}
		if !ok {
			return nil, false, &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
		}
	}
	for _, rcpt := range v.recipients[1:] {
		hash := s.Box.MAC("recipient", []byte(rcpt))
		key := scope + ":" + string(hexOf(hash))
		for _, b := range []struct {
			key    string
			window time.Duration
			limit  int
		}{
			{"r:" + key + ":min", time.Minute, p.RcptPerMinute},
			{"r:" + key + ":hour", time.Hour, p.RcptPerHour},
		} {
			wait, ok, err := hit(ctx, tx, b.key, b.window, b.limit)
			if err != nil {
				return nil, false, unavailable("rate_limit", err)
			}
			if !ok {
				return nil, false, &Error{Status: 429, Code: "rate_limited", RetryAfter: wait}
			}
		}
	}

	// 4. Durable insert.
	id := secure.NewUUID()
	pl, _ := json.Marshal(Payload{From: v.from, FromName: v.fromName, To: v.to, Template: v.tpl.Slug, Version: v.tpl.Version,
		VersionID: v.tpl.VersionID, Locale: v.locale, Vars: v.vars, Raw: v.raw, Subject: v.subject, HTML: v.html, Text: v.text,
		ReplyTo: v.replyTo, CC: v.cc, BCC: v.bcc, Headers: v.headers, System: v.system, Personal: v.personal})
	sealed, err := s.Box.Seal(pl, []byte(id))
	if err != nil {
		return nil, false, unavailable("seal", err)
	}
	meta := map[string]string{}
	if v.requestID != "" {
		meta["request_id"] = v.requestID
	}
	mj, _ := json.Marshal(meta)
	if v.cc == nil {
		v.cc = []string{}
	}
	if v.bcc == nil {
		v.bcc = []string{}
	}
	state := "queued"
	next := time.Now()
	ttlFrom := time.Now()
	if !v.sendAt.IsZero() && v.sendAt.After(time.Now()) {
		state, next, ttlFrom = "scheduled", v.sendAt, v.sendAt
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO messages (id, project_id, environment_id, api_key_id, template, template_version, state, payload_enc,
                      recipient_hash, request_hmac, idempotency_key, metadata, expires_at, next_attempt_at, locale, template_version_id,
                      priority, send_at, reply_to, cc, bcc, source, sender, recipient_masked, subject, campaign_id, reply_to_inbound_id, transport_test)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, $13::timestamptz + make_interval(secs => $14::float8), $15, $16, NULLIF($17,'')::uuid,
        $18, $19, $20, COALESCE($21::text[], '{}'), COALESCE($22::text[], '{}'), $23, $24, $25, $26, NULLIF($27,'')::uuid, NULLIF($28,'')::uuid, $29)
ON CONFLICT (project_id, environment_id, idempotency_key) DO NOTHING`,
		id, p.ProjectID, p.EnvironmentID, nullable(p.KeyID), v.tpl.Slug, v.tpl.Version, state, sealed, rcptHash, reqMAC, idemKey, mj, ttlFrom, v.ttl.Seconds(), next,
		v.locale, v.tpl.VersionID, v.priority, nullableTime(v.sendAt), v.replyTo, v.cc, v.bcc,
		sourceOf(v), v.from, maskEmail(v.to), operatorSubject(v), v.campaignID, v.replyInboundID, v.transportTest)
	if err != nil {
		return nil, false, unavailable("insert", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, true, nil // lost an idempotency race; re-resolve as a replay
	}
	if _, err := tx.Exec(ctx, `INSERT INTO message_events(message_id, event_type) VALUES ($1,'accepted')`, id); err != nil {
		return nil, false, unavailable("event", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sender_identities SET last_used_at=now() WHERE environment_id=$1 AND email=$2`, p.EnvironmentID, v.from); err != nil {
		return nil, false, unavailable("sender_used", err)
	}
	if v.copy != nil {
		sealedCopy, err := s.Box.Seal(v.copy, []byte("sent:"+id))
		if err != nil {
			return nil, false, unavailable("seal_copy", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sent_copies(message_id, data_enc) VALUES ($1,$2)`, id, sealedCopy); err != nil {
			return nil, false, unavailable("sent_copy", err)
		}
	}
	for _, a := range v.attachments {
		sealedAtt, err := s.Box.Seal(a.Data, []byte("attachment:"+id+":"+a.Filename))
		if err != nil {
			return nil, false, unavailable("seal_attachment", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO message_attachments(id, message_id, filename, content_type, size, data_enc) VALUES ($1,$2,$3,$4,$5,$6)`,
			secure.NewUUID(), id, a.Filename, a.ContentType, len(a.Data), sealedAtt); err != nil {
			return nil, false, unavailable("attachment", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, unavailable("commit", err)
	}
	return &Accepted{ID: id, Status: state}, false, nil
}

func sourceOf(v *validated) string {
	if v.source == "" {
		return "api"
	}
	return v.source
}

// operatorSubject keeps the subject only for mail an operator typed in the
// workspace. Application mail may carry codes in its subject; it is not stored.
func operatorSubject(v *validated) string {
	if v.source == "ui" || v.source == "bulk" {
		return clean(v.subject, 300)
	}
	return ""
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// domainAllowsLive is the Live sending gate; see requireLiveDomain.
func (s *Service) domainAllowsLive(ctx context.Context, tx *db.Tx, projectID, from string) error {
	return s.requireLiveDomain(ctx, tx, projectID, from[strings.LastIndexByte(from, '@')+1:], true)
}

func hexOf(b []byte) []byte {
	const d = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2], out[i*2+1] = d[c>>4], d[c&15]
	}
	return out
}

func hit(ctx context.Context, tx *db.Tx, key string, window time.Duration, limit int) (time.Duration, bool, error) {
	now := time.Now()
	ws := now.Truncate(window)
	var n int
	err := tx.QueryRow(ctx, `
INSERT INTO rate_limit_buckets (scope_key, window_start, count) VALUES ($1,$2,1)
ON CONFLICT (scope_key, window_start) DO UPDATE SET count = rate_limit_buckets.count + 1
RETURNING count`, key, ws).Scan(&n)
	if err != nil {
		return 0, false, err
	}
	if n > limit {
		wait := ws.Add(window).Sub(now)
		if wait < time.Second {
			wait = time.Second
		}
		return wait, false, nil
	}
	return 0, true, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
