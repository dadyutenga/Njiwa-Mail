package service

import (
	"strings"
	"testing"
)

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	valid := hashPassword("correct horse battery staple")
	if !verifyPassword(valid, "correct horse battery staple") {
		t.Fatal("valid hash rejected")
	}
	if verifyPassword(valid, "wrong password") {
		t.Fatal("wrong password accepted")
	}
	for _, malformed := range []string{
		strings.Replace(valid, "t=2", "t=0", 1),
		strings.Replace(valid, "p=2", "p=0", 1),
		strings.Replace(valid, "m=65536", "m=1048576", 1),
		valid + "garbage",
		strings.Repeat("x", 1000),
	} {
		if verifyPassword(malformed, "correct horse battery staple") {
			t.Errorf("malformed hash accepted: %q", malformed)
		}
	}
}
