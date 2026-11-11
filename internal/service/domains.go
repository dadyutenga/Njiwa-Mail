package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"time"
)

// DomainRecord is one DNS record a domain needs, with what was found.
type DomainRecord struct {
	Key       string `json:"key"`        // ownership, spf, dkim, dmarc, mx, return_path
	Type      string `json:"type"`       // TXT, MX
	Host      string `json:"host"`       // full name
	ShortHost string `json:"short_host"` // label most DNS panels want ("@" for the apex)
	Value     string `json:"value"`
	Priority  int    `json:"priority,omitempty"`
	Pass      bool   `json:"pass"`
	Found     string `json:"found"`
	Hint      string `json:"hint,omitempty"`
	// Placeholder marks a Value that is guidance with a part to fill in, not a
	// record that can be published as is. Clients must not offer to copy it.
	Placeholder bool `json:"placeholder,omitempty"`
}

// Which server actually sends a domain's mail decides what SPF and DKIM must
// authorise. Only the managed MTA (this host) and an operator-chosen Mailcow
// (whose IP and hostname MAIL_HOSTNAME/OUTBOUND_IP describe) are known here.
// For a generic SMTP server Njiwa-Mail cannot see the egress or the signer.
const (
	senderManaged = "managed"
	senderMailcow = "mailcow"
	senderGeneric = "generic"
)

func (s *Service) senderKind(ctx context.Context) string {
	var mode, provider string
	if err := s.Pool.QueryRow(ctx, `SELECT mode, provider_type FROM transport_profiles WHERE is_active`).Scan(&mode, &provider); err != nil {
		return senderGeneric
	}
	switch {
	case mode == "managed_mta":
		return senderManaged
	case provider == "mailcow":
		return senderMailcow
	}
	return senderGeneric
}

func shortHost(host, domain string) string {
	if host == domain {
		return "@"
	}
	return strings.TrimSuffix(host, "."+domain)
}

func sendingPurpose(p string) bool   { return p == "sending" || p == "both" }
func receivingPurpose(p string) bool { return p == "receiving" || p == "both" || p == "inbound" }

// WSCreateDomain adds a domain to this project. Purpose: sending, receiving or both.
// bulk marks a separate subdomain used only for bulk announcements.
func (s *Service) WSCreateDomain(ctx context.Context, sc *Scope, name, purpose string, bulk bool) (string, string, error) {
	if purpose == "inbound" {
		purpose = "receiving"
	}
	if purpose != "sending" && purpose != "receiving" && purpose != "both" {
		return "", "", badRequest("purpose must be sending, receiving or both")
	}
	name, ok := NormalizeDomain(name)
	if !ok {
		return "", "", badRequest("enter a domain name such as notify.example.com")
	}
	var exists int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM domains WHERE project_id=$1 AND name=$2`, sc.ProjectID, name).Scan(&exists); err != nil {
		return "", "", unavailable("domain_lookup", err)
	}
	if exists > 0 {
		return "", "", &Error{Status: 409, Code: "domain_exists", Message: name + " is already added to this project"}
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", "", unavailable("token", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO domains(project_id, name, purpose, verify_token, bulk) VALUES ($1,$2,$3,$4,$5) RETURNING id::text`,
		sc.ProjectID, name, purpose, token, bulk).Scan(&id)
	if err != nil {
		return "", "", unavailable("domain_create", err)
	}
	return id, "njiwa-mail-verify=" + token, nil
}

func (s *Service) WSDomains(ctx context.Context, sc *Scope, q string, attention bool, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT d.id::text AS id, d.name, d.purpose, d.bulk, d.verified_at, d.spf_ok, d.dkim_ok, d.dmarc_ok, d.mx_ok,
  d.last_checked_at, d.created_at, d.records,
  (d.verified_at IS NULL OR (d.purpose IN ('sending','both') AND NOT (d.spf_ok AND d.dkim_ok AND d.dmarc_ok))
     OR (d.purpose IN ('receiving','both') AND NOT d.mx_ok)) AS needs_attention
FROM domains d WHERE d.project_id=$1 AND ($2='' OR d.name LIKE '%' || lower($2) || '%')
  AND (NOT $3 OR d.verified_at IS NULL OR (d.purpose IN ('sending','both') AND NOT (d.spf_ok AND d.dkim_ok AND d.dmarc_ok))
       OR (d.purpose IN ('receiving','both') AND NOT d.mx_ok))
ORDER BY d.name`, sc.ProjectID, q, attention)
}

func (s *Service) WSDomain(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	d, err := s.one(ctx, `SELECT d.id::text AS id, d.name, d.purpose, d.bulk, d.verified_at, d.spf_ok, d.dkim_ok, d.dmarc_ok, d.mx_ok,
  d.last_checked_at, d.created_at, d.records, d.mailcow_provisioned_at, 'njiwa-mail-verify=' || d.verify_token AS txt_challenge,
  (d.verified_at IS NULL OR (d.purpose IN ('sending','both') AND NOT (d.spf_ok AND d.dkim_ok AND d.dmarc_ok))
     OR (d.purpose IN ('receiving','both') AND NOT d.mx_ok)) AS needs_attention
FROM domains d WHERE d.id=$1 AND d.project_id=$2`, id, sc.ProjectID)
	if err != nil {
		return nil, err
	}
	name := d["name"].(string)
	// Records are shown even before the first check, from the expected values.
	if recs, ok := d["records"].([]any); !ok || len(recs) == 0 {
		var token, purpose string
		_ = s.Pool.QueryRow(ctx, `SELECT verify_token, purpose FROM domains WHERE id=$1`, id).Scan(&token, &purpose)
		d["records"] = s.expectedRecords(s.senderKind(ctx), name, purpose, token, "")
	}
	if d["senders"], err = s.maps(ctx, `SELECT si.id::text AS id, si.email, e.mode FROM sender_identities si JOIN environments e ON e.id=si.environment_id
WHERE si.project_id=$1 AND split_part(si.email,'@',2)=$2 ORDER BY si.email`, sc.ProjectID, name); err != nil {
		return nil, err
	}
	if d["mailboxes"], err = s.maps(ctx, `SELECT a.id::text AS id, a.local_part || '@' || a.domain AS address, e.mode FROM inbound_addresses a
JOIN environments e ON e.id=a.environment_id WHERE a.project_id=$1 AND a.domain=$2 AND NOT a.temporary ORDER BY a.local_part`, sc.ProjectID, name); err != nil {
		return nil, err
	}
	if d["recent_messages"], err = s.maps(ctx, `SELECT id::text AS id, state, recipient_masked AS recipient, COALESCE(NULLIF(subject,''), template) AS subject, created_at
FROM messages WHERE environment_id=$1 AND split_part(sender,'@',2)=$2 ORDER BY created_at DESC LIMIT 10`, sc.EnvID, name); err != nil {
		return nil, err
	}
	d["mailcow_api"] = s.MailcowConfigured(ctx)
	d["provider_type"] = s.activeProvider(ctx)
	d["dkim_selector"] = s.Opts.DKIMSelector
	d["mail_host"] = s.Opts.MailHost
	return d, nil
}

// expectedRecords lists what the domain needs. kind says which server sends
// the mail (see senderKind). dkimValue is the real public key when known
// (from the Mailcow API); otherwise the DKIM value is a placeholder.
func (s *Service) expectedRecords(kind, name, purpose, token, dkimValue string) []DomainRecord {
	host := fqdn(s.Opts.MailHost)
	recs := []DomainRecord{{Key: "ownership", Type: "TXT", Host: name, Value: "njiwa-mail-verify=" + token,
		Hint: "Proves the domain is yours. It can stay published; it does not affect mail."}}
	if sendingPurpose(purpose) {
		spf := DomainRecord{Key: "spf", Type: "TXT", Host: name,
			Hint: "If the domain already has a v=spf1 record, edit that record and add the mechanism; never publish two."}
		if kind == senderGeneric {
			// Never authorise this host or MAIL_HOSTNAME for a server we cannot see.
			spf.Value, spf.Placeholder = "v=spf1 <your mail server's sending IPs (ip4:/ip6:) or SPF include> -all", true
			spf.Hint = "SPF must authorise the server that actually sends your mail: your SMTP server's outbound IP addresses, or the include: its operator documents. Njiwa-Mail cannot see that server, so replace the part in <…>. " + spf.Hint
		} else {
			spf.Value = "v=spf1 mx -all"
			if ip := strings.TrimSpace(s.Opts.OutboundIP); ip != "" {
				if strings.Contains(ip, ":") {
					spf.Value = "v=spf1 mx ip6:" + ip + " -all"
				} else {
					spf.Value = "v=spf1 mx ip4:" + ip + " -all"
				}
			} else if host != "" {
				spf.Value = "v=spf1 mx a:" + host + " -all"
			}
		}
		dkim := DomainRecord{Key: "dkim", Type: "TXT", Host: s.Opts.DKIMSelector + "._domainkey." + name, Value: dkimValue,
			Hint: "Paste the whole key. Some panels split long values into several quoted strings; that is fine."}
		if dkimValue == "" {
			dkim.Placeholder = true
			if kind == senderMailcow {
				dkim.Value = "v=DKIM1; k=rsa; p=<public key from Mailcow: Configuration → ARC/DKIM keys>"
			} else {
				dkim.Value = "v=DKIM1; k=rsa; p=<public key from the server that signs your mail>"
				dkim.Hint = "Get the public key from the DKIM settings of the server that signs your mail, for selector " + s.Opts.DKIMSelector + ", and publish exactly the value it gives you. Some panels split long values into several quoted strings; that is fine."
			}
		}
		recs = append(recs, spf, dkim,
			DomainRecord{Key: "dmarc", Type: "TXT", Host: "_dmarc." + name, Value: "v=DMARC1; p=none; rua=mailto:postmaster@" + name,
				Hint: "Start with p=none, then move to quarantine once reports look clean."})
		if _, bd, ok := splitAddr(s.Opts.BounceAddress); ok {
			rp := mxRecord(kind, "return_path", bd, host)
			rp.Hint = "Bounces return to " + s.Opts.BounceAddress + ". " + rp.Hint
			recs = append(recs, rp)
		}
	}
	if receivingPurpose(purpose) {
		recs = append(recs, mxRecord(kind, "mx", name, host))
	}
	for i := range recs {
		recs[i].ShortHost = shortHost(recs[i].Host, name)
		recs[i].Found = "(not checked yet)"
	}
	return recs
}

// CheckDomain looks up every record of one domain, stores the per-record
// results and updates the verification flags that gate live sending.
// mxRecord is an MX requirement whose guidance names exactly the target the
// check compares against (MAIL_HOSTNAME), with priority 10.
func mxRecord(kind, key, at, host string) DomainRecord {
	r := DomainRecord{Key: key, Type: "MX", Host: at, Value: host, Priority: 10}
	if host == "" {
		r.Value, r.Placeholder = "<your mail server hostname>", true
		if kind == senderMailcow {
			r.Value = "<your Mailcow hostname>"
		}
		r.Hint = "Point it at the mail server that receives for this name, with priority 10. Set MAIL_HOSTNAME to that host so the check can confirm it."
		return r
	}
	r.Hint = "Point the MX at " + host + " with priority 10, the host shown here: the check passes only when the MX target is exactly this host. On Cloudflare the mail hostname must be DNS only (grey cloud), never proxied."
	return r
}

func (s *Service) CheckDomain(ctx context.Context, id string, r Resolver) ([]DomainRecord, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	var name, token, purpose, projectID string
	if err := s.Pool.QueryRow(ctx, `SELECT name, verify_token, purpose, project_id::text FROM domains WHERE id=$1`, id).Scan(&name, &token, &purpose, &projectID); err != nil {
		return nil, notFound(err)
	}
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dkimKnown := ""
	if pk, err := s.mailcowDKIM(ctx, name); err == nil && pk != "" {
		dkimKnown = pk
	}
	kind := s.senderKind(ctx)
	recs := s.expectedRecords(kind, name, purpose, token, dkimKnown)
	sip := s.senderIP(kind)
	envDomain := name // the SMTP MAIL FROM domain that SPF authenticates
	if _, bd, ok := splitAddr(s.Opts.BounceAddress); ok {
		envDomain = bd
	}
	host := fqdn(s.Opts.MailHost)
	apex, apexErr := r.LookupTXT(ctx, name)
	var verified, spfOK, dkimOK, dmarcOK, mxOK bool
	for i := range recs {
		rec := &recs[i]
		switch rec.Key {
		case "ownership":
			rec.Found = observed(pick(apex, "njiwa-mail-verify="), apexErr)
			for _, v := range apex {
				if strings.TrimSpace(v) == rec.Value {
					rec.Pass = true
				}
			}
			verified = rec.Pass
		case "spf":
			pass, why, spf := evalSPF(ctx, r, name, sip)
			rec.Found = observed(spf, apexErr)
			rec.Pass = pass
			if why != "" {
				rec.Hint = why
			}
			spfOK = rec.Pass
		case "dkim":
			txt, err := r.LookupTXT(ctx, rec.Host)
			rec.Found = observed(txt, err)
			var hint string
			rec.Pass, hint = evalDKIM(txt, err, dkimKnown)
			if hint != "" {
				rec.Hint = hint
			}
			dkimOK = rec.Pass
		case "dmarc":
			// SPF counts for DMARC on the envelope (bounce) domain.
			envSPF := spfOK
			if envDomain != name {
				envSPF, _, _ = evalSPF(ctx, r, envDomain, sip)
			}
			var hint string
			rec.Pass, rec.Found, hint = evalDMARC(ctx, r, name, envDomain, dkimOK, envSPF)
			if hint != "" {
				rec.Hint = hint
			}
			dmarcOK = rec.Pass
		case "mx", "return_path":
			mx, err := r.LookupMX(ctx, rec.Host)
			var vals []string
			for _, m := range mx {
				vals = append(vals, fqdn(m.Host))
				if host == "" || fqdn(m.Host) == host {
					rec.Pass = true
				}
			}
			rec.Found = observed(vals, err)
			if rec.Key == "mx" {
				mxOK = rec.Pass
			}
		}
	}
	// Ownership proof alone is not enough: only one project in the
	// installation may hold the name (D01).
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, unavailable("domain_check", err)
	}
	defer tx.Rollback(ctx)
	if verified {
		mine, state, err := claimForVerifiedDomain(ctx, tx, name, projectID)
		if err != nil {
			return nil, unavailable("domain_claim", err)
		}
		if !mine {
			verified = false
			for i := range recs {
				if recs[i].Key == "ownership" {
					recs[i].Pass = false
					recs[i].Hint = "The ownership record is published, but this domain is already used by another project in this installation. An operator must transfer it to this project first."
					if state == "review_required" {
						recs[i].Hint = "The ownership record is published, but more than one project claimed this domain. An operator must review it and assign it to one project."
					}
				}
			}
		}
	}
	raw, _ := json.Marshal(recs)
	_, err = tx.Exec(ctx, `UPDATE domains SET verified_at=CASE WHEN $2 THEN COALESCE(verified_at, now()) ELSE NULL END,
spf_ok=$3, dkim_ok=$4, dmarc_ok=$5, mx_ok=$6, records=$7, last_checked_at=now() WHERE id=$1`, id, verified, spfOK, dkimOK, dmarcOK, mxOK, raw)
	if err != nil {
		return nil, unavailable("domain_check", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailable("domain_check", err)
	}
	for _, rec := range recs {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO dns_checks(domain, record_type, expected, observed, pass) VALUES ($1,$2,$3,$4,$5)`,
			rec.Host, strings.ToUpper(rec.Key), rec.Value, rec.Found, rec.Pass)
	}
	return recs, nil
}

// RecheckPendingDomains re-checks domains that are not fully passing and were
// added in the last 7 days, at most once every 10 minutes each.
func (s *Service) RecheckPendingDomains(ctx context.Context, r Resolver) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text FROM domains
WHERE created_at > now() - interval '7 days' AND (last_checked_at IS NULL OR last_checked_at < now() - interval '10 minutes')
  AND (verified_at IS NULL OR (purpose IN ('sending','both') AND NOT (spf_ok AND dkim_ok AND dmarc_ok)) OR (purpose IN ('receiving','both') AND NOT mx_ok))
LIMIT 50`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.CheckDomain(ctx, id, r); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// WSDeleteDomain refuses while senders or mailboxes use the domain.
func (s *Service) WSDeleteDomain(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var name string
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1 AND project_id=$2`, id, sc.ProjectID).Scan(&name); err != nil {
		return notFound(err)
	}
	var users []string
	rows, err := s.Pool.Query(ctx, `SELECT email FROM sender_identities WHERE project_id=$1 AND split_part(email,'@',2)=$2
UNION ALL SELECT local_part || '@' || domain FROM inbound_addresses WHERE project_id=$1 AND domain=$2 ORDER BY 1 LIMIT 20`, sc.ProjectID, name)
	if err != nil {
		return unavailable("domain_users", err)
	}
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			users = append(users, u)
		}
	}
	rows.Close()
	if len(users) > 0 {
		return &Error{Status: 409, Code: "domain_in_use", Message: "remove these senders and mailboxes first: " + strings.Join(users, ", ")}
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM domains WHERE id=$1 AND project_id=$2`, id, sc.ProjectID); err != nil {
		return unavailable("domain_delete", err)
	}
	if err := s.releaseClaimIfUnused(ctx, name, sc.ProjectID); err != nil {
		return unavailable("domain_delete", err)
	}
	return nil
}

// ReceivingDomains lists domains a mailbox may use in this scope: the
// configured inbound domain, and this project's verified receiving domains.
func (s *Service) ReceivingDomains(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	out, err := s.maps(ctx, `SELECT name, purpose, mx_ok, verified_at IS NOT NULL AS verified FROM domains
WHERE project_id=$1 AND purpose IN ('receiving','both') AND verified_at IS NOT NULL ORDER BY name`, sc.ProjectID)
	if err != nil {
		return nil, err
	}
	if s.Opts.InboundDomain != "" {
		out = append([]map[string]any{{"name": s.Opts.InboundDomain, "purpose": "inbound", "mx_ok": true, "verified": true, "shared": true}}, out...)
	}
	return out, nil
}
