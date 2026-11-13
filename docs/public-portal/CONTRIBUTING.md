# Contributing to the public documentation

The portal lives in `documentation/`. Pages are MDX in `documentation/content/docs/`. The API contract is `docs/openapi.yaml`. Everything agents fetch (`llms.txt`, `.md` pages, OpenAPI, the skill) is generated from those two sources, so there is nothing else to edit.

## Run it (no installs on your machine needed)

Use a throwaway container, or local Node 24:

```bash
cd documentation
npm ci --ignore-scripts
node node_modules/fumadocs-mdx/bin.js      # generates .source
npm run typecheck && npm run lint && npm test
npm run build                               # also generates llms.txt, .md pages, openapi.*
npm run check:snippets && npm run check:export
npx playwright install chromium && npx playwright test
```

The Go tests that tie the docs to the code run with the normal suite (`go test ./test -run 'TestPublicOpenAPI|TestDocs|TestDocumented'`, needs the test PostgreSQL).

## Adding or changing a page

1. Create `content/docs/<section>/<name>.mdx` with frontmatter `title`, `description` and optionally `status: planned`.
2. Add it to the section's `meta.json` (order = sidebar order).
3. Link it from where a reader would look for it. `npm run lint` fails on a broken internal link or heading anchor.
4. Run the checks above. Fix what they report; do not weaken a check.

## Rules the checks enforce

**MDX is code.** A pull request can contain MDX written by anyone, so content may not contain: `import`/`export`, curly-brace expressions (use `` `code` `` or a fence), raw HTML elements, event handlers, `javascript:`/`data:` links, `http://` links, or components other than `Tabs`, `Tab`, `Steps`, `Step`, `Callout`, `Cards`, `Card`. CI builds untrusted content with a read-only token and no secrets, and never deploys it.

**Claims must be true.**
- Never say or imply delivery. `submitted` means the mail server accepted the message. Say "sent" or "submitted", never "delivered" (except to say it is not).
- Mailcow is optional. Never require it; put it in the appendix.
- Mark anything not implemented as **PLANNED** (set `status: planned` on a whole page, or label the table row). Do not describe planned behaviour in the present tense.
- No real hosts: use `https://mail-api.example.com`, `notify.example.com`, `user@example.org`. No Ditronics names, no real or hosted endpoint.
- No secrets: use `dmk_sbx_...`-style placeholders. The scanner rejects anything shaped like a key, webhook secret or private key.

**Examples are executed.**
- A `bash curl-check` fence is replayed against the real handlers by `test/docs_content_test.go` and validated against the OpenAPI schema. It must target `"$NJIWA_MAIL_URL/…"`, send `Authorization: Bearer $NJIWA_MAIL_API_KEY`, and (for `POST /v1/emails`) `Idempotency-Key: $(uuidgen)`.
- A `ts check`, `go check` or `python check` fence is compiled (TypeScript against `clients/typescript/index.ts`, Go against the real client with `go vet`, Python with `py_compile`).
- Tables of webhook events, scopes, message states and error codes are compared with the source. If you add one to the code, the test names the page that must change.
- Only endpoints in `docs/openapi.yaml` may be named with `METHOD /path`.

**Idempotency, errors, honesty.** Every send example uses a new idempotency key per logical event. Do not show a browser calling the API with a key.

## Style

Plain, direct, present tense. Say what the reader does, then why. One idea per paragraph. Short tables for facts, prose for reasons. Prefer "your instance" to any hosted-service language: there is no central endpoint. Keep Kiswahili terms of art (DNS, SPF, DKIM, webhook) in English if you translate. Write link text that makes sense out of context. Do not repeat the API reference in guides: link to it.

## Changing the API contract

Change the handler, `docs/openapi.yaml` and the page together. The drift tests fail until all three agree, which is the point.

## Review checklist

- [ ] `npm run lint`, `npm test`, `npm run build`, `check:snippets`, `check:export` pass
- [ ] Go docs tests pass
- [ ] No new claim without evidence; planned things are labelled
- [ ] Desktop and mobile screenshots attached for UI changes (`SHOT_DIR=... npx playwright test`)
