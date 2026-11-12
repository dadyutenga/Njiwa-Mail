package dnsauth_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/njiwa-tz/mail/internal/dnsauth"
	"github.com/njiwa-tz/mail/internal/dnsauth/dkimtest"
)

// The fixture was signed by OpenDKIM (c=relaxed/simple, rsa-sha256) in this
// repository's managed-mode E2E run and captured at the receiving side, so it
// proves interoperability with a real signer, not with our own code.
func fixture(t *testing.T) ([]byte, fake) {
	t.Helper()
	msg, err := os.ReadFile("testdata/opendkim-signed.eml")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("testdata/opendkim-signed.key.txt")
	if err != nil {
		t.Fatal(err)
	}
	return msg, fake{txt: map[string][]string{"njiwa._domainkey.notify.example.test": {strings.TrimSpace(string(key))}}}
}

func one(t *testing.T, r fake, msg []byte) DKIMResult {
	t.Helper()
	res := VerifyDKIM(context.Background(), r, msg, time.Now())
	if len(res) != 1 {
		t.Fatalf("want one signature result, got %+v", res)
	}
	return res[0]
}

func TestVerifiesARealOpenDKIMSignature(t *testing.T) {
	msg, r := fixture(t)
	if got := one(t, r, msg); got.Result != "pass" || got.Domain != "notify.example.test" || got.Selector != "njiwa" {
		t.Fatalf("%+v", got)
	}
	// LF line endings (a saved file) verify the same.
	if got := one(t, r, bytes.ReplaceAll(msg, []byte("\r\n"), []byte("\n"))); got.Result != "pass" {
		t.Fatalf("LF endings: %+v", got)
	}
}

func TestDetectsTamperingAndKeyProblems(t *testing.T) {
	msg, r := fixture(t)
	body := bytes.Replace(msg, []byte("hello from the managed MTA test"), []byte("hello from somebody else"), 1)
	if got := one(t, r, body); got.Result != "fail" || !strings.Contains(got.Reason, "body hash") {
		t.Fatalf("changed body: %+v", got)
	}
	subj := bytes.Replace(msg, []byte("Subject: managed e2e"), []byte("Subject: changed"), 1)
	if bytes.Equal(subj, msg) {
		t.Fatal("fixture has no Subject to change")
	}
	if got := one(t, r, subj); got.Result != "fail" || !strings.Contains(got.Reason, "does not verify") {
		t.Fatalf("changed signed header: %+v", got)
	}
	other := fake{txt: map[string][]string{"njiwa._domainkey.notify.example.test": {dkimtest.KeyRecord(t, 2048)}}}
	if got := one(t, other, msg); got.Result != "fail" {
		t.Fatalf("wrong key: %+v", got)
	}
	revoked := fake{txt: map[string][]string{"njiwa._domainkey.notify.example.test": {"v=DKIM1; k=rsa; p="}}}
	if got := one(t, revoked, msg); got.Result != "fail" || !strings.Contains(got.Reason, "revoked") {
		t.Fatalf("revoked key: %+v", got)
	}
	if got := one(t, fake{txt: map[string][]string{}}, msg); got.Result != "permerror" {
		t.Fatalf("no key: %+v", got)
	}
	if res := VerifyDKIM(context.Background(), r, []byte("From: a@b.example\r\n\r\nno signature\r\n"), time.Now()); len(res) != 0 {
		t.Fatalf("unsigned message: %+v", res)
	}
}

// signEd25519 is a minimal test signer (relaxed/relaxed) used only to reach
// the ed25519 and relaxed-body branches; interoperability is proven by the
// OpenDKIM fixture above.
func signEd25519(t *testing.T, priv ed25519.PrivateKey, headers []string, body string) []byte {
	t.Helper()
	cb := body
	lines := strings.Split(strings.ReplaceAll(cb, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(strings.Join(strings.Fields(l), " "), " ")
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			lines[i] = " " + lines[i]
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	canon := strings.Join(lines, "\r\n") + "\r\n"
	bh := sha256.Sum256([]byte(canon))
	sigHdr := "DKIM-Signature: v=1; a=ed25519-sha256; c=relaxed/relaxed; d=ed.example; s=ed; h=from:subject; bh=" +
		base64.StdEncoding.EncodeToString(bh[:]) + "; b="
	h := sha256.New()
	for _, hd := range headers {
		name, val, _ := strings.Cut(hd, ":")
		h.Write([]byte(strings.ToLower(name) + ":" + strings.Join(strings.Fields(val), " ") + "\r\n"))
	}
	name, val, _ := strings.Cut(sigHdr, ":")
	h.Write([]byte(strings.ToLower(name) + ":" + strings.Join(strings.Fields(val), " ")))
	sig := ed25519.Sign(priv, h.Sum(nil))
	return []byte(sigHdr + base64.StdEncoding.EncodeToString(sig) + "\r\n" + strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

func TestEd25519AndRelaxedBody(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	r := fake{txt: map[string][]string{"ed._domainkey.ed.example": {"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)}}}
	msg := signEd25519(t, priv, []string{"From: Someone <a@ed.example>", "Subject:   spaced    subject"}, "line one  \r\n\tindented\t line\r\n\r\n\r\n")
	if got := one(t, r, msg); got.Result != "pass" {
		t.Fatalf("ed25519: %+v", got)
	}
	// Relaxed body: trailing whitespace and extra blank lines do not matter.
	relaxed := bytes.Replace(msg, []byte("line one  \r\n"), []byte("line one\r\n"), 1)
	if got := one(t, r, append(relaxed, []byte("\r\n\r\n")...)); got.Result != "pass" {
		t.Fatalf("relaxed body: %+v", got)
	}
	if got := one(t, r, bytes.Replace(msg, []byte("line one"), []byte("line two"), 1)); got.Result != "fail" {
		t.Fatalf("changed body: %+v", got)
	}
}
