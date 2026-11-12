# Examples

All examples use the application API ([openapi.yaml](openapi.yaml)); the key is a **server-side secret**. They show the three things an
app does: send with an idempotency key, read the status, and verify a webhook. `NJIWA_MAIL_URL` and `NJIWA_MAIL_API_KEY` are described in
[INTEGRATION.md](INTEGRATION.md). The webhook envelope, headers and retries are in [WEBHOOKS.md](WEBHOOKS.md).

A `202` means the mail is **durably queued**. `submitted` later means the mail server accepted it; it is never a claim of inbox delivery.
Use one `Idempotency-Key` per logical event and reuse it on every retry (a repeat returns the same message id; a different body is `409`).
Retry only `429` and `5xx` (honour `Retry-After`).

## curl

```bash
curl -sS "$NJIWA_MAIL_URL/v1/emails" \
  -H "Authorization: Bearer $NJIWA_MAIL_API_KEY" \
  -H "Idempotency-Key: signup-8841-otp" \
  -H "Content-Type: application/json" \
  -d '{"from":"noreply@notify.example.com","to":"user@example.org","template":"otp","locale":"sw",
       "variables":{"name":"Ada","code":"482913","expires_in_minutes":5}}'
# {"id":"6f1c2d0e-...","status":"queued"}

curl -sS "$NJIWA_MAIL_URL/v1/emails/6f1c2d0e-..." -H "Authorization: Bearer $NJIWA_MAIL_API_KEY"
```

## Go (this repository's `client` package, no other dependencies)

```go
c := &client.Client{BaseURL: os.Getenv("NJIWA_MAIL_URL"), APIKey: os.Getenv("NJIWA_MAIL_API_KEY")}
acc, err := c.Send(ctx, "signup-8841-otp", client.Email{
    From: "noreply@notify.example.com", To: "user@example.org", Template: "otp", Locale: "sw",
    Variables: map[string]any{"name": "Ada", "code": "482913", "expires_in_minutes": 5},
})
var apiErr *client.APIError
if errors.As(err, &apiErr) && apiErr.Retryable() {
    // 429 or 5xx: try again later with the SAME idempotency key
}
```

Verify a webhook (raw body first, then parse):

```go
body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
ev, err := client.VerifyWebhook(os.Getenv("DM_WEBHOOK_SECRET"), r.Header.Get("X-DM-Signature"), body, time.Now())
if err != nil { http.Error(w, "bad signature", 401); return }
// ev.ID (deduplicate on it), ev.Type, ev.SchemaVersion, ev.Data
```

## Node.js (18+)

```js
const res = await fetch(`${process.env.NJIWA_MAIL_URL}/v1/emails`, {
  method: "POST",
  headers: {
    Authorization: `Bearer ${process.env.NJIWA_MAIL_API_KEY}`,
    "Idempotency-Key": "signup-8841-otp",
    "Content-Type": "application/json",
  },
  body: JSON.stringify({ from: "noreply@notify.example.com", to: "user@example.org", template: "otp",
    variables: { name: "Ada", code: "482913", expires_in_minutes: 5 } }),
});
if (res.status === 429 || res.status >= 500) { /* retry later, same Idempotency-Key, honour Retry-After */ }
const { id, status } = await res.json();
```

```js
import crypto from "node:crypto";
// Express: use express.raw({ type: "application/json" }) so req.body is the untouched Buffer.
function verifyWebhook(secret, header, rawBody) {
  const parts = Object.fromEntries(header.split(",").map((p) => p.trim().split("=")));
  const expected = crypto.createHmac("sha256", secret).update(`${parts.t}.${rawBody}`).digest("hex");
  const ok = parts.v1 && crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(parts.v1));
  const fresh = Math.abs(Date.now() / 1000 - Number(parts.t)) <= 300; // five minutes
  return ok && fresh;
}
```

## Python (requests)

```python
import os, requests
r = requests.post(
    f"{os.environ['NJIWA_MAIL_URL']}/v1/emails",
    headers={"Authorization": f"Bearer {os.environ['NJIWA_MAIL_API_KEY']}", "Idempotency-Key": "signup-8841-otp"},
    json={"from": "noreply@notify.example.com", "to": "user@example.org", "template": "otp",
          "variables": {"name": "Ada", "code": "482913", "expires_in_minutes": 5}},
    timeout=10,
)
if r.status_code in (429,) or r.status_code >= 500:
    pass  # retry later with the same Idempotency-Key; honour r.headers.get("Retry-After")
r.raise_for_status()
message_id = r.json()["id"]
```

```python
import hashlib, hmac, time
def verify_webhook(secret: str, header: str, raw_body: bytes) -> bool:
    parts = dict(p.strip().split("=", 1) for p in header.split(","))
    signed = parts["t"].encode() + b"." + raw_body
    expected = hmac.new(secret.encode(), signed, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, parts.get("v1", "")) and abs(time.time() - int(parts["t"])) <= 300
```

Deduplicate on the event `id` (delivery is at least once), answer `2xx` within 10 seconds, and ignore fields you do not know:
`schema_version` only changes when a field is removed or changed.
