package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/mailer"
)

// Managed mode (backlog E02-E04, E09). The host installer runs as root
// outside the API; when it finishes it records here: a verified receipt, the
// worker's mutual-TLS credentials (sealed), and the DKIM public records it
// signs with. The web process never touches the host.

const managedTLSAAD = "transport_profile:managed_tls"

// ManagedInstall is what a successful installer apply reports.
type ManagedInstall struct {
	ReceiptID      string
	PlanDigest     string
	ApprovedBy     string
	BackupRef      string
	PostfixVersion string
	SubmissionHost string
	SubmissionPort int
	ServerName     string
	Selector       string
	DKIMRecords    map[string]string // domain -> TXT value
	ClientCertPEM  []byte
	ClientKeyPEM   []byte
	CAPEM          []byte
}

type managedSecret struct {
	ClientCert string `json:"client_cert"`
	ClientKey  string `json:"client_key"`
	CA         string `json:"ca"`
}

// RecordManagedInstall stores an installer result on the managed profile and
// a verified installer receipt. The profile revision changes, so earlier
// connection checks no longer count.
func (s *Service) RecordManagedInstall(ctx context.Context, a Actor, in ManagedInstall) error {
	if in.SubmissionHost == "" || in.SubmissionPort == 0 || len(in.ClientCertPEM) == 0 || len(in.ClientKeyPEM) == 0 || len(in.CAPEM) == 0 {
		return badRequest("the installer result is incomplete")
	}
	if _, err := tls.X509KeyPair(in.ClientCertPEM, in.ClientKeyPEM); err != nil {
		return badRequest("the client certificate and key do not match")
	}
	sec, _ := json.Marshal(managedSecret{ClientCert: string(in.ClientCertPEM), ClientKey: string(in.ClientKeyPEM), CA: string(in.CAPEM)})
	sealed, err := s.Box.Seal(sec, []byte(managedTLSAAD))
	if err != nil {
		return unavailable("seal", err)
	}
	cfg, _ := json.Marshal(map[string]any{"submission_host": in.SubmissionHost, "submission_port": in.SubmissionPort,
		"server_name": in.ServerName, "selector": in.Selector, "dkim_records": in.DKIMRecords})
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("managed_install", err)
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM transport_profiles WHERE mode='managed_mta' AND status <> 'retired'
ORDER BY is_active DESC, created_at LIMIT 1 FOR UPDATE`).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO transport_profiles(mode, label, status) VALUES ('managed_mta','Send from this server','draft') RETURNING id::text`).Scan(&id)
	}
	if err != nil {
		return unavailable("managed_install", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE transport_profiles SET config_json=$2::jsonb, encrypted_secret=$3, config_revision=config_revision+1,
  status='needs_action', updated_at=now() WHERE id=$1::uuid`, id, cfg, sealed); err != nil {
		return unavailable("managed_install", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO installer_receipts(profile_id, plan_digest, approved_by, action, result_code, config_backup_ref, applied_version, verified_at, rollback_state)
VALUES ($1::uuid,$2,$3,'apply','ok',$4,$5,now(),'available')`, id, in.PlanDigest, in.ApprovedBy, in.BackupRef, in.PostfixVersion); err != nil {
		return unavailable("managed_install", err)
	}
	if err := audit(ctx, tx, a, "installer.apply", "transport_profile", id, map[string]any{"receipt": in.ReceiptID, "digest": in.PlanDigest,
		"domains": len(in.DKIMRecords), "postfix": in.PostfixVersion}); err != nil {
		return unavailable("audit", err)
	}
	return tx.Commit(ctx)
}

// RecordManagedRollback marks the latest receipt rolled back, so managed
// mode is no longer considered installed.
func (s *Service) RecordManagedRollback(ctx context.Context, a Actor, digest string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE installer_receipts SET rollback_state='rolled_back' WHERE plan_digest=$1 AND rollback_state <> 'rolled_back'`, digest)
	if err != nil {
		return unavailable("managed_rollback", err)
	}
	return s.Audit(ctx, a, "installer.rollback", "installer", digest, nil)
}

// openManagedProfile builds the worker's SMTP settings for the managed MTA:
// the private submission listener, verified against the installer's CA, with
// the worker's client certificate. ok is false until the installer has run.
func (s *Service) openManagedProfile(cj, secEnc []byte) (mailer.SMTPConfig, bool, error) {
	var cfg mailer.SMTPConfig
	var c struct {
		Host       string `json:"submission_host"`
		Port       int    `json:"submission_port"`
		ServerName string `json:"server_name"`
	}
	_ = json.Unmarshal(cj, &c)
	if c.Host == "" || c.Port == 0 || secEnc == nil {
		return cfg, false, nil
	}
	raw, err := s.Box.Open(secEnc, []byte(managedTLSAAD))
	if err != nil {
		return cfg, false, err
	}
	var sec managedSecret
	if err := json.Unmarshal(raw, &sec); err != nil {
		return cfg, false, err
	}
	pair, err := tls.X509KeyPair([]byte(sec.ClientCert), []byte(sec.ClientKey))
	if err != nil {
		return cfg, false, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(sec.CA)) {
		return cfg, false, errors.New("managed CA is not PEM")
	}
	cfg = mailer.SMTPConfig{Host: c.Host, Port: c.Port, ServerName: c.ServerName, RootCAs: pool, ClientCert: &pair, HeloName: "njiwa-worker"}
	return cfg, true, nil
}

// managedDKIM is the DKIM record the managed signer uses for domain, if the
// installer configured it.
func (s *Service) managedDKIM(ctx context.Context, domain string) string {
	var v string
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(config_json->'dkim_records'->>$1,'') FROM transport_profiles
WHERE mode='managed_mta' AND status <> 'retired' ORDER BY is_active DESC, created_at LIMIT 1`, domain).Scan(&v)
	return v
}

// ClaimedDomains lists every active domain claim in the installation: the
// sending domains the managed signer must hold keys for.
func (s *Service) ClaimedDomains(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT name FROM domain_claims WHERE state='active' ORDER BY name`)
	if err != nil {
		return nil, unavailable("claims", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
