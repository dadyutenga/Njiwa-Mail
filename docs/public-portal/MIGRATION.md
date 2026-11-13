# Migration notes

## From the earlier documentation

| Before | Now |
|---|---|
| `docs/openapi.yaml`: 3 operations, `template`/`template_version`/`variables` required, `additionalProperties: false`, no `scheduled`/`cancelled`, "32 KiB max" | 14 operations; `from` and `to` required; template or raw send; optional `template_version`; `locale`, `send_at`, `cc`, `bcc`, `priority`, `attachments`, `headers`; ten states; 6 MiB on `POST /v1/emails`; batch items carry `idempotency_key` |
| `docs/INTEGRATION.md` said live keys submit to Mailcow | Live keys submit through the active delivery profile (managed Postfix or existing SMTP); Mailcow is optional |
| TypeScript client `status()` typed as the accepted response | `status()` returns `MessageStatus` |
| The dashboard welcome page linked to a hard-coded repository | Links come from `VITE_GITHUB_URL` and `VITE_DOCS_URL`; the docs link is hidden until you publish a site |

`docs/INTEGRATION.md`, `docs/WEBHOOKS.md` and `docs/MAILCOW_SETUP.md` remain as repository references. The public site is the canonical, versioned source for developers: link to it rather than copying text.

## For people who generated code from the old OpenAPI

Regenerate. Fields that were required may now be omitted (`template_version`, `variables` for raw sends), and enums grew (`status`). Treat unknown enum values as unknown, never as errors.

## Private dashboard vs public portal

The dashboard's **API docs** page (`ApiDocs`) is private, per project and environment, and shows that project's real host and default sender. The public portal is generic, uses placeholders, and holds no project data. The dashboard links to the portal when `VITE_DOCS_URL` is set. Do not copy portal text into the dashboard.
