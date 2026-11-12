package dnsauth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// DKIMResult is the verification of one DKIM-Signature header (RFC 6376).
type DKIMResult struct {
	Domain   string `json:"d"`
	Selector string `json:"s"`
	Result   string `json:"result"` // pass | fail | permerror | temperror
	Reason   string `json:"reason,omitempty"`
}

// MaxSignatures bounds the work spent on one message.
const MaxSignatures = 5

type header struct {
	name string // as written
	raw  string // the full field including the trailing CRLF
}

// splitMessage normalises line endings to CRLF and separates the header
// fields (unfolded into one entry each) from the body.
func splitMessage(raw []byte) ([]header, []byte) {
	msg := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	msg = bytes.ReplaceAll(msg, []byte("\n"), []byte("\r\n"))
	head, body, found := bytes.Cut(msg, []byte("\r\n\r\n"))
	if !found {
		head, body = msg, nil
	}
	var hs []header
	for _, line := range strings.SplitAfter(string(head)+"\r\n", "\r\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(hs) > 0 {
			hs[len(hs)-1].raw += line
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		hs = append(hs, header{name: strings.TrimSpace(name), raw: line})
	}
	return hs, body
}

func isWSP(c byte) bool { return c == ' ' || c == '\t' }

// canonBody implements the simple and relaxed body algorithms (3.4.3, 3.4.4).
func canonBody(body []byte, relaxed bool) []byte {
	lines := strings.Split(string(body), "\r\n")
	if relaxed {
		for i, l := range lines {
			var b strings.Builder
			ws := false
			for j := 0; j < len(l); j++ {
				if isWSP(l[j]) {
					ws = true
					continue
				}
				if ws {
					b.WriteByte(' ') // a run of WSP becomes one SP; trailing WSP is dropped
				}
				ws = false
				b.WriteByte(l[j])
			}
			lines[i] = b.String() // trailing whitespace dropped
		}
	}
	// Remove empty lines at the end of the body.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		if relaxed {
			return nil
		}
		return []byte("\r\n")
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// canonHeader implements the simple and relaxed header algorithms (3.4.1, 3.4.2).
func canonHeader(raw string, relaxed bool) string {
	if !relaxed {
		return raw
	}
	name, value, _ := strings.Cut(raw, ":")
	value = strings.ReplaceAll(value, "\r\n", "")
	var b strings.Builder
	ws := false
	for i := 0; i < len(value); i++ {
		if isWSP(value[i]) {
			ws = true
			continue
		}
		if ws && b.Len() > 0 {
			b.WriteByte(' ')
		}
		ws = false
		b.WriteByte(value[i])
	}
	return strings.ToLower(strings.TrimSpace(name)) + ":" + b.String() + "\r\n"
}

func parseTags(v string) map[string]string {
	tags := map[string]string{}
	for _, part := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, dup := tags[k]; dup {
			tags[k+"\x00dup"] = ""
		}
		tags[k] = strings.TrimSpace(val)
	}
	return tags
}

func stripWS(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }), "")
}

// removeB returns the signature header with the value of its b= tag removed
// (but b= kept), as the signature is computed over it (3.7).
func removeB(raw string) string {
	name, value, _ := strings.Cut(raw, ":")
	parts := strings.Split(value, ";")
	for i, p := range parts {
		k, _, ok := strings.Cut(p, "=")
		if ok && strings.TrimSpace(k) == "b" {
			eq := strings.Index(p, "=")
			parts[i] = p[:eq+1]
			if strings.HasSuffix(p, "\r\n") && !strings.HasSuffix(parts[i], "\r\n") {
				parts[i] += "\r\n"
			}
		}
	}
	return name + ":" + strings.Join(parts, ";")
}

// VerifyDKIM checks every DKIM signature on a message (at most
// MaxSignatures), fetching each public key from DNS. now is used for x=.
func VerifyDKIM(ctx context.Context, r TXTResolver, raw []byte, now time.Time) []DKIMResult {
	hs, body := splitMessage(raw)
	var out []DKIMResult
	for _, h := range hs {
		if !strings.EqualFold(h.name, "DKIM-Signature") {
			continue
		}
		if len(out) == MaxSignatures {
			break
		}
		out = append(out, verifyOne(ctx, r, hs, body, h, now))
	}
	return out
}

func verifyOne(ctx context.Context, r TXTResolver, hs []header, body []byte, sig header, now time.Time) DKIMResult {
	_, value, _ := strings.Cut(sig.raw, ":")
	t := parseTags(value)
	res := DKIMResult{Domain: strings.ToLower(t["d"]), Selector: t["s"], Result: "permerror"}
	for k := range t {
		if strings.HasSuffix(k, "\x00dup") {
			res.Reason = "a tag appears twice"
			return res
		}
	}
	for _, req := range []string{"v", "a", "b", "bh", "d", "h", "s"} {
		if t[req] == "" {
			res.Reason = "the signature lacks the " + req + "= tag"
			return res
		}
	}
	if t["v"] != "1" {
		res.Reason = "unsupported signature version"
		return res
	}
	alg := strings.ToLower(t["a"])
	if alg != "rsa-sha256" && alg != "ed25519-sha256" {
		res.Reason = "unsupported algorithm " + alg + " (rsa-sha256 and ed25519-sha256 are accepted)"
		return res
	}
	hc, bc, _ := strings.Cut(strings.ToLower(t["c"]), "/")
	if hc == "" {
		hc = "simple"
	}
	if bc == "" {
		bc = "simple"
	}
	if (hc != "simple" && hc != "relaxed") || (bc != "simple" && bc != "relaxed") {
		res.Reason = "unknown canonicalization " + t["c"]
		return res
	}
	if x := t["x"]; x != "" {
		if n, err := strconv.ParseInt(x, 10, 64); err == nil && now.Unix() > n {
			res.Result, res.Reason = "fail", "the signature has expired (x=)"
			return res
		}
	}
	signed := strings.Split(strings.ToLower(stripWS(t["h"])), ":")
	hasFrom := false
	for _, n := range signed {
		hasFrom = hasFrom || n == "from"
	}
	if !hasFrom {
		res.Reason = "the From header is not signed"
		return res
	}
	if i := t["i"]; i != "" {
		_, idom, _ := strings.Cut(i, "@")
		idom = strings.ToLower(idom)
		if idom != res.Domain && !strings.HasSuffix(idom, "."+res.Domain) {
			res.Reason = "i= is not within d="
			return res
		}
	}

	// Body hash.
	cb := canonBody(body, bc == "relaxed")
	if l := t["l"]; l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 || n > len(cb) {
			res.Result, res.Reason = "fail", "invalid body length l="
			return res
		}
		cb = cb[:n]
	}
	bh := sha256.Sum256(cb)
	want, err := base64.StdEncoding.DecodeString(stripWS(t["bh"]))
	if err != nil {
		res.Reason = "bh= is not base64"
		return res
	}
	if !bytes.Equal(bh[:], want) {
		res.Result, res.Reason = "fail", "the body hash does not match: the body changed after signing"
		return res
	}

	// Headers, last instance first for repeated names (5.4.2).
	used := map[int]bool{}
	hh := sha256.New()
	for _, n := range signed {
		for i := len(hs) - 1; i >= 0; i-- {
			if !used[i] && strings.EqualFold(hs[i].name, n) {
				used[i] = true
				hh.Write([]byte(canonHeader(hs[i].raw, hc == "relaxed")))
				break
			}
		}
	}
	self := canonHeader(removeB(sig.raw), hc == "relaxed")
	hh.Write([]byte(strings.TrimSuffix(self, "\r\n")))
	digest := hh.Sum(nil)

	sigBytes, err := base64.StdEncoding.DecodeString(stripWS(t["b"]))
	if err != nil {
		res.Reason = "b= is not base64"
		return res
	}
	txts, err := r.LookupTXT(ctx, res.Selector+"._domainkey."+res.Domain)
	if err != nil && !isNotFound(err) {
		res.Result, res.Reason = "temperror", "the DNS lookup for the public key failed"
		return res
	}
	if len(txts) != 1 {
		res.Reason = "expected exactly one key record at " + res.Selector + "._domainkey." + res.Domain
		return res
	}
	key, err := ParseDKIMKey(txts[0])
	if err != nil {
		res.Reason = "the published key is invalid: " + err.Error()
		return res
	}
	if key.Revoked {
		res.Result, res.Reason = "fail", "the key is revoked"
		return res
	}
	switch pub := key.Public.(type) {
	case *rsa.PublicKey:
		if alg != "rsa-sha256" {
			res.Reason = "the key type does not match a="
			return res
		}
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, sigBytes); err != nil {
			res.Result, res.Reason = "fail", "the signature does not verify: a signed header changed, or the key is not the signing key"
			return res
		}
	case ed25519.PublicKey:
		if alg != "ed25519-sha256" {
			res.Reason = "the key type does not match a="
			return res
		}
		if !ed25519.Verify(pub, digest, sigBytes) {
			res.Result, res.Reason = "fail", "the signature does not verify"
			return res
		}
	default:
		res.Reason = "unsupported key"
		return res
	}
	res.Result, res.Reason = "pass", ""
	return res
}

// ErrNoSignature is returned by callers that need a signature and found none.
var ErrNoSignature = errors.New("the message has no DKIM-Signature header")
