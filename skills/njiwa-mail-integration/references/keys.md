# Playbook: API keys

Keys are created by the operator, not through the public API (there is no key endpoint in `/v1`). Tell the user to run, on the instance:

- `njiwa-mail key create -project P -mode sandbox|live -scopes emails:send,emails:read -label L` (the secret is shown once)
- `njiwa-mail key rotate -prefix P -grace 24h` (the old key works for the grace period)
- `njiwa-mail key revoke -prefix P`

Scopes: `emails:send`, `emails:send_raw` (own subject/body), `emails:read`, `inbound:read`, `suppressions:manage`, `addresses:verify` (any sending key may also verify), `contacts:manage`. Give the least a service needs. A key belongs to one project and one environment; a sandbox key cannot touch live data. Optional expiry (`-ttl`) and IP allowlist. A leaked key is revoked and replaced, never "hidden". Never put keys in client code, logs or repositories.
