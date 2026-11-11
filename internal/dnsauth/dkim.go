package dnsauth

import (
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// DKIMKey is a parsed DKIM public-key record (RFC 6376 3.6.1).
type DKIMKey struct {
	KeyType     string `json:"k"`
	Bits        int    `json:"bits"`
	Revoked     bool   `json:"revoked"`
	Fingerprint string `json:"fingerprint,omitempty"` // sha256 of the decoded key, hex, first 16 bytes
	TestingMode bool   `json:"testing,omitempty"`     // t=y
}

// MinRSABits is the smallest RSA key accepted; 2048 is recommended.
const MinRSABits = 1024

// ParseDKIMKey validates one DKIM TXT record. A record whose p= is empty is
// a revoked key: valid syntax, but it cannot authenticate mail.
func ParseDKIMKey(rec string) (DKIMKey, error) {
	k := DKIMKey{KeyType: "rsa"}
	var p *string
	seen := map[string]bool{}
	for i, part := range strings.Split(rec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, val, ok := strings.Cut(part, "=")
		if !ok {
			return k, errors.New("tag without a value: " + part)
		}
		tag = strings.ToLower(strings.TrimSpace(tag))
		// Values may be folded with whitespace; it is not significant.
		val = strings.Join(strings.Fields(val), "")
		if seen[tag] {
			return k, errors.New("tag " + tag + " appears twice")
		}
		seen[tag] = true
		switch tag {
		case "v":
			if i != 0 || val != "DKIM1" {
				return k, errors.New("v=DKIM1 must be the first tag when present")
			}
		case "k":
			k.KeyType = strings.ToLower(val)
			if k.KeyType != "rsa" && k.KeyType != "ed25519" {
				return k, errors.New("k must be rsa or ed25519")
			}
		case "p":
			v := val
			p = &v
		case "t":
			for _, f := range strings.Split(strings.ToLower(val), ":") {
				if f == "y" {
					k.TestingMode = true
				}
			}
		}
	}
	if p == nil {
		return k, errors.New("the p= tag (the public key) is missing")
	}
	if *p == "" {
		k.Revoked = true
		return k, nil
	}
	raw, err := base64.StdEncoding.DecodeString(*p)
	if err != nil {
		return k, errors.New("p= is not valid base64; the value may have been cut off when pasted")
	}
	sum := sha256.Sum256(raw)
	k.Fingerprint = hex.EncodeToString(sum[:16])
	switch k.KeyType {
	case "ed25519":
		if len(raw) != ed25519.PublicKeySize {
			return k, errors.New("an ed25519 key must be 32 bytes")
		}
		k.Bits = 256
	default:
		pub, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			if rk, err2 := x509.ParsePKCS1PublicKey(raw); err2 == nil {
				pub = rk
			} else {
				return k, errors.New("p= is not an RSA public key; the value may have been cut off when pasted")
			}
		}
		rk, ok := pub.(*rsa.PublicKey)
		if !ok {
			return k, errors.New("p= is not an RSA public key")
		}
		k.Bits = rk.N.BitLen()
		if k.Bits < MinRSABits {
			return k, errors.New("the RSA key is too short; use 2048 bits")
		}
	}
	return k, nil
}
