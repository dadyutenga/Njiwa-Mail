package secure

import (
	"bytes"
	"testing"
)

var hmacKey = []byte("hmac-key-hmac-key-hmac-key-hmac-key!!")

func box(t *testing.T, k string) *Box {
	t.Helper()
	b, err := New([]byte(k), hmacKey)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPreviousKeysOpenButNeverSeal(t *testing.T) {
	oldB := box(t, "0123456789abcdef0123456789abcdef")
	newB := box(t, "fedcba9876543210fedcba9876543210")
	rot, err := newB.WithPrevious([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	sealedOld, _ := oldB.Seal([]byte("secret"), []byte("aad"))
	pt, used, err := rot.OpenReport(sealedOld, []byte("aad"))
	if err != nil || !used || !bytes.Equal(pt, []byte("secret")) {
		t.Fatalf("old data: %q used=%v err=%v", pt, used, err)
	}
	// What the rotated box seals opens with the new key alone.
	sealedNew, _ := rot.Seal([]byte("fresh"), []byte("aad"))
	if pt, used, err := rot.OpenReport(sealedNew, []byte("aad")); err != nil || used || string(pt) != "fresh" {
		t.Fatalf("new data: %q used=%v err=%v", pt, used, err)
	}
	if _, err := newB.Open(sealedNew, []byte("aad")); err != nil {
		t.Fatalf("a rotated box must seal with the current key: %v", err)
	}
	if _, err := newB.Open(sealedOld, []byte("aad")); err == nil {
		t.Fatal("the new key alone opened old data")
	}
	// The AAD still binds the value to its row.
	if _, _, err := rot.OpenReport(sealedOld, []byte("other")); err == nil {
		t.Fatal("wrong aad accepted through a previous key")
	}
	if rot.OldKeyCount() != 1 {
		t.Fatal("old key count")
	}
	if _, err := newB.WithPrevious([]byte("short")); err == nil {
		t.Fatal("a previous key of the wrong length was accepted")
	}
}
