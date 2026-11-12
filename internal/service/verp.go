package service

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"strings"
	"time"
)

// F06: signed VERP return path.
//
// The SMTP envelope sender of a message is `bounces+<id>.<tag>@<bounce domain>`
// where <id> is the message id (32 hex digits) and <tag> the first 64 bits of an
// HMAC of it. A delivery status notification comes back to that exact address,
// so the message is identified by the envelope the remote server used, not by
// headers the remote server may have rewritten or a third party may have forged.
// A tag that does not verify is never trusted, and its message is not even
// looked up from the headers. Notifications without a tag (older messages,
// servers that drop the extension) still use the message id in the returned headers.

const (
	verpTagLen = 16 // hex digits of the HMAC kept
	verpIDLen  = 32 // hex digits of the message id
)

type verpState int

const (
	verpNone verpState = iota
	verpValid
	verpInvalid
)

func (s *Service) verpTag(compactID string) string {
	return hex.EncodeToString(s.Box.MAC("verp", []byte(compactID)))[:verpTagLen]
}

// VerpAddress is the signed return path for a message, or "" when no bounce
// address is configured.
func (s *Service) VerpAddress(messageID string) string {
	local, domain, ok := splitAddr(s.Opts.BounceAddress)
	if !ok {
		return ""
	}
	compact := strings.ReplaceAll(strings.ToLower(messageID), "-", "")
	if len(compact) != verpIDLen {
		return s.Opts.BounceAddress
	}
	return local + "+" + compact + "." + s.verpTag(compact) + "@" + domain
}

// bounceDomains is the set of domains whose bounce mailbox we serve: the
// installation's own and every per-domain return-path domain (D04). The
// per-domain set is cached for a short time; changing a domain's return path
// takes effect at once in this process and within 30 seconds in the others.
func (s *Service) bounceDomains(ctx context.Context) map[string]bool {
	s.bdMu.Lock()
	defer s.bdMu.Unlock()
	if s.bdSet != nil && time.Since(s.bdAt) < 30*time.Second {
		return s.bdSet
	}
	set := map[string]bool{}
	if _, d, ok := splitAddr(s.Opts.BounceAddress); ok {
		set[d] = true
	}
	if rows, err := s.Pool.Query(ctx, `SELECT DISTINCT bounce_domain FROM domains WHERE bounce_domain <> ''`); err == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				set[d] = true
			}
		}
		rows.Close()
	} else if s.bdSet != nil {
		return s.bdSet // keep the last good set through a database blip
	}
	s.bdSet, s.bdAt = set, time.Now()
	return set
}

func (s *Service) forgetBounceDomains() {
	s.bdMu.Lock()
	s.bdSet = nil
	s.bdMu.Unlock()
}

// bounceLocalExt splits an address on a bounce domain into the extension
// after "+" (if any). ok is false when the address is not a bounce mailbox.
func (s *Service) bounceLocalExt(ctx context.Context, addr string) (ext string, hasExt, ok bool) {
	local, domain, aok := splitAddr(strings.ToLower(strings.TrimSpace(addr)))
	bl, _, bok := splitAddr(s.Opts.BounceAddress)
	if !aok || !bok || local == "" {
		return "", false, false
	}
	// The bounce mailbox can also arrive through an alias onto the inbound domain.
	if !s.bounceDomains(ctx)[domain] && !(s.Opts.InboundDomain != "" && domain == s.Opts.InboundDomain) {
		return "", false, false
	}
	base, ext, has := strings.Cut(local, "+")
	if base != bl {
		return "", false, false
	}
	return ext, has, true
}

// EnvelopeSender is the SMTP envelope sender (return path) for a message: the
// bounce mailbox of the sending domain's own return-path domain when it has one
// (D04), else the installation's, signed per message when verp is set (F06).
func (s *Service) EnvelopeSender(ctx context.Context, messageID, from string, verp bool) string {
	local, domain, ok := splitAddr(s.Opts.BounceAddress)
	if !ok {
		return from
	}
	if _, fd, fok := splitAddr(strings.ToLower(from)); fok && len(s.bounceDomains(ctx)) > 1 {
		var bd string
		if err := s.Pool.QueryRow(ctx, `SELECT d.bounce_domain FROM messages m JOIN domains d ON d.project_id=m.project_id AND d.name=$2
WHERE m.id=$1 AND d.bounce_domain <> '' AND d.verified_at IS NOT NULL`, messageID, fd).Scan(&bd); err == nil && bd != "" {
			domain = bd
		}
	}
	if !verp {
		return local + "@" + domain
	}
	compact := strings.ReplaceAll(strings.ToLower(messageID), "-", "")
	if len(compact) != verpIDLen {
		return local + "@" + domain
	}
	return local + "+" + compact + "." + s.verpTag(compact) + "@" + domain
}

// verifyVERP inspects the envelope recipients of a returned notification.
func (s *Service) verifyVERP(ctx context.Context, rcpts []string) (messageID string, st verpState) {
	for _, r := range rcpts {
		ext, has, ok := s.bounceLocalExt(ctx, r)
		if !ok || !has {
			continue
		}
		id, tag, found := strings.Cut(ext, ".")
		if !found || len(id) != verpIDLen || len(tag) != verpTagLen || !isHex(id) {
			return "", verpInvalid
		}
		if !hmac.Equal([]byte(tag), []byte(s.verpTag(id))) {
			return "", verpInvalid
		}
		return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], verpValid
	}
	return "", verpNone
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
