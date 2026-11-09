package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// WebhookTolerance is the maximum accepted age of a signed webhook.
const WebhookTolerance = 5 * time.Minute

// WebhookEvent is the body of every webhook. ID is unique per event and stays
// the same across retries and replays: store it and ignore ids already seen.
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

var ErrWebhookSignature = errors.New("ditronics-mail: invalid webhook signature")

// VerifyWebhook checks the X-DM-Signature header ("t=<unix>,v1=<hex>") against
// the RAW request body, exactly as received, and rejects timestamps further
// than WebhookTolerance from now. Verify before parsing the JSON.
func VerifyWebhook(secret, signatureHeader string, body []byte, now time.Time) (*WebhookEvent, error) {
	var ts, sig string
	for _, part := range strings.Split(signatureHeader, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return nil, ErrWebhookSignature
	}
	if d := now.Sub(time.Unix(unix, 0)); d > WebhookTolerance || d < -WebhookTolerance {
		return nil, ErrWebhookSignature
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	want, err := hex.DecodeString(sig)
	if err != nil || !hmac.Equal(m.Sum(nil), want) {
		return nil, ErrWebhookSignature
	}
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}
