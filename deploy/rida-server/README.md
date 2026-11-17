# Automatic deploys to rida-server

Every push to `main` runs CI. When CI passes, `.github/workflows/deploy.yml`
deploys that commit to production. It can also be started by hand from
GitHub → Actions → Deploy → Run workflow.

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
   - records the deployed commit in `/opt/njiwa-mail/.deployed-commit`.
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

- Pause: GitHub → Actions → Deploy → Disable workflow. Revoke: delete the
  `njiwa-mail-cd` line from `/root/.ssh/authorized_keys`.
- Roll back a release: `ssh rida-server /usr/local/sbin/njiwa-deploy <older-main-sha>`
  (forward-only migrations stay applied; restore the dump if one is needed).
