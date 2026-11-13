#!/usr/bin/env bash
# Runs ON net (online once, for the SASL packages): turns the lab receiver's Postfix into a generic "existing SMTP server" that accepts
# authenticated TLS submission on 587 (user relayuser) and delivers to the lab mailbox. Stands in for G04(b)/C06/G06's existing server.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq libsasl2-modules sasl2-bin >/dev/null
sudo install -d /etc/postfix/sasl
printf 'pwcheck_method: auxprop\nauxprop_plugin: sasldb\nmech_list: PLAIN LOGIN\n' | sudo tee /etc/postfix/sasl/smtpd.conf >/dev/null
echo 'relaypass-lab-12345' | sudo saslpasswd2 -p -c -u mx.rcv.test relayuser
sudo chgrp postfix /etc/sasldb2; sudo chmod 640 /etc/sasldb2
sudo postconf -e "smtpd_sasl_local_domain=mx.rcv.test" "smtpd_sasl_security_options=noanonymous"
grep -q '^submission ' /etc/postfix/master.cf || sudo tee -a /etc/postfix/master.cf >/dev/null <<'M'
submission inet n - n - - smtpd
  -o syslog_name=postfix/submission
  -o smtpd_tls_security_level=encrypt
  -o smtpd_sasl_auth_enable=yes
  -o smtpd_relay_restrictions=permit_sasl_authenticated,reject
  -o smtpd_recipient_restrictions=permit_sasl_authenticated,reject
M
sudo systemctl restart postfix
sleep 2; ss -ltn | grep -c ':587 '
