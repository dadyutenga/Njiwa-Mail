package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// IngestWindow is how far a signed request's timestamp may be from now.
const IngestWindow = 5 * time.Minute

// RotateIngestSecret creates (or replaces) the environment's ingest secret
// and returns it. It is shown once; only its encrypted form is kept.
func (s *Service) RotateIngestSecret(ctx context.Context, envID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", unavailable("ingest_secret", err)
	}
	secret := "nis_" + base64.RawURLEncoding.EncodeToString(b)
	sealed, err := s.Box.Seal([]byte(secret), []byte("ingest:"+envID))
	if err != nil {
		return "", unavailable("ingest_secret", err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO inbound_ingest(environment_id, secret_enc) VALUES ($1,$2)
ON CONFLICT (environment_id) DO UPDATE SET secret_enc=EXCLUDED.secret_enc, created_at=now(), last_used_at=NULL`, envID, sealed); err != nil {
		return "", unavailable("ingest_secret", err)
	}
	return secret, nil
}

// IngestStatus says whether HTTP ingest is on, never the secret.
func (s *Service) IngestStatus(ctx context.Context, envID string) (map[string]any, error) {
	out := map[string]any{"enabled": false, "path": "/v1/inbound/ingest/" + envID}
	var created time.Time
	var used *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT created_at, last_used_at FROM inbound_ingest WHERE environment_id=$1`, envID).Scan(&created, &used)
	if errors.Is(err, db.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, unavailable("ingest_status", err)
	}
	out["enabled"], out["created_at"], out["last_used_at"] = true, created, used
	return out, nil
}

// DisableIngest removes the secret: every signed request is refused.
func (s *Service) DisableIngest(ctx context.Context, envID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM inbound_ingest WHERE environment_id=$1`, envID)
	return err
}

// IngestSignature is the value of X-Njiwa-Signature for a body: v1=<hex
// HMAC-SHA256(secret, timestamp + "." + body)>.
func IngestSignature(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}

// VerifyIngest checks a signed ingest request and returns a spool id derived
// from the signature: the same request sent twice is the same spool entry.
// Every failure is the same 401, so a caller learns nothing about which part
// was wrong.
func (s *Service) VerifyIngest(ctx context.Context, envID, timestamp, signature string, body []byte) (string, error) {
	deny := newErr(401, "invalid_signature")
	if !uuidRe.MatchString(envID) {
		return "", deny
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > IngestWindow {
		return "", deny
	}
	var enc []byte
	err = s.Pool.QueryRow(ctx, `SELECT secret_enc FROM inbound_ingest WHERE environment_id=$1`, envID).Scan(&enc)
	if errors.Is(err, db.ErrNoRows) {
		return "", deny
	}
	if err != nil {
		return "", unavailable("ingest_verify", err)
	}
	secret, err := s.Box.Open(enc, []byte("ingest:"+envID))
	if err != nil {
		return "", deny
	}
	want := IngestSignature(string(secret), timestamp, body)
	got := ""
	for _, part := range strings.Split(signature, ",") {
		if p := strings.TrimSpace(part); strings.HasPrefix(p, "v1=") {
			got = p
		}
	}
	if !hmac.Equal([]byte(got), []byte(want)) {
		return "", deny
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE inbound_ingest SET last_used_at=now() WHERE environment_id=$1`, envID)
	sum := sha256.Sum256([]byte(envID + ":" + want))
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]), nil
}

// IngestRecipients keeps the recipients that are enabled inbound addresses
// of this environment. Mail for any other environment, or for the bounce
// mailbox, cannot come in through this environment's secret.
func (s *Service) IngestRecipients(ctx context.Context, envID string, rcpts []string) ([]string, error) {
	var out []string
	for _, r := range rcpts {
		a, ok := NormalizeEmail(r)
		if !ok {
			continue
		}
		rt, err := s.route(ctx, a)
		if err != nil {
			return nil, err
		}
		if rt != nil && rt.EnvironmentID == envID {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, invalid("no_inbound_recipient")
	}
	return out, nil
}

// SpoolExists reports whether a spool id was already recorded (a replay).
func (s *Service) SpoolExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM inbound_spool WHERE id=$1)
  OR EXISTS (SELECT 1 FROM inbound_messages WHERE spool_id=$1)`, id).Scan(&ok)
	return ok, err
}
