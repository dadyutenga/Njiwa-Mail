# Webhooks

Njiwa Mail posts a signed JSON event to your endpoint when something happens to a message you sent, or when mail arrives for one of your inbound addresses.

## Events

| Type | When | `data` |
|---|---|---|
| `email.submitted` | Your mail server (managed Postfix or your existing SMTP server) acknowledged the message. This is **not** proof of delivery. | `message_id`, `state`, `reason`, `template`, `request_id` |
| `email.failed` | Permanent failure before acceptance, or retries exhausted | same |
| `email.unknown` | Acceptance could not be confirmed; an operator will reconcile | same |
| `email.expired` | TTL passed before submission started | same |
| `email.bounced` | A delivery status notification came back | `message_id`, `bounce_id`, `type` (`hard`, `soft`, `policy`, `unknown`), `status`, `suppressed` |
| `email.delivery_deferred` | The mail server log shows the recipient's server has not accepted it yet; the mail server keeps retrying | `message_id`, `event`, `status` (enhanced status code) |
| `email.delivery_remote_accepted` | The mail server log shows the recipient's mail server accepted it. This does **not** show inbox placement | same |
| `email.delivery_bounced` | The mail server log shows a permanent rejection | same |
| `email.delivery_expired_in_queue` | The mail server gave up retrying | same |
| `inbound.received` | Mail was parsed for one of your inbound addresses | `inbound_id`, `address_id`, `attachment_count`, `raw_size` |

There is no `email.delivered` event and there never will be: the service cannot prove delivery.

The `email.delivery_*` events come from the mail server log (`njiwa-mail mta-log`, see `docs/OPERATIONS.md`) and are only sent when
that collector runs; they are evidence, and a message's `state` never changes because of them. Each log line produces at most one
event: re-reading the log (the collector restarted) records nothing twice. You can subscribe to these events like any other
(`email.delivery_deferred`, `email.delivery_remote_accepted`, `email.delivery_bounced`, `email.delivery_expired_in_queue`).

Payloads contain ids, states and safe reason codes only. They never contain bodies, variables, tokens or recipient addresses. To read an inbound message, call `GET /v1/inbound/{inbound_id}` with a key that has the `inbound:read` scope.

## Request

```
POST <your url>
Content-Type: application/json
X-DM-Event-ID: 6f1c2d0e-...
X-DM-Event-Type: email.bounced
X-DM-Schema-Version: 1
X-DM-Signature: t=1794222000,v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8bd

{"id":"6f1c2d0e-...","type":"email.bounced","schema_version":1,"created_at":"2026-11-09T10:00:00Z","data":{"message_id":"...","bounce_id":"...","type":"hard","status":"5.1.1","suppressed":true}}
```

## Schema versions

Every event carries `schema_version` (an integer, also sent as `X-DM-Schema-Version`). Today it is `1`.

- **Adding** a field to `data` or to the envelope does not change the version: ignore fields you do not know.
- **Changing or removing** a field adds a new version. The previous version stays available for at least one release, and an endpoint
  keeps receiving the version it is pinned to (`PATCH /admin/v1/webhooks/{id}` with `{"schema_version": 1}`; an unsupported value is
  refused with `422 unsupported_schema_version`).
- Each delivery is stamped with the endpoint's version when the event is created, so a retry or a replay always has the shape first
  sent, even if the endpoint was pinned to another version in between. An event without `schema_version` (older senders) is version 1.

## Verify the signature

1. Read the **raw** request body, before any JSON parsing.
2. Split `X-DM-Signature` into `t` and `v1`.
3. Compute `HMAC-SHA256(signing_secret, t + "." + raw_body)` and hex-encode it.
4. Compare with `v1` in constant time.
5. Reject the request if `t` is more than **five minutes** from your clock.

Go (dependency-free, in this repository's `client` package):

```go
body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
ev, err := client.VerifyWebhook(os.Getenv("DM_WEBHOOK_SECRET"), r.Header.Get("X-DM-Signature"), body, time.Now())
if err != nil {
    http.Error(w, "bad signature", http.StatusUnauthorized)
    return
}
// ev.ID, ev.Type, ev.Data
```

The signing secret (`whsec_...`) is shown once when the endpoint is created or its secret is rotated. Keep it in server configuration.

## Respond

- Answer **2xx within 10 seconds**. Do slow work after you answer.
- Any other status, a timeout or a redirect counts as a failure. Redirects are never followed.
- At most 1 MiB of your response is read.

## Retries, duplicates and replay

- Delivery is **at least once**. Store `id` and ignore an id you have already processed. A retry and a manual replay both carry the same `id`.
- Failed deliveries are retried after about 30 s, 2 min, 10 min, 30 min, 1 h, 2 h, 4 h, 6 h and 10 h (roughly 24 hours), then marked `dead`.
- An operator can replay any finished delivery from the admin UI.
- After 20 consecutive failed attempts (configurable in Settings) the endpoint is disabled and an alert is raised. Events already queued for it wait and are sent when an operator enables it again. Events that happen **while it is disabled are not queued**; read the current state through the API after re-enabling.
- Events are not ordered. Use `GET /v1/emails/{id}` when you need the current state.

## Endpoint rules

HTTPS only, a public hostname, no credentials in the URL. Private, loopback, link-local and other reserved addresses are refused, both when the endpoint is saved and again on every connection (so a hostname that later resolves to a private address is refused too).
