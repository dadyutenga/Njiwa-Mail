// Package secure holds the encryption and keyed-HMAC primitives used to protect
// message payloads and idempotency fingerprints.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const sealVersion byte = 1

// Box encrypts payloads with AES-256-GCM and computes keyed HMACs.
type Box struct {
	aead    cipher.AEAD
	hmacKey []byte
}

// New builds a Box. encKey must be 32 bytes, hmacKey at least 32 bytes.
func New(encKey, hmacKey []byte) (*Box, error) {
	if len(encKey) != 32 {
		return nil, errors.New("encryption key must be exactly 32 bytes")
	}
	if len(hmacKey) < 32 {
		return nil, errors.New("hmac key must be at least 32 bytes")
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, hmacKey: append([]byte(nil), hmacKey...)}, nil
}

// Seal encrypts plaintext, binding it to aad (the message ID).
func (b *Box) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts data produced by Seal with the same aad.
func (b *Box) Open(data, aad []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(data) < 1+ns+b.aead.Overhead() || data[0] != sealVersion {
		return nil, errors.New("invalid sealed payload")
	}
	pt, err := b.aead.Open(nil, data[1:1+ns], data[1+ns:], aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %w", err)
	}
	return pt, nil
}

// WithEncryptionKey returns a box that encrypts with encKey and keeps this HMAC key.
func (b *Box) WithEncryptionKey(encKey []byte) (*Box, error) {
	return New(encKey, b.hmacKey)
}

// MAC returns HMAC-SHA256 over domain-separated, length-prefixed parts.
func (b *Box) MAC(domain string, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, b.hmacKey)
	write := func(p []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		h.Write(l[:])
		h.Write(p)
	}
	write([]byte(domain))
	for _, p := range parts {
		write(p)
	}
	return h.Sum(nil)
}

// NewUUID returns a random RFC 4122 version-4 UUID string.
func NewUUID() string {
	var u [16]byte
	if _, err := io.ReadFull(rand.Reader, u[:]); err != nil {
		panic(err)
	}
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}
