package test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/njiwa-tz/mail/internal/objstore"
)

// memStore is an in-memory object store.
type memStore struct {
	mu     sync.Mutex
	m      map[string][]byte
	getErr error
}

func (s *memStore) Put(_ context.Context, k string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = append([]byte(nil), b...)
	return nil
}
func (s *memStore) Get(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	b, ok := s.m[k]
	if !ok {
		return nil, objstore.ErrNotFound
	}
	return b, nil
}
func (s *memStore) Delete(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
func (s *memStore) DeletePrefix(_ context.Context, p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.m {
		if strings.HasPrefix(k, p) {
			delete(s.m, k)
		}
	}
	return nil
}

func TestAttachmentsInAnObjectStore(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "sacas", "mode": "live", "local_part": "support"})
	inKey, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	addr := e.listener("127.0.0.0/8")
	to := "support@" + inDomain

	// 1. Before a store is configured, an attachment goes to the disk, sealed.
	e.must(smtpSend(addr, "asha@customer.example", []string{to}, inboundMail(to)))
	e.process()
	var idA, aidA, relA string
	e.must(e.pool.QueryRow(ctx, `SELECT a.inbound_message_id::text, a.id::text, a.storage_path FROM inbound_attachments a`).Scan(&idA, &aidA, &relA))
	fullA := filepath.Join(e.svc.Opts.DataDir, relA)
	if _, err := os.Stat(fullA); err != nil {
		t.Fatalf("disk file: %v", err)
	}

	// 2. With a store, new attachments go there (sealed) and not to the disk; old ones still download.
	store := &memStore{m: map[string][]byte{}}
	e.svc.Opts.ObjectStore = store
	e.must(smtpSend(addr, "ben@customer.example", []string{to}, strings.Replace(inboundMail(to), "reply-1@", "reply-2@", 1)))
	e.process()
	var idB, aidB, relB string
	e.must(e.pool.QueryRow(ctx, `SELECT a.inbound_message_id::text, a.id::text, a.storage_path FROM inbound_attachments a WHERE a.id<>$1`, aidA).Scan(&idB, &aidB, &relB))
	if _, err := os.Stat(filepath.Join(e.svc.Opts.DataDir, relB)); !os.IsNotExist(err) {
		t.Fatalf("a new attachment was written to the disk too: %v", err)
	}
	if b := store.m[relB]; len(b) == 0 || bytes.Contains(b, []byte("%PDF")) {
		t.Fatalf("object missing or stored in clear (%d bytes)", len(b))
	}
	for _, c := range [][2]string{{idB, aidB}, {idA, aidA}} {
		if r := e.do("GET", "/v1/inbound/"+c[0]+"/attachments/"+c[1], inKey, "", nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF-1.4") {
			t.Fatalf("download %s: %d %q", c[1], r.Code, r.Raw)
		}
	}

	// 3. An object store that fails is a 503, not a "not found".
	store.getErr = errors.New("connection refused")
	if r := e.do("GET", "/v1/inbound/"+idB+"/attachments/"+aidB, inKey, "", nil); r.Code != 503 {
		t.Fatalf("store down: %d %s", r.Code, r.Raw)
	}
	store.getErr = nil

	// 4. Migration moves the disk file to the store, removes it from the disk, and is repeatable.
	moved, skipped, err := e.svc.MigrateAttachmentsToStore(ctx, 10)
	if err != nil || moved != 1 || skipped != 0 {
		t.Fatalf("migrate: %d %d %v", moved, skipped, err)
	}
	if _, err := os.Stat(fullA); !os.IsNotExist(err) {
		t.Fatal("disk copy kept after migration")
	}
	if _, ok := store.m[relA]; !ok {
		t.Fatal("object missing after migration")
	}
	if r := e.do("GET", "/v1/inbound/"+idA+"/attachments/"+aidA, inKey, "", nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF-1.4") {
		t.Fatalf("download after migration: %d", r.Code)
	}
	if moved, _, _ := e.svc.MigrateAttachmentsToStore(ctx, 10); moved != 0 {
		t.Fatalf("second migration moved %d", moved)
	}
	e.svc.Opts.ObjectStore = nil
	if _, _, err := e.svc.MigrateAttachmentsToStore(ctx, 10); err == nil {
		t.Fatal("migrating without a store was accepted")
	}
	e.svc.Opts.ObjectStore = store

	// 5. A privacy delete for one sender removes that message's objects and only those.
	e.admOK(admin, "POST", "/admin/v1/w/sacas/live/privacy/delete", map[string]any{"email": "ben@customer.example"})
	if _, ok := store.m[relB]; ok {
		t.Fatal("deleted recipient's object left in the store")
	}
	if _, ok := store.m[relA]; !ok {
		t.Fatal("another sender's object was removed")
	}
	// 6. Deleting the address removes the rest.
	_, err = e.pool.Exec(ctx, `UPDATE inbound_messages SET retention_expires_at = now() - interval '1 minute'`)
	e.must(err)
	if _, err := e.svc.RunRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.m) != 0 {
		t.Fatalf("objects left after retention: %d", len(store.m))
	}
}

// Key rotation re-seals attachment objects in the store, not only files on the disk.
func TestKeyRotationCoversAttachmentsInTheStore(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	e.provision("sacas")
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.admOK(admin, "POST", "/admin/v1/inbound-addresses", map[string]any{"project": "sacas", "mode": "live", "local_part": "support"})
	inKey, _, err := e.svc.CreateKey(ctx, "sacas", "live", []string{"inbound:read"}, "in", 0)
	e.must(err)
	store := &memStore{m: map[string][]byte{}}
	e.svc.Opts.ObjectStore = store
	to := "support@" + inDomain
	e.must(smtpSend(e.listener("127.0.0.0/8"), "asha@customer.example", []string{to}, inboundMail(to)))
	e.process()
	var inID, attID, rel string
	e.must(e.pool.QueryRow(ctx, `SELECT inbound_message_id::text, id::text, storage_path FROM inbound_attachments`).Scan(&inID, &attID, &rel))
	sealedBefore := append([]byte(nil), store.m[rel]...)

	both, onlyNew := rotatedBoxes(t, oldKey)
	e.svc.Box = both
	done, err := e.svc.Rekey(ctx, false)
	e.must(err)
	if done.Unreadable() != 0 || done.Changed() == 0 {
		t.Fatalf("rekey: %+v", done)
	}
	if bytes.Equal(store.m[rel], sealedBefore) {
		t.Fatal("the object in the store was not re-sealed")
	}
	e.svc.Box = onlyNew
	if r := e.do("GET", "/v1/inbound/"+inID+"/attachments/"+attID, inKey, "", nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF") {
		t.Fatalf("attachment with only the new key: %d", r.Code)
	}
}
