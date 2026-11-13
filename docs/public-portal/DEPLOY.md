# Building and deploying the documentation site

The site is a **static export** served by unprivileged nginx. There is no server runtime, no database, no secret at build or run time, and it never proxies to the application. It is independent of the application stack.

## Build and run

```bash
docker compose -f compose.docs.yaml build \
  --build-arg DOCS_BASE_URL=https://docs.example.com \
  --build-arg DOCS_COMMIT=$(git rev-parse --short HEAD)
DOCS_COMMIT=$(git rev-parse --short HEAD) docker compose -f compose.docs.yaml up -d      # 127.0.0.1:8088
sh documentation/scripts/smoke.sh http://127.0.0.1:8088 $(git rev-parse --short HEAD)
```

`DOCS_BASE_URL` is the public origin (used in `llms.txt` and prompts). There is no default hosted site: `https://docs.example.com` is a placeholder you must replace. The Docker build itself runs the content lint, the static export and the export-parity check, so a broken or contradictory page cannot produce an image.

Container properties: runs as an unprivileged user (uid 101), read-only root filesystem, all capabilities dropped, `no-new-privileges`, only port 8080, no environment secrets.

## HTTPS and the reverse proxy

Put it behind HTTPS with `deploy/nginx-docs.conf` on its **own host name**. Do not share a server block, host name or upstream with the application API (`deploy/nginx.conf`). The container answers 404 for `/admin`, `/v1`, `/console`, `/metrics`, `/health` and dotfiles, so a wrong proxy rule still cannot reach an API.

## Content-Security-Policy

The container sends a strict CSP (`default-src 'self'`, no objects, no framing, no external connections). Scripts and styles allow `'unsafe-inline'` because the Next.js static export embeds hydration data inline. That is acceptable here only because content is linted so MDX cannot carry script (see `CONTRIBUTING.md`). "Open in …" links are plain navigations in a new tab with `noopener`, not fetches, so they need no `connect-src`.

## Upgrade and rollback

Images are tagged with the source commit. To deploy a build, start its tag; to roll back, start the previous tag:

```bash
docker rm -f docs; docker run -d --name docs -p 127.0.0.1:8088:8080 --read-only --tmpfs /tmp --cap-drop ALL \
  --security-opt no-new-privileges:true njiwa-mail-docs:<tag>
sh documentation/scripts/smoke.sh http://127.0.0.1:8088 <tag>
```

Pages are served with `Cache-Control: no-cache`, so a deploy or rollback is visible at once; only content-addressed `/_next/static/` files are cached long-term.

**Demonstrated 2026-11-13** on `rida-server` with throwaway containers on a loopback port (not a production deployment): image `aaaa111` deployed and passed the smoke (41 checks), image `bbbb222` deployed over it and passed, rollback to `aaaa111` passed, and the smoke correctly failed when asked for the wrong commit. The demonstration also found and fixed a typo in `nginx.conf` that made the container exit, which is what the smoke test is for.

## CI

- `docs` (every push and pull request): typecheck, lint, content lint, unit tests, build, snippet checks, export parity, Playwright. Read-only token, no secrets, nothing deployed. Untrusted MDX is only ever built and checked here.
- `docs-image`: builds the image, starts it hardened, runs the smoke test against the commit it was built from.
- `Docs image` workflow: **manual only** (`workflow_dispatch`); builds, smoke-tests and stores the image as an artifact. It does not publish anywhere. It cannot run for pull requests.
- The Go docs tests (`TestPublicOpenAPI*`, `TestDocs*`, `TestDocumented*`) run in the normal `test` job.
