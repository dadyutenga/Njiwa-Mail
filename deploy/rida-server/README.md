# Automatic deploys to rida-server

Push to `dev` and it reaches production by itself: CI runs on the push and, when
it passes, `.github/workflows/promote.yml` fast-forwards `main` to that exact
commit and runs `.github/workflows/deploy.yml` for it. `main` only moves
forward: if it has commits `dev` lacks, the promote fails and nothing deploys
(merge `main` into `dev` and push again).

A push straight to `main` still works the old way (CI on `main`, then Deploy).
A deploy can also be started by hand from GitHub → Actions → Deploy → Run workflow.

## How it works

1. The workflow connects over SSH with a key that can run only
   `/usr/local/sbin/njiwa-deploy` (a forced command), and passes the commit SHA.
2. `njiwa-deploy` (this folder's `njiwa-deploy.sh`):
   - fetches the commit into `/opt/njiwa-mail-src` and refuses it if it is not on `main`;
   - copies the tracked files into `/opt/njiwa-mail` (the server-only `.env`,
     `.env.docs`, `secrets/` and data are untracked and never touched);
   - rebuilds only what changed: API/worker/scheduler (Go), dashboard, docs;
   - dumps the database first when migrations changed
     (`/opt/njiwa-mail-backups/db-before-<sha>.sql.gz`);
   - waits for health checks and, for docs, the public smoke test;
   - on failure, puts the previous images back (`njiwa-mail:rollback`,
     `njiwa-mail-frontend:rollback`) and fails the workflow;
   - records the deployed commit in `/opt/njiwa-mail/.deployed-commit`;
   - refreshes the host agent (`njiwa-mail-host-agent`, run from
     `/opt/njiwa-mail/bin/njiwa-mail`) from the new image and installs its unit
     from `njiwa-mail-host-agent.service`, restarting it when either changed.

## Mail queue and delivery results (Mailcow)

Postfix on this server is Mailcow's, inside `mailcowdockerized-postfix-mailcow-1`.
The host agent runs `postqueue`/`postsuper` there with `docker exec`
(`-postfix-container`) and reads its delivery lines with `docker logs`. The API
asks the agent every 30 s for new `status=sent|deferred|bounced` lines and stores
them as delivery evidence, so a message shows "the recipient's mail server
accepted it" (or deferred / bounced) instead of only "submitted". Check:
`curl -s --unix-socket /run/njiwa-mail/mta-agent.sock -H "Authorization: Bearer $(cat /opt/njiwa-mail/secrets/mta-agent.token)" "http://agent/log?since=$(date -d -1hour +%s)"`.
3. One deploy runs at a time (GitHub concurrency group and a server lock).

Build logs: `/opt/njiwa-mail-backups/deploy-<sha>.log`.

## Setup (done once)

```sh
# on the server
install -m 755 deploy/rida-server/njiwa-deploy.sh /usr/local/sbin/njiwa-deploy
ssh-keygen -t ed25519 -N '' -C njiwa-mail-cd -f /root/.ssh/github-deploy
echo "command=\"/usr/local/sbin/njiwa-deploy\",restrict $(cat /root/.ssh/github-deploy.pub)" >> /root/.ssh/authorized_keys
```

GitHub secrets in the `production` environment: `DEPLOY_SSH_KEY` (the private
key, then delete it from the server), `DEPLOY_HOST`, `DEPLOY_KNOWN_HOSTS`
(`ssh-keyscan -t ed25519 <host>`).

When `deploy/rida-server/njiwa-deploy.sh` changes, reinstall it on the server
by hand: the pipeline never replaces its own entrypoint.

## Turning it off or rolling back

- Pause: GitHub → Actions → Deploy → Disable workflow (or disable only
  "Promote dev" to keep `dev` pushes off production). Revoke: delete the
  `njiwa-mail-cd` line from `/root/.ssh/authorized_keys`.
- Roll back a release: `ssh rida-server /usr/local/sbin/njiwa-deploy <older-main-sha>`
  (forward-only migrations stay applied; restore the dump if one is needed).

## SSH port

The workflow connects on the `production` environment variable `DEPLOY_PORT` (22 when unset); the server's
SSH listens on 6969 (`/etc/ssh/sshd_config.d/10-port.conf`). After moving SSH again, update it:
`gh variable set DEPLOY_PORT --env production --body <port>`. The pinned host key is checked by host name
(`HostKeyAlias`), so a port change alone does not need a new `DEPLOY_KNOWN_HOSTS`.
