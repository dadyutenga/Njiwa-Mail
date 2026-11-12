package service

import (
	"crypto/hmac"
	"encoding/hex"
	"strings"
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

// bounceLocalExt splits an address on the bounce domain into the extension
// after "+" (if any). ok is false when the address is not the bounce mailbox.
func (s *Service) bounceLocalExt(addr string) (ext string, hasExt, ok bool) {
	local, domain, aok := splitAddr(strings.ToLower(strings.TrimSpace(addr)))
	bl, bd, bok := splitAddr(s.Opts.BounceAddress)
	if !aok || !bok || local == "" {
		return "", false, false
	}
	// The bounce mailbox can also arrive through an alias onto the inbound domain.
	if domain != bd && !(s.Opts.InboundDomain != "" && domain == s.Opts.InboundDomain) {
		return "", false, false
	}
	base, ext, has := strings.Cut(local, "+")
	if base != bl {
		return "", false, false
	}
	return ext, has, true
}

// verifyVERP inspects the envelope recipients of a returned notification.
func (s *Service) verifyVERP(rcpts []string) (messageID string, st verpState) {
	for _, r := range rcpts {
		ext, has, ok := s.bounceLocalExt(r)
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
