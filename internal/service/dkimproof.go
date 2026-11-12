package service

import (
	"bytes"
	"context"
	"net"
	"net/mail"
	"time"

	"github.com/njiwa-tz/mail/internal/dnsauth"
)

// Signed-mail evidence (backlog D06). A domain's DKIM key being published is
// not proof that its mail is signed. A received message from the domain with
// a valid DKIM signature whose d= aligns with it is.

// MaxVerifyBytes bounds a pasted or received message checked for DKIM.
const MaxVerifyBytes = 2 << 20

// SignedMailCheck is the outcome of checking one received message.
type SignedMailCheck struct {
	FromDomain string               `json:"from_domain"`
	Signatures []dnsauth.DKIMResult `json:"signatures"`
	Aligned    bool                 `json:"aligned"`  // a passing signature aligned with the From domain
	Recorded   bool                 `json:"recorded"` // evidence stored for this domain
	Message    string               `json:"message"`
}

func fromDomain(raw []byte) string {
	m, err := mail.ReadMessage(bytes.NewReader(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))))
	if err != nil {
		return ""
	}
	as, err := mail.ParseAddressList(m.Header.Get("From"))
	if err != nil || len(as) != 1 {
		return ""
	}
	_, d, ok := splitAddr(as[0].Address)
	if !ok {
		return ""
	}
	if n, ok := NormalizeDomain(d); ok {
		return n
	}
	return ""
}

// checkSigned verifies the message against domain: its From must be the
// domain, and a passing signature must align (relaxed, as DMARC does).
func checkSigned(ctx context.Context, r Resolver, domain string, raw []byte) (SignedMailCheck, string) {
	c := SignedMailCheck{FromDomain: fromDomain(raw)}
	c.Signatures = dnsauth.VerifyDKIM(ctx, r, raw, time.Now())
	if c.Signatures == nil {
		c.Signatures = []dnsauth.DKIMResult{}
	}
	switch {
	case c.FromDomain == "":
		c.Message = "The message has no single From address; paste the complete original message, headers included."
		return c, ""
	case c.FromDomain != domain:
		c.Message = "The message is from " + c.FromDomain + ", not " + domain + "."
		return c, ""
	case len(c.Signatures) == 0:
		c.Message = "The message has no DKIM signature: the mail server did not sign it."
		return c, ""
	}
	for _, s := range c.Signatures {
		if s.Result == "pass" && dnsauth.Aligned("r", domain, s.Domain) {
			c.Aligned = true
			c.Message = "The DKIM signature is valid and aligned with " + domain + " (d=" + s.Domain + ", s=" + s.Selector + ")."
			return c, s.Selector
		}
	}
	c.Message = "No valid DKIM signature aligned with " + domain + " was found."
	for _, s := range c.Signatures {
		if s.Result == "pass" {
			c.Message = "The message is signed by " + s.Domain + ", which is not aligned with " + domain + ": DMARC will not pass through DKIM."
			break
		}
		if s.Reason != "" {
			c.Message += " " + s.Domain + ": " + s.Reason + "."
		}
	}
	return c, ""
}

// VerifyReceivedMessage checks a message an operator received (for example
// the "original" of a test sent to their own mailbox) and records the proof
// on the domain when it passes. It never changes DNS results.
func (s *Service) VerifyReceivedMessage(ctx context.Context, sc *Scope, domainID string, raw []byte, r Resolver) (SignedMailCheck, error) {
	if !uuidRe.MatchString(domainID) {
		return SignedMailCheck{}, newErr(404, "not_found")
	}
	if len(raw) == 0 || len(raw) > MaxVerifyBytes {
		return SignedMailCheck{}, badRequest("paste the complete received message (at most 2 MiB)")
	}
	var name string
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1 AND project_id=$2 AND purpose IN ('sending','both')`, domainID, sc.ProjectID).Scan(&name); err != nil {
		return SignedMailCheck{}, notFound(err)
	}
	if r == nil {
		r = net.DefaultResolver
	}
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, sel := checkSigned(vctx, r, name, raw)
	if c.Aligned {
		if _, err := s.Pool.Exec(ctx, `UPDATE domains SET dkim_verified_at=now(), dkim_verified_selector=$2, dkim_verified_source='upload' WHERE id=$1`, domainID, sel); err != nil {
			return c, unavailable("dkim_evidence", err)
		}
		c.Recorded = true
	}
	return c, nil
}

// RecordInboundDKIM checks a message received into a project's inbox: when
// it comes from one of that project's own sending domains with a valid,
// aligned signature, the domain gets signed-mail evidence. Best effort: it
// never blocks or changes inbound processing.
func (s *Service) RecordInboundDKIM(ctx context.Context, rcpts []string, raw []byte, r Resolver) {
	if len(raw) > MaxVerifyBytes {
		return
	}
	from := fromDomain(raw)
	if from == "" {
		return
	}
	if r == nil {
		r = net.DefaultResolver
	}
	seen := map[string]bool{}
	for _, rcpt := range rcpts {
		rt, err := s.route(ctx, rcpt)
		if err != nil || rt == nil || seen[rt.ProjectID] {
			continue
		}
		seen[rt.ProjectID] = true
		var id string
		if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM domains WHERE project_id=$1 AND name=$2 AND purpose IN ('sending','both')`, rt.ProjectID, from).Scan(&id); err != nil {
			continue
		}
		vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		c, sel := checkSigned(vctx, r, from, raw)
		cancel()
		if c.Aligned {
			_, _ = s.Pool.Exec(ctx, `UPDATE domains SET dkim_verified_at=now(), dkim_verified_selector=$2, dkim_verified_source='inbound' WHERE id=$1`, id, sel)
		}
	}
}
