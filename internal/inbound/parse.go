package inbound

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"

	"github.com/ditronics-tz/mail/internal/service"
	"github.com/jhillyerd/enmime/v2"
)

const (
	maxHeaderValue = 2000
	maxHeaderCount = 200
)

func cleanStr(s string, max int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
	if max > 0 && len(s) > max {
		s = strings.ToValidUTF8(s[:max], "")
	}
	return s
}

var (
	spamResultRe = regexp.MustCompile(`\[\s*(-?\d+(?:\.\d+)?)\s*/`)
	dmIDRe       = regexp.MustCompile(`(?mi)^X-DM-Message-ID:[ \t]*([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)
	msgIDRe      = regexp.MustCompile(`(?mi)^Message-ID:[ \t]*<([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})@`)
	statusRe     = regexp.MustCompile(`\b([245]\.\d{1,3}\.\d{1,3})\b`)
	emailRe      = regexp.MustCompile(`[^\s<>@"':;,()]+@[^\s<>@"':;,()]+`)
)

// spamScore reads the score Rspamd (Mailcow) recorded on the message.
func spamScore(get func(string) string) *float64 {
	for _, h := range []string{"X-Rspamd-Score", "X-Spam-Score"} {
		if f, err := strconv.ParseFloat(strings.TrimSpace(get(h)), 64); err == nil {
			return &f
		}
	}
	if m := spamResultRe.FindStringSubmatch(get("X-Spamd-Result")); m != nil {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			return &f
		}
	}
	return nil
}

func fileParts(env *enmime.Envelope) []*enmime.Part {
	var out []*enmime.Part
	out = append(out, env.Attachments...)
	out = append(out, env.Inlines...)
	out = append(out, env.OtherParts...)
	return out
}

// CountAttachments returns how many file parts a raw message carries.
func CountAttachments(raw []byte) (int, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	return len(fileParts(env)), nil
}

// ParseMessage turns a raw message into text, HTML, headers and attachments.
func ParseMessage(raw []byte) (*service.ParsedInbound, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	p := &service.ParsedInbound{
		HeaderFrom: cleanStr(env.GetHeader("From"), 500),
		Subject:    cleanStr(env.GetHeader("Subject"), 1000),
		Text:       env.Text,
		HTML:       env.HTML,
		Headers:    map[string][]string{},
		SpamScore:  spamScore(env.GetHeader),
	}
	for i, k := range env.GetHeaderKeys() {
		if i >= maxHeaderCount {
			break
		}
		var vals []string
		for _, v := range env.GetHeaderValues(k) {
			vals = append(vals, cleanStr(v, maxHeaderValue))
		}
		p.Headers[cleanStr(k, 100)] = vals
	}
	for i, part := range fileParts(env) {
		name := part.FileName
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		ct := part.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		p.Attachments = append(p.Attachments, service.ParsedAttachment{Filename: cleanStr(name, 255), ContentType: cleanStr(ct, 200), Content: part.Content})
	}
	return p, nil
}

// redact masks e-mail addresses inside free text kept from a DSN.
func redact(s string) string {
	return emailRe.ReplaceAllStringFunc(s, func(a string) string {
		at := strings.LastIndexByte(a, '@')
		if at < 1 {
			return "***"
		}
		return a[:1] + "***" + a[at:]
	})
}

func ctypeIs(p *enmime.Part, types ...string) bool {
	for _, t := range types {
		if strings.EqualFold(p.ContentType, t) {
			return true
		}
	}
	return false
}

// ParseDSN extracts per-recipient reports from a delivery status notification
// (RFC 3464 multipart/report). A message that is not a parseable DSN yields a
// single report with an empty status, which is recorded as type "unknown" for
// operator review.
func ParseDSN(raw []byte) []service.BounceReport {
	sum := sha256.Sum256(raw)
	dsnID := "sha256:" + hex.EncodeToString(sum[:])
	ourID := ""
	if m := dmIDRe.FindSubmatch(raw); m != nil {
		ourID = strings.ToLower(string(m[1]))
	}
	fallback := func(why, subject string) []service.BounceReport {
		return []service.BounceReport{{MessageID: ourID, DSNID: dsnID, Diagnostic: cleanStr(redact(why+": "+subject), 300)}}
	}
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil || env.Root == nil {
		return fallback("unparseable", "")
	}
	if id := strings.TrimSpace(env.GetHeader("Message-Id")); id != "" {
		dsnID = id
	}
	subject := env.GetHeader("Subject")

	// Our id from the returned original (full message or headers only).
	if ourID == "" {
		for _, part := range env.Root.DepthMatchAll(func(p *enmime.Part) bool { return ctypeIs(p, "message/rfc822", "text/rfc822-headers") }) {
			if m := dmIDRe.FindSubmatch(part.Content); m != nil {
				ourID = strings.ToLower(string(m[1]))
				break
			}
			if m := msgIDRe.FindSubmatch(part.Content); m != nil {
				ourID = strings.ToLower(string(m[1]))
				break
			}
		}
	}
	if ourID == "" {
		// Last resort: a Message-ID of ours quoted anywhere after the DSN's own headers.
		if i := bytes.Index(raw, []byte("\r\n\r\n")); i > 0 {
			if m := msgIDRe.FindSubmatch(raw[i:]); m != nil {
				ourID = strings.ToLower(string(m[1]))
			}
		}
	}

	status := env.Root.DepthMatchFirst(func(p *enmime.Part) bool { return ctypeIs(p, "message/delivery-status") })
	if status == nil {
		return fallback("not_a_dsn", subject)
	}
	var out []service.BounceReport
	rd := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(bytes.TrimLeft(status.Content, "\r\n"), "\r\n\r\n"...))))
	for {
		h, err := rd.ReadMIMEHeader()
		if len(h) == 0 {
			if err != nil {
				break
			}
			continue
		}
		rcpt := h.Get("Final-Recipient")
		if rcpt == "" {
			rcpt = h.Get("Original-Recipient")
		}
		if rcpt != "" {
			if i := strings.IndexByte(rcpt, ';'); i >= 0 {
				rcpt = rcpt[i+1:]
			}
			rcpt = strings.Trim(strings.TrimSpace(rcpt), "<>")
			diag := h.Get("Diagnostic-Code")
			st := strings.TrimSpace(h.Get("Status"))
			if !statusRe.MatchString(st) {
				st = statusRe.FindString(diag)
			} else {
				st = statusRe.FindString(st)
			}
			out = append(out, service.BounceReport{MessageID: ourID, Recipient: cleanStr(rcpt, 320), DSNID: dsnID,
				Action: strings.ToLower(strings.TrimSpace(h.Get("Action"))), Status: st, Diagnostic: cleanStr(redact(diag), 500)})
		}
		if err != nil {
			break
		}
	}
	if len(out) == 0 {
		return fallback("dsn_without_recipient", subject)
	}
	return out
}
