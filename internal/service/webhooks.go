package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Webhooks use the outbox pattern: dm_emit() writes a delivery row inside the
// transaction that changes state, and a worker delivers it later with retries.

var WebhookEvents = []string{"email.submitted", "email.failed", "email.unknown", "email.expired", "email.bounced", "inbound.received"}

// Retry schedule: about 24 hours in total, then the delivery is dead.
var webhookBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour, 10 * time.Hour}

func validEvents(evs []string) error {
	if len(evs) == 0 {
		return badRequest("at least one event type is required")
	}
	for _, e := range evs {
		ok := false
		for _, k := range WebhookEvents {
			ok = ok || k == e
		}
		if !ok {
			return badRequest("unknown event type " + e)
		}
	}
	return nil
}

// PublicIP reports whether ip is a routable public address. Webhooks are
// never sent to loopback, private, link-local, CGNAT or other special ranges.
func PublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32"} {
		if _, n, _ := net.ParseCIDR(cidr); n.Contains(ip) {
			return false
		}
	}
	return true
}

// CheckWebhookURL validates the static shape of a webhook URL. The delivery
// worker additionally checks every resolved address at connect time.
func CheckWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return badRequest("webhook URL must be an https URL without credentials")
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil && !PublicIP(ip) {
		return badRequest("webhook URL must not point at a private or loopback address")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return badRequest("webhook URL must not point at a private or loopback address")
	}
	return nil
}

func newWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b)
}

// CreateWebhook registers an endpoint. The signing secret is returned once.
func (s *Service) CreateWebhook(ctx context.Context, project, mode, rawURL string, events []string) (id, secret string, err error) {
	if err = CheckWebhookURL(rawURL); err != nil {
		return
	}
	if err = validEvents(events); err != nil {
		return
	}
	pid, eid, err := s.envID(ctx, project, mode)
	if err != nil {
		return "", "", newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", "", unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `INSERT INTO webhook_endpoints(project_id, environment_id, url, secret_enc, events) VALUES ($1,$2,$3,''::bytea,$4) RETURNING id::text`,
		pid, eid, rawURL, events).Scan(&id); err != nil {
		return "", "", unavailable("webhook_create", err)
	}
	secret = newWebhookSecret()
	enc, err := s.Box.Seal([]byte(secret), []byte("webhook:"+id))
	if err != nil {
		return "", "", unavailable("seal", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE webhook_endpoints SET secret_enc=$2 WHERE id=$1`, id, enc); err != nil {
		return "", "", unavailable("webhook_secret", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", unavailable("commit", err)
	}
	return id, secret, nil
}

func (s *Service) RotateWebhookSecret(ctx context.Context, id string) (string, error) {
	if !uuidRe.MatchString(id) {
		return "", newErr(404, "not_found")
	}
	secret := newWebhookSecret()
	enc, err := s.Box.Seal([]byte(secret), []byte("webhook:"+id))
	if err != nil {
		return "", unavailable("seal", err)
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE webhook_endpoints SET secret_enc=$2 WHERE id=$1`, id, enc)
	if err != nil {
		return "", unavailable("webhook_rotate", err)
	}
	if tag.RowsAffected() == 0 {
		return "", newErr(404, "not_found")
	}
	return secret, nil
}

// UpdateWebhook changes URL, subscriptions or enabled state. Re-enabling
// clears the failure counter so an auto-disabled endpoint gets a fresh start.
func (s *Service) UpdateWebhook(ctx context.Context, id string, rawURL *string, events []string, enabled *bool) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	if rawURL != nil {
		if err := CheckWebhookURL(*rawURL); err != nil {
			return err
		}
	}
	if events != nil {
		if err := validEvents(events); err != nil {
			return err
		}
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE webhook_endpoints SET url=COALESCE($2,url), events=COALESCE($3,events),
  enabled=COALESCE($4,enabled),
  consecutive_failures=CASE WHEN $4 IS TRUE THEN 0 ELSE consecutive_failures END,
  disabled_reason=CASE WHEN $4 IS TRUE THEN '' WHEN $4 IS FALSE THEN 'disabled_by_operator' ELSE disabled_reason END
WHERE id=$1`, id, rawURL, events, enabled)
	if err != nil {
		return unavailable("webhook_update", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) DeleteWebhook(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id=$1`, id)
	if err != nil {
		return unavailable("webhook_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) ListWebhooks(ctx context.Context, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `
SELECT w.id::text AS id, p.name AS project, e.mode, w.url, w.events, w.enabled, w.consecutive_failures, w.disabled_reason, w.created_at,
  (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id=w.id AND d.state IN ('pending','sending')) AS backlog
FROM webhook_endpoints w JOIN projects p ON p.id=w.project_id JOIN environments e ON e.id=w.environment_id
ORDER BY w.created_at DESC`)
}

func (s *Service) ListDeliveries(ctx context.Context, endpointID, state string, pg Page) ([]map[string]any, bool, error) {
	if endpointID != "" && !uuidRe.MatchString(endpointID) {
		return []map[string]any{}, false, nil
	}
	return s.page(ctx, pg, `
SELECT d.id::text AS id, d.endpoint_id::text AS endpoint_id, d.event_type, d.payload, d.state, d.attempts, d.next_attempt_at,
       d.last_response_code, d.last_error, d.created_at, d.completed_at
FROM webhook_deliveries d
WHERE ($1='' OR d.endpoint_id=NULLIF($1,'')::uuid) AND ($2='' OR d.state=$2)
ORDER BY d.created_at DESC`, endpointID, state)
}

// ReplayDelivery queues a finished delivery again with the same event id, so
// receivers that already processed it can recognise the duplicate.
func (s *Service) ReplayDelivery(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE webhook_deliveries SET state='pending', attempts=0, next_attempt_at=now(), completed_at=NULL,
lease_token=NULL, lease_expires_at=NULL WHERE id=$1 AND state IN ('succeeded','dead')`, id)
	if err != nil {
		return unavailable("replay", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(409, "delivery_not_finished")
	}
	return nil
}

// Delivery is one leased webhook attempt.
type Delivery struct {
	ID, Token, URL, Secret, EventType string
	Attempts                          int
	Data                              json.RawMessage
	CreatedAt                         time.Time
}

// Body is the exact JSON document that is signed and posted.
func (d Delivery) Body() []byte {
	b, _ := json.Marshal(struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		CreatedAt string          `json:"created_at"`
		Data      json.RawMessage `json:"data"`
	}{d.ID, d.EventType, d.CreatedAt.UTC().Format(time.RFC3339), d.Data})
	return b
}

// ClaimDeliveries leases due deliveries of enabled endpoints. A lease that
// expired (worker died mid-request) is claimed again: webhooks are
// at-least-once and receivers deduplicate on the event id.
func (s *Service) ClaimDeliveries(ctx context.Context, limit int, lease time.Duration) ([]Delivery, error) {
	rows, err := s.Pool.Query(ctx, `
WITH c AS (
  SELECT d.id FROM webhook_deliveries d JOIN webhook_endpoints w ON w.id=d.endpoint_id
  WHERE w.enabled AND ((d.state='pending' AND d.next_attempt_at <= now()) OR (d.state='sending' AND d.lease_expires_at < now()))
  ORDER BY d.next_attempt_at LIMIT $1 FOR UPDATE OF d SKIP LOCKED)
UPDATE webhook_deliveries d SET state='sending', attempts=d.attempts+1, lease_token=gen_random_uuid(),
       lease_expires_at=now() + make_interval(secs => $2::float8)
FROM c, webhook_endpoints w WHERE d.id=c.id AND w.id=d.endpoint_id
RETURNING d.id::text, d.lease_token::text, w.url, w.secret_enc, w.id::text, d.event_type, d.attempts, d.payload, d.created_at`, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var enc []byte
		var endpoint string
		if err := rows.Scan(&d.ID, &d.Token, &d.URL, &enc, &endpoint, &d.EventType, &d.Attempts, &d.Data, &d.CreatedAt); err != nil {
			return nil, err
		}
		sec, err := s.Box.Open(enc, []byte("webhook:"+endpoint))
		if err != nil {
			// Unusable secret: leave the lease to expire rather than send unsigned.
			continue
		}
		d.Secret = string(sec)
		out = append(out, d)
	}
	return out, rows.Err()
}

// FinishDelivery records the outcome of an attempt. Failures are retried on
// the backoff schedule and then marked dead. An endpoint that keeps failing is
// disabled automatically and an alert is raised.
func (s *Service) FinishDelivery(ctx context.Context, d Delivery, ok bool, code int, errCode string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var respCode *int
	if code > 0 {
		respCode = &code
	}
	var endpoint string
	state := "succeeded"
	var retry *float64
	if !ok {
		state = "dead"
		if d.Attempts <= len(webhookBackoff) {
			state = "pending"
			secs := webhookBackoff[d.Attempts-1].Seconds()
			retry = &secs
		}
	}
	err = tx.QueryRow(ctx, `UPDATE webhook_deliveries SET state=$3, last_response_code=$4, last_error=$5,
  completed_at=CASE WHEN $3 IN ('succeeded','dead') THEN now() ELSE NULL END,
  next_attempt_at=CASE WHEN $6::float8 IS NULL THEN next_attempt_at ELSE now() + make_interval(secs => $6::float8) END,
  lease_expires_at=NULL
WHERE id=$1 AND lease_token=$2::uuid AND state='sending' RETURNING endpoint_id::text`, d.ID, d.Token, state, respCode, errCode, retry).Scan(&endpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // lease lost; another worker owns it
	}
	if err != nil {
		return err
	}
	if ok {
		_, err = tx.Exec(ctx, `UPDATE webhook_endpoints SET consecutive_failures=0 WHERE id=$1 AND consecutive_failures<>0`, endpoint)
	} else {
		limit := s.settingInt(ctx, "webhook_auto_disable_after")
		var disabled bool
		var u string
		err = tx.QueryRow(ctx, `UPDATE webhook_endpoints SET consecutive_failures=consecutive_failures+1,
  enabled = enabled AND consecutive_failures+1 < $2,
  disabled_reason = CASE WHEN consecutive_failures+1 >= $2 THEN 'auto_disabled_consecutive_failures' ELSE disabled_reason END
WHERE id=$1 RETURNING NOT enabled AND consecutive_failures = $2, url`, endpoint, limit).Scan(&disabled, &u)
		if err == nil && disabled {
			err = raiseAlert(ctx, tx, "webhook_endpoint_disabled", "Webhook endpoint disabled after repeated failures",
				map[string]any{"endpoint_id": endpoint, "url": u, "consecutive_failures": limit})
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
