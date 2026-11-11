// Package dkimtest generates DKIM key records for tests. Never import it from
// production code.
package dkimtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

// KeyRecord returns a DKIM TXT record with a freshly generated RSA public
// key of the given size. For tests only.
func KeyRecord(tb testing.TB, bits int) string {
	tb.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		tb.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		tb.Fatal(err)
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
}
