// Package mailer builds MIME messages and submits them over verified-TLS SMTP.
package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
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
	MessageID string // without angle brackets
	Date      time.Time
}

func noCRLF(s string) bool { return !strings.ContainsAny(s, "\r\n\x00") }

// Build renders an RFC 5322 multipart/alternative message with CRLF line endings.
func Build(m Message) ([]byte, error) {
	for _, s := range []string{m.FromName, m.FromEmail, m.To, m.Subject, m.MessageID} {
		if !noCRLF(s) {
			return nil, errors.New("header injection rejected")
		}
	}
	var b bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", (&mail.Address{Name: m.FromName, Address: m.FromEmail}).String())
	hdr("To", (&mail.Address{Address: m.To}).String())
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", m.Date.UTC().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+m.MessageID+">")
	hdr("MIME-Version", "1.0")
	hdr("Auto-Submitted", "auto-generated")
	hdr("X-Auto-Response-Suppress", "All")

	var rb [12]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return nil, err
	}
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary("dm-" + hex.EncodeToString(rb[:])); err != nil {
		return nil, err
	}
	hdr("Content-Type", "multipart/alternative; boundary="+mw.Boundary())
	b.WriteString("\r\n")
	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		ph := textproto.MIMEHeader{}
		ph.Set("Content-Type", p.ct)
		ph.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := mw.CreatePart(ph)
		if err != nil {
			return nil, err
		}
		qw := quotedprintable.NewWriter(w)
		if _, err := qw.Write([]byte(p.body)); err != nil {
			return nil, err
		}
		if err := qw.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
