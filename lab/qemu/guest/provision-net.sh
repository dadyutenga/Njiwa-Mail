#!/usr/bin/env bash
# Runs ON the "net" VM (online mode). Builds the simulated internet: authoritative test DNS (dnsmasq) for the
# *.test lab zones with PTRs, and receiving mail servers that verify SPF, DKIM and DMARC. Everything is private to the lab.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
echo "postfix postfix/main_mailer_type select Internet Site" | sudo debconf-set-selections
echo "postfix postfix/mailname string mx.rcv.test" | sudo debconf-set-selections
echo "opendmarc opendmarc/dbconfig-install boolean false" | sudo debconf-set-selections
# Do not let packages start services with their default config (dnsmasq would grab :53 next to systemd-resolved).
printf '#!/bin/sh\nexit 101\n' | sudo tee /usr/sbin/policy-rc.d >/dev/null; sudo chmod +x /usr/sbin/policy-rc.d
sudo DEBIAN_FRONTEND=noninteractive dpkg --configure -a || true
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq dnsmasq postfix opendkim opendmarc postfix-policyd-spf-python opendkim-tools \
  swaks nmap netcat-openbsd dnsutils python3 mailutils >/dev/null

# --- DNS: static lab zone; dynamic records are appended with labdns ---------------------------------
sudo systemctl disable --now systemd-resolved-dummy 2>/dev/null || true
sudo tee /etc/dnsmasq.d/lab.conf >/dev/null <<'C'
listen-address=10.77.0.20
bind-interfaces
no-resolv
no-hosts
domain-needed
local=/test/
local=/10.in-addr.arpa/
host-record=mailout.njiwa.test,10.77.0.10
host-record=mx.rcv.test,10.77.0.20
mx-host=rcv-a.test,mx.rcv.test,10
mx-host=rcv-b.test,mx.rcv.test,10
mx-host=rcv-c.test,mx.rcv.test,10
ptr-record=20.0.77.10.in-addr.arpa,mx.rcv.test
conf-file=/etc/dnsmasq.d/zone-dynamic.conf
C
sudo touch /etc/dnsmasq.d/zone-dynamic.conf
sudo tee /usr/local/sbin/labdns >/dev/null <<'C'
#!/bin/sh
# labdns txt NAME VALUE | mx NAME HOST [PREF] | a NAME IP | ptr IP NAME | del NAME | list
# This stands in for the registrar/DNS provider of the lab. Real DNS is never touched.
f=/etc/dnsmasq.d/zone-dynamic.conf
chunks() { printf '%s' "$1" | fold -w 200 | sed 's/.*/"&"/' | paste -sd, -; }
case "$1" in
  txt) echo "txt-record=$2,$(chunks "$3")" >>$f ;;
  mx)  echo "mx-host=$2,$3,${4:-10}" >>$f ;;
  a)   echo "host-record=$2,$3" >>$f ;;
  ptr) echo "ptr-record=$(echo "$2" | awk -F. '{print $4"."$3"."$2"."$1}').in-addr.arpa,$3" >>$f ;;
  del) grep -v -F "=$2," $f >$f.new; mv $f.new $f ;;
  list) cat $f ;;
  *) echo usage; exit 2 ;;
esac
[ "$1" = list ] || systemctl restart dnsmasq
C
sudo chmod +x /usr/local/sbin/labdns

# --- receiver: three provider-like domains, user alice, everything else is rejected at RCPT (a 550 like Gmail) ----
sudo useradd -m -s /usr/sbin/nologin alice 2>/dev/null || true
sudo -u alice mkdir -p /home/alice/Maildir/{new,cur,tmp}
echo "alice alice" | sudo tee /etc/postfix/lab-users >/dev/null; sudo postmap /etc/postfix/lab-users
sudo postconf -e "myhostname=mx.rcv.test" "mydestination=rcv-a.test, rcv-b.test, rcv-c.test, mx.rcv.test, localhost" \
  "local_recipient_maps=hash:/etc/postfix/lab-users" "home_mailbox=Maildir/" "inet_interfaces=all" \
  "smtpd_milters=" "milter_default_action=accept" \
  "smtpd_tls_security_level=may" "smtpd_tls_cert_file=/etc/ssl/certs/ssl-cert-snakeoil.pem" "smtpd_tls_key_file=/etc/ssl/private/ssl-cert-snakeoil.key" \
  "smtpd_recipient_restrictions=reject_unauth_destination,check_policy_service unix:private/policyd-spf" \
  "policyd-spf_time_limit=3600" "smtp_host_lookup=dns" "disable_vrfy_command=yes"
grep -q policyd-spf /etc/postfix/master.cf || echo 'policyd-spf unix - n n - 0 spawn user=policyd-spf argv=/usr/bin/policyd-spf' | sudo tee -a /etc/postfix/master.cf >/dev/null
sudo adduser postfix opendkim >/dev/null 2>&1 || true

# opendkim: verify only (its unbound resolver needs an unbound forward-zone file; the lab DNS is 10.77.0.20)
# dnsmasq does not synthesise empty non-terminals (_domainkey.<domain>), so unbound must not minimise names or harden below NXDOMAIN
printf 'server:\n  qname-minimisation: no\n  harden-below-nxdomain: no\nforward-zone:\n  name: "."\n  forward-addr: 10.77.0.20\n' | sudo tee /etc/opendkim-resolv.conf >/dev/null
sudo tee /etc/opendkim.conf >/dev/null <<'C'
Syslog yes
Mode v
Socket inet:8891@127.0.0.1
PidFile /run/opendkim/opendkim.pid
ResolverConfiguration /etc/opendkim-resolv.conf
UserID opendkim
AuthservID mx.rcv.test
C
# opendmarc: evaluates, never rejects (we want to read the verdict)
sudo tee /etc/opendmarc.conf >/dev/null <<'C'
Syslog true
Socket inet:8893@127.0.0.1
PidFile /run/opendmarc/opendmarc.pid
UserID opendmarc
AuthservID mx.rcv.test
TrustedAuthservIDs mx.rcv.test
RejectFailures false
SPFSelfValidate true
IgnoreAuthenticatedClients false
PublicSuffixList /usr/share/publicsuffix/public_suffix_list.dat
FailureReports false
C
sudo tee /usr/local/sbin/lab-receiver >/dev/null <<'C'
#!/bin/sh
# lab-receiver stop|start|inbox|clear
case "$1" in
  stop)  systemctl stop postfix ;;
  start) systemctl start postfix ;;
  inbox) ls -1 /home/alice/Maildir/new ;;
  clear) rm -f /home/alice/Maildir/new/* /home/alice/Maildir/cur/* ;;
esac
C
sudo chmod +x /usr/local/sbin/lab-receiver
sudo rm -f /usr/sbin/policy-rc.d
sudo systemctl enable --now dnsmasq opendkim opendmarc
sudo systemctl restart dnsmasq opendkim opendmarc postfix
echo provisioned-net

# The packaged opendkim/opendmarc milters could not resolve the lab zone (libunbound never queried the lab DNS), so their
# Authentication-Results were misleading; verdicts come from lab-verify, an independent verifier (see guest/lab-verify.py).
sudo postconf -e "smtpd_milters="
sudo systemctl disable --now opendkim opendmarc 2>/dev/null || true
sudo systemctl restart postfix
