#!/usr/bin/env python3
"""lab-verify MESSAGEFILE [CLIENT_IP]: an independent DKIM/SPF/DMARC check for the QEMU lab receiver.
It does NOT use Njiwa-Mail code. DKIM: RFC 6376 (rsa-sha256, relaxed/simple or relaxed/relaxed) with the key read from the lab DNS.
SPF: pyspf. DMARC: relaxed alignment of the From domain with the DKIM d= / SPF MAIL FROM domain, policy read from the lab DNS.
These are lab verdicts, not Gmail's or Outlook's."""
import base64, hashlib, re, sys, email, DNS
from email import policy
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding
import spf
DNS.defaults['server'] = ['10.77.0.20']
def txt(name):
    try:
        r = DNS.DnsRequest(name, qtype='TXT', timeout=5).req()
        return [b''.join(a['data']).decode() if isinstance(a['data'], list) else a['data'] for a in r.answers if a['typename'] == 'TXT']
    except Exception: return []
raw = open(sys.argv[1], 'rb').read().replace(b'\r\n', b'\n')
ip = sys.argv[2] if len(sys.argv) > 2 else '10.77.0.10'
head, _, body = raw.partition(b'\n\n')
lines = head.decode('utf8', 'replace').split('\n'); hdrs = []
for l in lines:
    if l[:1] in ' \t' and hdrs: hdrs[-1] += '\n' + l
    else: hdrs.append(l)
def relax(h):
    n, _, v = h.partition(':'); v = re.sub(r'\s+', ' ', v.replace('\n', ' ')).strip(); return n.strip().lower() + ':' + v
def find_all(name): return [h for h in hdrs if h.split(':', 1)[0].strip().lower() == name]
res = {'dkim': 'none', 'spf': 'none', 'dmarc': 'none'}; why = {}
sigs = find_all('dkim-signature')
dkim_domains = []
for s in sigs[:1]:
    tags = dict((k.strip(), v.strip().replace('\n', '').replace(' ', '')) for k, v in (t.split('=', 1) for t in re.sub(r'\s+', '', s.split(':', 1)[1]).split(';') if '=' in t))
    d, sel = tags['d'], tags['s']; c = tags.get('c', 'simple/simple').split('/'); ch = c[0]; cb = c[1] if len(c) > 1 else 'simple'
    recs = txt(f'{sel}._domainkey.{d}'); why['dkim_key'] = f'{sel}._domainkey.{d}: ' + (f'{len(recs)} TXT' if recs else 'not found')
    if not recs: res['dkim'] = 'fail'; why['dkim'] = 'key not found'; continue
    p = re.search(r'p=([A-Za-z0-9+/=]+)', recs[0].replace(' ', '')).group(1)
    pub = serialization.load_der_public_key(base64.b64decode(p))
    b = body.replace(b'\n', b'\r\n')
    if cb == 'relaxed':
        ls = [re.sub(rb'[ \t]+', b' ', l.rstrip(b' \t')) for l in b.split(b'\r\n')]; b = b'\r\n'.join(ls)
    b = b.rstrip(b'\r\n') + b'\r\n' if b.strip(b'\r\n') else (b'' if cb == 'relaxed' else b'\r\n')
    bh = base64.b64encode(hashlib.sha256(b).digest()).decode()
    if bh != tags['bh']: res['dkim'] = 'fail'; why['dkim'] = 'body hash mismatch'; continue
    pieces = []; used = {}
    for name in tags['h'].split(':'):
        cand = find_all(name.lower()); i = used.get(name.lower(), 0); used[name.lower()] = i + 1
        if i < len(cand):
            h = cand[len(cand) - 1 - i]; pieces.append(relax(h) if ch == 'relaxed' else h.replace('\n', '\r\n'))
    sig_h = re.sub(r'(b=)[^;]*', r'\1', s.replace('\n', ' ') if False else s)
    sig_h = re.sub(r'(\bb=)[^;]*$|(\bb=)[^;]*;', lambda m: (m.group(1) or m.group(2)) + (';' if m.group(2) else ''), s)
    canon = '\r\n'.join(pieces) + '\r\n' + (relax(sig_h) if ch == 'relaxed' else sig_h.replace('\n', '\r\n'))
    try:
        pub.verify(base64.b64decode(tags['b']), canon.encode(), padding.PKCS1v15(), hashes.SHA256()); res['dkim'] = 'pass'; dkim_domains.append(d)
    except Exception as e: res['dkim'] = 'fail'; why['dkim'] = 'signature does not verify'
rp = (find_all('return-path') or ['Return-Path: <>'])[0].split(':', 1)[1].strip().strip('<>')
helo = 'mailout.njiwa.test'
try: r = spf.check2(ip, rp, helo); res['spf'] = r[0]; why['spf'] = f'{r[0]} for {rp.split("@")[-1] or helo}'
except Exception as e: res['spf'] = 'temperror'; why['spf'] = str(e)
frm = re.search(r'@([\w.-]+)', (find_all('from') or ['From: '])[0]); fd = frm.group(1).lower() if frm else ''
def org(x): return '.'.join(x.split('.')[-2:])
al_dkim = any(org(d) == org(fd) for d in dkim_domains); al_spf = res['spf'] == 'pass' and org(rp.split('@')[-1]) == org(fd)
pol = [t for t in txt('_dmarc.' + fd) if t.startswith('v=DMARC1')]
if pol: res['dmarc'] = 'pass' if (al_dkim or al_spf) else 'fail'; why['dmarc'] = f'policy {pol[0]}; dkim_aligned={al_dkim} spf_aligned={al_spf}'
print(f"spf={res['spf']} dkim={res['dkim']} dmarc={res['dmarc']} header.from={fd}")
for k, v in why.items(): print(f'  {k}: {v}')
