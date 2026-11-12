package service

import (
	"testing"
	"time"
)

// RFC 6238 appendix B vectors (SHA-1, secret "12345678901234567890"); the
// RFC lists 8 digits, we use the last 6.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := totpAt(secret, v.unix/30); got != v.code {
			t.Errorf("t=%d: %s, want %s", v.unix, got, v.code)
		}
	}
}

func TestTOTPSkewWindow(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	cur := now.Unix() / 30
	for d, want := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		_, ok := totpMatch(secret, totpAt(secret, cur+d), now)
		if ok != want {
			t.Errorf("step offset %d accepted=%v, want %v", d, ok, want)
		}
	}
	if _, ok := totpMatch(secret, "12345", now); ok {
		t.Error("short code accepted")
	}
}
