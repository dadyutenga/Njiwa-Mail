# Frontend ↔ backend integration

The operator UI uses the Go admin API at `/admin/v1`. The browser sends an HttpOnly session cookie. The login and session endpoints return a CSRF token; the frontend keeps it in memory and sends it as `X-CSRF-Token` on writes. No API key or session token is stored in browser storage.

## Run locally

1. Start PostgreSQL and the backend, configure `DATABASE_URL`, `ENCRYPTION_KEY`, `HMAC_KEY`, and run migrations.
2. For local HTTP, set `ADMIN_INSECURE_COOKIES=true` in the backend environment.
3. From `frontend`, run `npm ci && npm run dev`. Vite proxies `/admin` to `http://127.0.0.1:8080`.
4. Open the local Vite URL and sign in with an operator account created by the backend CLI.

`docker compose up -d --build` also starts the frontend at `http://127.0.0.1:3000`; its Nginx container proxies `/admin/v1` to the `api` container. Use HTTPS and secure cookies for a deployed site.

## Roles

The backend defines two operator roles. `admin` can use 27 operator routes. `owner` has the same access plus `/users`, including user creation and enable/disable. The sidebar and route guard reflect these permissions, while the backend enforces them on every request. There is no public registration route or MFA flow.

## Screen mapping

| Screen | Backend endpoint |
|---|---|
| Overview | `GET /admin/v1/overview` |
| Messages and detail | `GET /admin/v1/messages`, `GET /admin/v1/messages/{id}` |
| Projects, environments, keys, senders | `/admin/v1/projects`, `/keys`, `/senders` |
| Templates, versions, preview, publishing, sandbox test send | `/templates` and `/templates/{id}/versions` |
| Inbound mail and addresses | `/inbound`, `/inbound-addresses` |
| Webhooks, deliveries, suppressions | `/webhooks`, `/webhook-deliveries`, `/suppressions` |
| Bounces, alerts, alert channels, domains, DNS | `/bounces`, `/alerts`, `/alert-channels`, `/domains`, `/dns` |
| Sandbox, audit | `/sandbox`, `/audit` |
| Analytics | `GET /admin/v1/analytics` |
| Settings | `GET` and `PUT /admin/v1/settings` |
| Profile and sessions | `/auth/me`, `/auth/sessions` |
| Team users (owner) | `/admin/v1/users` |
| Recipient privacy | `/admin/v1/privacy/export`, `/privacy/delete` |

The listed resource screens provide live lists, creation where supported, and common row actions. Message resolution, webhook replay, privacy export/deletion, and template versioning have dedicated UI actions.
