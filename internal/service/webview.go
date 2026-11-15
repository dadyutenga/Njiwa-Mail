package service

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"html"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// WebViewTTL is how long a "view in browser" link works.
const WebViewTTL = 30 * 24 * time.Hour

func (s *Service) webViewToken(messageID string) string {
	mac := s.Box.MAC("web-view", []byte(messageID))
	return messageID + "." + base64.RawURLEncoding.EncodeToString(mac[:16])
}

// WebViewURL is the signed link for a message's browser copy.
func (s *Service) WebViewURL(messageID string) string {
	return strings.TrimRight(s.Opts.PublicURL, "/") + "/v1/view/" + s.webViewToken(messageID)
}

// StoreWebView keeps an encrypted copy of a message's rendered HTML and
// returns its link. Storing again (a retried send) replaces the copy.
func (s *Service) StoreWebView(ctx context.Context, messageID, subject, body string) (string, error) {
	sealed, err := s.Box.Seal([]byte(body), []byte("web-view:"+messageID))
	if err != nil {
		return "", err
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO web_views(message_id, html_enc, subject, expires_at) VALUES ($1,$2,$3,$4)
ON CONFLICT (message_id) DO UPDATE SET html_enc=EXCLUDED.html_enc, subject=EXCLUDED.subject`,
		messageID, sealed, clean(subject, 300), time.Now().Add(WebViewTTL)); err != nil {
		return "", err
	}
	return s.WebViewURL(messageID), nil
}

// WithWebViewLink puts a "view in browser" line at the top of a message.
func WithWebViewLink(url, text, htmlBody string) (string, string) {
	if text != "" {
		text = "View this email in your browser: " + url + "\n\n" + text
	}
	if htmlBody != "" {
		link := `<p style="font-size:12px;color:#666;text-align:center;margin:0 0 12px"><a href="` + html.EscapeString(url) +
			`" style="color:#666">View this email in your browser</a></p>`
		if i := strings.Index(strings.ToLower(htmlBody), "<body"); i >= 0 {
			if j := strings.IndexByte(htmlBody[i:], '>'); j >= 0 {
				k := i + j + 1
				return text, htmlBody[:k] + link + htmlBody[k:]
			}
		}
		htmlBody = link + htmlBody
	}
	return text, htmlBody
}

// OpenWebView returns the subject and HTML behind a signed link. Any bad,
// expired or deleted link is the same 404, so a link reveals nothing.
func (s *Service) OpenWebView(ctx context.Context, token string) (string, string, error) {
	id, _, ok := strings.Cut(token, ".")
	if !ok || !uuidRe.MatchString(id) || !hmac.Equal([]byte(s.webViewToken(id)), []byte(token)) {
		return "", "", newErr(404, "not_found")
	}
	var enc []byte
	var subject string
	err := s.Pool.QueryRow(ctx, `SELECT html_enc, subject FROM web_views WHERE message_id=$1 AND expires_at > now()`, id).Scan(&enc, &subject)
	if errors.Is(err, db.ErrNoRows) {
		return "", "", newErr(404, "not_found")
	}
	if err != nil {
		return "", "", unavailable("web_view", err)
	}
	body, err := s.Box.Open(enc, []byte("web-view:"+id))
	if err != nil {
		return "", "", newErr(404, "not_found")
	}
	return subject, string(body), nil
}
