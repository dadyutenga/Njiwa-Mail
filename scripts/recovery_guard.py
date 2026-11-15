#!/usr/bin/env python3
"""Fail-closed recovery checks. No service commands or host writes occur here.

Python 3 standard library only. --root is for isolated fixture tests; the shell
entrypoints always use /. Managed archives are trusted operator input, not signed
backups: keep the backup and its manifest encrypted and access-controlled.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import sys
import tarfile
import subprocess

MARKER = b'# Managed by the Njiwa-Mail installer. Manual changes are replaced on the next apply.'
STATE = 'var/lib/njiwa-mail/installer'
CORE = ('etc/postfix/main.cf', 'etc/postfix/master.cf', 'etc/opendkim.conf')
FIXED = set(CORE) | {
    'etc/njiwa-mail/firewall.nft',
    'etc/systemd/system/njiwa-mail-firewall.service',
    'etc/systemd/system/postfix@.service.d/njiwa-mail.conf',
    'etc/systemd/system/postfix.service.d/njiwa-mail.conf',
    'etc/systemd/system/njiwa-mail-mta-agent.service',
    'etc/systemd/system/njiwa-mail-mta-log.service',
}


def safe_name(name):
    p = PurePosixPath(name)
    if p.is_absolute() or '..' in p.parts or not p.parts:
        raise ValueError(f'unsafe archive path: {name}')
    return str(p)


def path(root, name):
    name = safe_name(name)
    p = root / name
    # Never follow a symlink, including parent directories.
    for part in [p, *p.parents]:
        if part == root.parent:
            break
        if part.is_symlink():
            raise ValueError(f'symlink in recovery path: {part}')
    return p


def allowed(name):
    return name in FIXED or any(name.startswith(p) for p in (
        'etc/postfix/njiwa/', 'etc/opendkim/njiwa/',
        'etc/opendkim/keys/', 'etc/njiwa-mail/submission/',
        STATE + '/receipts/', STATE + '/backups/',
    ))


def receipt(root):
    receipts = path(root, STATE + '/receipts')
    files = sorted(receipts.glob('*.json'))
    if not files:
        return None
    rc = json.loads(path(root, str(files[-1].relative_to(root))).read_text())
    if rc.get('result') != 'ok' or rc.get('rollback_state') == 'rolled_back':
        raise ValueError('latest installer receipt is not a successful owned installation')
    if rc['params'].get('StateDir', '/' + STATE) != '/' + STATE:
        raise ValueError('custom installer state directory requires a reviewed recovery procedure')
    for name in CORE:
        if not path(root, name).read_bytes().startswith(MARKER + b'\n'):
            raise ValueError(f'foreign mail configuration: {name}')
    return rc


def backup_plan(root, out, include):
    rc = receipt(root) if include else None
    if include and rc is None:
        raise ValueError('BACKUP_MANAGED=1 requires a successful installer receipt and owned configuration')
    meta = {'version': 1, 'mode': 'application', 'paths': []}
    if rc:
        names = set()
        for f in rc['files']:
            name = f['path'].lstrip('/')
            if not allowed(name):
                raise ValueError(f'receipt contains unsupported path: {name}')
            if path(root, name).exists():
                names.add(name)
        # Agent units can be installed after the original managed receipt.
        for name in FIXED:
            p = path(root, name)
            if p.exists() and p.read_bytes().startswith(MARKER + b'\n'):
                names.add(name)
        for p in path(root, STATE + '/receipts').glob('*.json'):
            names.add(str(p.relative_to(root)))
        # Include rollback snapshots, but never unrelated /etc/opendkim keys.
        backups = path(root, STATE + '/backups')
        if backups.exists():
            for p in backups.rglob('*'):
                name = str(p.relative_to(root))
                path(root, name)
                if p.is_file():
                    names.add(name)
        for name in names:
            p = path(root, name)
            if not p.is_file():
                raise ValueError(f'not a regular managed file: {name}')
        if not set(CORE).issubset(names):
            raise ValueError('receipt does not own all core mail configuration')
        platform = rc['params'].get('platform', {})
        main = path(root, CORE[0]).read_text()
        backend = re.search(r'(hash|lmdb):/etc/postfix/njiwa/', main)
        if not backend:
            raise ValueError('cannot establish the owned Postfix map backend')
        platform['map_type'] = backend[1]
        meta.update(mode='managed', receipt_id=rc['id'], plan_digest=rc['plan_digest'],
                    platform=platform, paths=sorted(names))
    out.mkdir(mode=0o700, parents=True, exist_ok=True)
    (out / 'recovery.json').write_text(json.dumps(meta, indent=2) + '\n')
    (out / 'managed-paths.txt').write_text(''.join(n + '\n' for n in meta['paths']))


def verify_archive(archive, root, allowed_names=None, queue=False):
    seen = set()
    with tarfile.open(archive, 'r:gz') as tf:
        for member in tf:
            name = safe_name(member.name)
            if name in seen:
                raise ValueError(f'duplicate archive entry: {name}')
            seen.add(name)
            if not (member.isfile() or member.isdir()):
                raise ValueError(f'links and special files are refused: {name}')
            path(root, name)
            if allowed_names is not None and name not in allowed_names:
                raise ValueError(f'unapproved managed archive path: {name}')
            if queue and not (name == 'var/spool/postfix' or name.startswith('var/spool/postfix/')):
                raise ValueError(f'non-queue file in queue archive: {name}')
    return seen


def restore_check(root, src, include):
    manifest = src / 'MANIFEST.sha256'
    entries = {}
    for line in manifest.read_text().splitlines():
        m = re.fullmatch(r'([0-9a-f]{64}) [ *](?:\./)?([A-Za-z0-9_.-]+)', line)
        if not m or m[2] in entries:
            raise ValueError('unsafe or duplicate checksum manifest entry')
        entries[m[2]] = m[1]
    actual = {p.name for p in src.iterdir() if p.name != manifest.name}
    if actual != set(entries) or 'mail.dump' not in entries:
        raise ValueError('manifest must cover every backup artifact, including mail.dump')
    for name, expected in entries.items():
        p = path(src, name)
        if not p.is_file():
            raise ValueError(f'backup artifact is not a regular file: {name}')
        h = hashlib.sha256()
        with p.open('rb') as f:
            for block in iter(lambda: f.read(1024 * 1024), b''):
                h.update(block)
        if h.hexdigest() != expected:
            raise ValueError(f'checksum mismatch: {name}')
    if (src / 'data.tgz').exists():
        # Target paths in the app archive are validated separately against DATA_DIR.
        verify_archive(src / 'data.tgz', Path(os.environ.get('DATA_DIR') or '/tmp/njiwa-recovery-data-validation'))
        if not (os.environ.get('DATA_DIR') or os.environ.get('DATA_VOLUME')):
            raise ValueError('data.tgz requires DATA_DIR or DATA_VOLUME before any restore')
    has_managed = (src / 'managed.tgz').exists()
    if not has_managed:
        if (src / 'postfix-queue.tgz').exists():
            raise ValueError('queue archive requires managed configuration')
        return
    if not include:
        raise ValueError('managed restore requires explicit RESTORE_MANAGED=1; target must be owned or empty')
    if os.geteuid() != 0:
        raise ValueError('managed restore requires root')
    if 'recovery.json' not in entries:
        raise ValueError('legacy managed archive requires reviewed conversion; automatic adoption is refused')
    meta = json.loads((src / 'recovery.json').read_text())
    if meta.get('version') != 1 or meta.get('mode') != 'managed':
        raise ValueError('unsupported recovery metadata')
    names = set(meta['paths'])
    if not set(CORE).issubset(names) or any(not allowed(n) for n in names):
        raise ValueError('invalid managed file inventory')
    if verify_archive(src / 'managed.tgz', root, names) != names:
        raise ValueError('managed archive does not match the approved inventory')
    with tarfile.open(src / 'managed.tgz', 'r:gz') as tf:
        for name in CORE:
            if not tf.extractfile(name).read().startswith(MARKER + b'\n'):
                raise ValueError('archive contains foreign mail configuration')
    # No implicit adoption, even if the mail service is currently stopped.
    owned = receipt(root)
    if not owned:
        queue = path(root, 'var/spool/postfix')
        if queue.exists() and any(p.is_file() for p in queue.rglob('*')):
            raise ValueError('fresh target contains a foreign Postfix queue')
        for name in ('etc/postfix', 'etc/opendkim', 'etc/opendkim.conf', 'etc/exim', 'etc/exim4', 'etc/mail', 'etc/njiwa-mail'):
            p = path(root, name)
            if p.exists() and (not p.is_dir() or any(p.iterdir())):
                raise ValueError(f'fresh target contains mail configuration: {name}; refuse adoption')
        if root == Path('/'):
            # Read-only discovery catches active/containerised services even
            # when their configuration is mounted outside standard paths.
            result = subprocess.run(['ss', '-H', '-ltn'], capture_output=True, text=True, check=True)
            for line in result.stdout.splitlines():
                fields = line.split()
                if len(fields) > 3 and fields[3].rsplit(':', 1)[-1] in ('25', '465', '587'):
                    raise ValueError('fresh target already has a listening mail service')
            result = subprocess.run(['systemctl', 'is-active', 'postfix', 'exim', 'exim4', 'opendkim'], capture_output=True, text=True)
            if 'active' in result.stdout.splitlines():
                raise ValueError('fresh target has an active mail service')
    owned_names = {f['path'].lstrip('/') for f in owned['files']} if owned else set()
    for name in names:
        p = path(root, name)
        if p.exists() and name not in CORE:
            if owned is None:
                raise ValueError(f'fresh target already contains {name}')
            if name not in owned_names and not name.startswith(STATE + '/') and name not in FIXED:
                raise ValueError(f'target receipt does not own {name}')
            if name in FIXED and not p.read_bytes().startswith(MARKER + b'\n'):
                raise ValueError(f'target file is not owned: {name}')
    if (src / 'postfix-queue.tgz').exists():
        verify_archive(src / 'postfix-queue.tgz', root, queue=True)
    backend = meta.get('platform', {}).get('map_type') or ('lmdb' if meta.get('platform', {}).get('family') == 'rhel' else 'hash')
    if backend not in ('hash', 'lmdb'):
        raise ValueError('unsupported map backend')
    print(backend)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('action', choices=['backup-plan', 'restore-check'])
    ap.add_argument('directory', type=Path)
    ap.add_argument('--root', type=Path, default=Path('/'))
    ap.add_argument('--managed', action='store_true')
    args = ap.parse_args()
    try:
        if args.action == 'backup-plan':
            backup_plan(args.root.resolve(), args.directory, args.managed)
        else:
            restore_check(args.root.resolve(), args.directory, args.managed)
    except (ValueError, KeyError, OSError, tarfile.TarError) as e:
        sys.exit(f'recovery refused before mutation: {e}')


if __name__ == '__main__':
    main()
