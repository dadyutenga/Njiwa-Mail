# Documentation version policy

- The documentation version follows the **API version**: `/v1` is documentation version `1.0`. A breaking change to `/v1` becomes `/v2` and a new documentation version; the old one stays published at its own path.
- **Not breaking** (no new version): an optional request field, a new response field, a new error code, a new endpoint, a new webhook event type or `data` field, a new state that clients must already treat as unknown. Clients must ignore what they do not know.
- **Breaking**: removing or renaming a field, endpoint, state or event; changing a field's type or meaning; making an optional field required; changing what an existing code means.
- Webhook payloads carry their own `schema_version`; the operator pins endpoints per version (see the Webhooks page).
- Minor documentation versions (`1.1`, …) are published when the contract grows compatibly. They replace `1.0` as "latest"; the version string, the source commit and the OpenAPI file are stamped into every generated file.
- A published version is immutable except for fixing errors; a correction is a new commit and the same version.
- To add an archived version: add its content tree and an entry to `documentation/lib/versions.ts` (and `scripts/generate-assets.mjs`), keep `docs/openapi.yaml` for each version under its own name, and extend the export check. The selector then switches between them keeping the page when it exists in both.
- Today exactly one version is published (`1.0`).

## Release steps

1. Merge with the full CI (`docs`, `docs-image`, Go docs tests) green.
2. Run `RELEASE_CHECKLIST.md`.
3. Build the image with `DOCS_COMMIT` set to the release commit and deploy it ([DEPLOY.md](DEPLOY.md)).
4. Keep the previous image tag for rollback.
