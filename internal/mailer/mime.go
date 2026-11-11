// Package mailer builds MIME messages and submits them over verified-TLS SMTP.
package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

type Message struct {
	FromName  string
	FromEmail string
	To        string
	Subject   string
	Text      string
	HTML      string
	MessageID string // without angle brackets; stable for the life of the message
	// DMMessageID is our message id, sent as X-DM-Message-ID so a returned DSN
	// can be correlated even when the Message-ID was rewritten.
	DMMessageID string
	Date        time.Time
	ReplyTo     string
	CC          []string
	BCC         []string // envelope only; never written as a header
	Attachments []Attachment
	// Extra headers are caller-supplied X- headers. Names are already checked.
	Extra map[string]string
	// System headers are set by Njiwa-Mail itself: In-Reply-To, References,
	// List-Unsubscribe, List-Unsubscribe-Post. Other names are ignored.
	System map[string]string
	// Personal mail (written by an operator) is not marked Auto-Submitted.
	Personal bool
}

var systemHeaders = map[string]bool{"In-Reply-To": true, "References": true, "List-Unsubscribe": true, "List-Unsubscribe-Post": true}

type Attachment struct {
	Filename, ContentType string
	Data                  []byte
}

func noCRLF(s string) bool { return !strings.ContainsAny(s, "\r\n\x00") }

// Build renders an RFC 5322 multipart/alternative message with CRLF line endings.
func Build(m Message) ([]byte, error) {
	check := []string{m.FromName, m.FromEmail, m.To, m.Subject, m.MessageID, m.DMMessageID, m.ReplyTo}
	check = append(check, m.CC...)
	check = append(check, m.BCC...)
	for _, s := range check {
		if !noCRLF(s) {
			return nil, errors.New("header injection rejected")
		}
	}
	var b bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", (&mail.Address{Name: m.FromName, Address: m.FromEmail}).String())
	hdr("To", (&mail.Address{Address: m.To}).String())
	if len(m.CC) > 0 {
		hdr("Cc", strings.Join(m.CC, ", "))
	}
	if m.ReplyTo != "" {
		hdr("Reply-To", m.ReplyTo)
	}
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", m.Date.UTC().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+m.MessageID+">")
	if m.DMMessageID != "" {
		hdr("X-DM-Message-ID", m.DMMessageID)
	}
	hdr("MIME-Version", "1.0")
	if !m.Personal {
		hdr("Auto-Submitted", "auto-generated")
		hdr("X-Auto-Response-Suppress", "All")
	}
	for _, name := range headerNames(m.System) {
		if !systemHeaders[name] {
			continue
		}
		if !noCRLF(m.System[name]) {
			return nil, errors.New("header injection rejected")
		}
		hdr(name, m.System[name])
	}
	for _, name := range headerNames(m.Extra) {
		if !noCRLF(m.Extra[name]) || !noCRLF(name) {
			return nil, errors.New("header injection rejected")
		}
		hdr(name, m.Extra[name])
	}

	alt, altBoundary, err := alternative(m.Text, m.HTML)
	if err != nil {
		return nil, err
	}
	if len(m.Attachments) == 0 {
		hdr("Content-Type", "multipart/alternative; boundary="+altBoundary)
		b.WriteString("\r\n")
		b.Write(alt)
		return b.Bytes(), nil
	}
	var rb [12]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return nil, err
	}
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary("dm-" + hex.EncodeToString(rb[:])); err != nil {
		return nil, err
	}
	hdr("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	b.WriteString("\r\n")
	ph := textproto.MIMEHeader{}
	ph.Set("Content-Type", "multipart/alternative; boundary="+altBoundary)
	w, err := mw.CreatePart(ph)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(alt); err != nil {
		return nil, err
	}
	for _, a := range m.Attachments {
		ah := textproto.MIMEHeader{}
		ah.Set("Content-Type", a.ContentType)
		ah.Set("Content-Transfer-Encoding", "base64")
		ah.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
		aw, err := mw.CreatePart(ah)
		if err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for i := 0; i < len(enc); i += 76 {
			end := i + 76
			if end > len(enc) {
				end = len(enc)
			}
			if _, err := fmt.Fprintf(aw, "%s\r\n", enc[i:end]); err != nil {
				return nil, err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func headerNames(h map[string]string) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func alternative(text, html string) ([]byte, string, error) {
	var rb [12]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return nil, "", err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.SetBoundary("dm-" + hex.EncodeToString(rb[:])); err != nil {
		return nil, "", err
	}
	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", html},
	} {
		ph := textproto.MIMEHeader{}
		ph.Set("Content-Type", p.ct)
		ph.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := mw.CreatePart(ph)
		if err != nil {
			return nil, "", err
		}
		qw := quotedprintable.NewWriter(w)
		if _, err := qw.Write([]byte(p.body)); err != nil {
			return nil, "", err
		}
		if err := qw.Close(); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), mw.Boundary(), nil
}
