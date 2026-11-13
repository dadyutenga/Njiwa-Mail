# Contributing to Njiwa Mail

Karibu, and thank you for helping. This guide gets you from a fresh clone to a pull request that CI accepts.

Ways to help, from smallest to largest:

- Report a bug or a confusing message ([open an issue](https://github.com/dadyutenga/Njiwa-Mail/issues/new/choose)).
- Fix a typo, a doc page, or the English or Kiswahili wording.
- Run the installer or `scripts/discover.sh` on a distribution you use, and report the result.
- Add a test for behaviour that is not covered yet.
- Implement an open item from [docs/REMAINING_WORK.md](docs/REMAINING_WORK.md) or [task.md](task.md). For anything larger than a
  small fix, open an issue first so we can agree on the approach before you spend time on it.

Security problems go to [SECURITY.md](SECURITY.md), never to a public issue.

## Development setup

You need Go 1.26, Node.js 24, PostgreSQL 16 and Docker (with Compose) for the full stack.

```bash
git clone https://github.com/dadyutenga/Njiwa-Mail.git && cd Njiwa-Mail
git switch dev
```

Run the stack locally in sandbox mode (no mail leaves your machine) with the [Quick start](README.md#quick-start-local-sandbox).

### Backend (Go)

The test suite resets the `public` schema of the database it connects to, so **always use a throwaway database**:

```bash
createdb -T template0 njiwa_mail_test
export TEST_DATABASE_URL='postgres://USER@/njiwa_mail_test?host=/var/run/postgresql'
go vet ./... && go test ./... -race -count=1
```

### Dashboard (React, `frontend/`)

```bash
cd frontend && npm ci
npm run typecheck && npm run lint && npm test && npm run build
```

### Documentation portal (`documentation/`)

See [docs/public-portal/CONTRIBUTING.md](docs/public-portal/CONTRIBUTING.md).

### Managed installer

The installer changes a real host (Postfix, OpenDKIM, firewall). **Never test it on a server you care about.** Use a throwaway VM
or container. The unit tests run against a temporary directory and a fake command runner, so they are safe anywhere:

```bash
go test ./internal/installer/...
```

The end-to-end checks against a real Postfix and OpenDKIM run in CI on Ubuntu, AlmaLinux, Rocky Linux and Fedora (see the
`managed-mta` and `managed-mta-rhel` jobs in [.github/workflows/ci.yml](.github/workflows/ci.yml)).

## Branches and pull requests

- `dev` is where work happens. **Open pull requests against `dev`.** `main` only receives releases that were tested together.
- Keep one topic per pull request, and one logical change per commit, with a message that says why.
- Fill in the pull request template: what changed, why, and how you tested it.
- CI must be green. It runs the Go tests with the race detector, the dashboard and docs checks, `govulncheck`, a supply-chain
  check, the installer end-to-end jobs and a Docker smoke test.

## Project rules

These keep Njiwa Mail honest and safe. Reviews check them, and several are enforced by tests:

- **Be honest about delivery.** Never report a message as "delivered" because an SMTP server accepted it. Readiness checks report
  what they could not verify as `CANNOT_CONFIRM`, never as passing.
- **Never touch a mail server that is in use.** The installer must refuse, and change nothing, when it finds one.
- **Every new route goes into the security matrix test**, and `docs/ENDPOINTS.md` is regenerated:
  `UPDATE_DOCS=1 go test ./test -run TestEndpointsAndSchemaDoc`.
- **Every new dashboard string has a Kiswahili translation.** A coverage test fails otherwise. If you do not speak Kiswahili,
  say so in the pull request and a maintainer will help.
- **Database migrations are additive.** Add new ones; never edit or remove a migration that has been released.
- **No new dependency without a reason.** Say why in the pull request; the supply-chain check reviews it.
- **Don't commit secrets, real customer addresses or personal data**, including in tests, fixtures and screenshots. Use
  `example.com`, `example.org` and `example.test`.

## License of contributions

Njiwa Mail is licensed under the [GNU AGPL-3.0](LICENSE). By submitting a contribution you agree that it is licensed under the
same terms, and you confirm you have the right to submit it.

## Code of Conduct

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).
