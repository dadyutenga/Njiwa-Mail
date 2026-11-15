package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/njiwa-tz/mail/internal/objstore"
)

// Inbound attachment files are sealed (AES-GCM, bound to the attachment id) before they are
// stored, on local disk under DATA_DIR by default, or in an S3-compatible bucket when
// ATTACHMENT_STORE=s3. The stored key is the row's storage_path ("attachments/<message>/<name>")
// in both cases, so switching needs no database change: with a bucket configured, reads fall
// back to the disk for files not yet migrated, and `njiwa-mail attachments migrate` moves them.

// attachPut stores one sealed attachment under rel.
func (s *Service) attachPut(ctx context.Context, rel string, sealed []byte) error {
	if s.Opts.ObjectStore != nil {
		return s.Opts.ObjectStore.Put(ctx, rel, sealed)
	}
	full, err := attachmentPath(s.Opts.DataDir, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	return os.WriteFile(full, sealed, 0o600)
}

// attachGet reads a stored file. inStore tells where it was found (the bucket, or the disk).
func (s *Service) attachGet(ctx context.Context, rel string) (data []byte, inStore bool, err error) {
	if s.Opts.ObjectStore != nil {
		b, err := s.Opts.ObjectStore.Get(ctx, rel)
		if err == nil {
			return b, true, nil
		}
		if !errors.Is(err, objstore.ErrNotFound) {
			return nil, false, err
		}
	}
	if s.Opts.DataDir == "" {
		return nil, false, os.ErrNotExist
	}
	full, err := attachmentPath(s.Opts.DataDir, rel)
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(full)
	return b, false, err
}

// attachDeleteMessage removes every attachment file of one inbound message, from the bucket and
// from the disk. A failure is logged and not returned: the database rows are already gone, and
// retention or a later privacy request tries again for what is left.
func (s *Service) attachDeleteMessage(ctx context.Context, messageID string) {
	if !uuidRe.MatchString(messageID) {
		return
	}
	if s.Opts.ObjectStore != nil {
		if err := s.Opts.ObjectStore.DeletePrefix(ctx, "attachments/"+messageID+"/"); err != nil {
			slog.Warn("attachment files could not be removed from the object store", "err", err)
		}
	}
	if s.Opts.DataDir != "" {
		os.RemoveAll(s.attachmentDir(messageID))
	}
}

// MigrateAttachmentsToStore copies up to limit sealed attachment files from the disk to the
// object store and removes the disk copy once the upload succeeded. Rows are not changed. It
// returns how many files moved. Files written before encryption at rest (not yet sealed) are
// skipped: run `attachments encrypt` first.
func (s *Service) MigrateAttachmentsToStore(ctx context.Context, limit int) (moved, skipped int, err error) {
	if s.Opts.ObjectStore == nil || s.Opts.DataDir == "" {
		return 0, 0, errors.New("no object store is configured (ATTACHMENT_STORE=s3)")
	}
	rows, err := s.Pool.Query(ctx, `SELECT storage_path, encrypted FROM inbound_attachments ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	type item struct {
		rel    string
		sealed bool
	}
	var todo []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.rel, &it.sealed); err != nil {
			rows.Close()
			return 0, 0, err
		}
		todo = append(todo, it)
	}
	rows.Close()
	for _, it := range todo {
		if moved >= limit {
			break
		}
		full, err := attachmentPath(s.Opts.DataDir, it.rel)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			continue // already in the store, or purged
		}
		if !it.sealed {
			skipped++
			continue
		}
		if err := s.Opts.ObjectStore.Put(ctx, it.rel, raw); err != nil {
			return moved, skipped, err
		}
		if err := os.Remove(full); err != nil {
			return moved, skipped, err
		}
		moved++
	}
	return moved, skipped, nil
}
