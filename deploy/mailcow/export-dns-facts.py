#!/usr/bin/env python3
"""Export public Mailcow DNS facts only; never modify Mailcow or export secrets.

The operator supplies verified egress IPs. SMTP DNS addresses alone do not prove
outbound routing. Run periodically as root and mount the output directory read-only
into Njiwa with LOCAL_MAILCOW_FACTS_FILE pointing to the exported JSON file.
"""
import argparse
import datetime
import ipaddress
import json
import os
import pathlib
import subprocess
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--mailcow-dir', default='/opt/mailcow-dockerized')
p.add_argument('--smtp-host', required=True)
p.add_argument('--egress-ip', action='append', required=True)
p.add_argument('--output', required=True)
a = p.parse_args()
for address in a.egress_ip:
    if not ipaddress.ip_address(address).is_global:
        raise SystemExit('Egress addresses must be verified public IP addresses')

def compose(service, *command):
    return subprocess.check_output(['docker', 'compose', 'exec', '-T', service, *command], cwd=a.mailcow_dir, timeout=20, text=True).strip()

# A configured upstream relay means the host addresses are not authoritative.
if compose('postfix-mailcow', 'postconf', '-h', 'relayhost') or compose('postfix-mailcow', 'postconf', '-h', 'sender_dependent_relayhost_maps'):
    raise SystemExit('An upstream SMTP relay is configured; obtain its authoritative sending facts')
host = compose('postfix-mailcow', 'postconf', '-h', 'myhostname')
if host.lower() != a.smtp_host.lower():
    raise SystemExit('Configured SMTP hostname must match this Mailcow instance')

def redis(*args):
    # REDISPASS stays inside the container. No password enters command arguments
    # or stdout, and only the two public DKIM hashes are queried.
    return compose('redis-mailcow', 'sh', '-c', 'REDISCLI_AUTH="$REDISPASS" exec redis-cli --raw "$@"', 'redis-public-facts', *args)

keys = {}
selectors = set()
for domain in redis('HKEYS', 'DKIM_PUB_KEYS').splitlines():
    selector = redis('HGET', 'DKIM_SELECTORS', domain)
    key = redis('HGET', 'DKIM_PUB_KEYS', domain)
    if not selector or not key:
        continue
    selectors.add(selector)
    keys[domain.lower()] = key if key.startswith('v=DKIM1;') else 'v=DKIM1; k=rsa; p=' + key
if len(selectors) > 1:
    raise SystemExit('Multiple DKIM selectors: refusing to relabel keys; configure per-profile facts instead')
snapshot = dict(smtp_host=a.smtp_host.lower(), mail_host=host.lower(), egress_ips=a.egress_ip,
                dkim_selector=next(iter(selectors), ''), dkim_keys=keys,
                updated_at=datetime.datetime.now(datetime.timezone.utc).isoformat())
output = pathlib.Path(a.output)
output.parent.mkdir(parents=True, exist_ok=True)
fd, temporary = tempfile.mkstemp(dir=output.parent, prefix='.mailcow-public-')
try:
    with os.fdopen(fd, 'w') as file:
        json.dump(snapshot, file)
        file.write('\n')
        os.fchmod(file.fileno(), 0o644)  # Contains only public DNS material.
    os.replace(temporary, output)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
print('Exported public DNS facts for', len(keys), 'Mailcow domains')
