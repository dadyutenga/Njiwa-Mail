package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// C02: ENCRYPTION_KEY rotation.
//
// Rotation without downtime: put the new key in ENCRYPTION_KEY and the old one
// in ENCRYPTION_KEY_PREVIOUS (the process then seals with the new key and opens
// with either), run `njiwa-mail rekey` until it reports nothing left, and only
// then remove ENCRYPTION_KEY_PREVIOUS. Rekey is idempotent, resumable and safe
// to run while the service is up: each row is replaced only if it still holds
// the bytes that were read.

// sealedSpec describes one sealed column: how to address a row and how to
// rebuild the additional data (AAD) it was sealed with.
type sealedSpec struct {
	Name  string
	Table string
	Col   string
	Key   string // SQL expression, text, unique per row
	AAD   string // SQL expression, text
}

// sealedColumns is every column the application seals. TestEverySealedColumnIsRekeyed
// fails when a migration adds an *_enc column that is missing here.
var sealedColumns = []sealedSpec{
	{"messages.payload_enc", "messages", "payload_enc", "id::text", "id::text"},
	{"sent_copies.data_enc", "sent_copies", "data_enc", "message_id::text", "'sent:'||message_id::text"},
	{"message_attachments.data_enc", "message_attachments", "data_enc", "id::text", "'attachment:'||message_id::text||':'||filename"},
	{"sandbox_captures.raw_enc", "sandbox_captures", "raw_enc", "message_id::text", "'capture:'||message_id::text"},
	{"inbound_messages.text_enc", "inbound_messages", "text_enc", "id::text", "'inbound-text:'||id::text"},
	{"inbound_messages.html_enc", "inbound_messages", "html_enc", "id::text", "'inbound-html:'||id::text"},
	{"drafts.data_enc", "drafts", "data_enc", "id::text", "'draft:'||id::text"},
	{"campaign_recipients.email_enc", "campaign_recipients", "email_enc", "id::text", "'campaign:'||campaign_id::text"},
	{"campaign_recipients.vars_enc", "campaign_recipients", "vars_enc", "id::text", "'campaign-vars:'||campaign_id::text"},
	{"webhook_endpoints.secret_enc", "webhook_endpoints", "secret_enc", "id::text", "'webhook:'||id::text"},
	{"mail_server.smtp_password_enc", "mail_server", "smtp_password_enc", "id::text", "'" + smtpPasswordAAD + "'"},
	{"mail_server.mailcow_api_key_enc", "mail_server", "mailcow_api_key_enc", "id::text", "'" + mailcowAPIKeyAAD + "'"},
	{"transport_profiles.encrypted_secret", "transport_profiles", "encrypted_secret", "id::text",
		"CASE mode WHEN 'managed_mta' THEN '" + managedTLSAAD + "' ELSE '" + smtpPasswordAAD + "' END"},
	{"transport_profiles.encrypted_provider_secret", "transport_profiles", "encrypted_provider_secret", "id::text", "'" + mailcowAPIKeyAAD + "'"},
	{"users.totp_secret_enc", "users", "totp_secret_enc", "id::text", "'totp:'||id::text"},
	{"dns_connections.token_enc", "dns_connections", "token_enc", "id::text", "'dns-connection:'||id::text||':token'"},
	{"dns_connections.refresh_enc", "dns_connections", "refresh_enc", "id::text", "'dns-connection:'||id::text||':refresh'"},
	{"dns_oauth_states.verifier_enc", "dns_oauth_states", "verifier_enc", "encode(state_hash,'hex')", "'dns-oauth:'||encode(state_hash,'hex')"},
}

// SealedColumnNames lists every "table.column" that rekey covers.
func SealedColumnNames() []string {
	var out []string
	for _, sp := range sealedColumns {
		out = append(out, sp.Table+"."+sp.Col)
	}
	return out
}

// RekeyColumn is the outcome for one column or for the attachment files.
type RekeyColumn struct {
	Name       string   `json:"name"`
	Scanned    int      `json:"scanned"`
	Resealed   int      `json:"resealed"`   // re-sealed with the current key (or, in a dry run, still on a previous key)
	Unreadable int      `json:"unreadable"` // no configured key opens it
	Examples   []string `json:"examples,omitempty"`
}

// RekeyReport is the result of a rekey run.
type RekeyReport struct {
	DryRun  bool          `json:"dry_run"`
	Columns []RekeyColumn `json:"columns"`
}

// Pending is how many values still need the current key (dry run) or were re-sealed (real run).
func (r *RekeyReport) Changed() int {
	n := 0
	for _, c := range r.Columns {
		n += c.Resealed
	}
	return n
}

// Unreadable is how many values no configured key can open. Never zero-risk:
// they are lost unless the right key is added to ENCRYPTION_KEY_PREVIOUS.
func (r *RekeyReport) Unreadable() int {
	n := 0
	for _, c := range r.Columns {
		n += c.Unreadable
	}
	return n
}

const rekeyBatch = 200

// Rekey re-seals every sealed value that only a previous key can open. With
// dryRun it only counts them.
func (s *Service) Rekey(ctx context.Context, dryRun bool) (*RekeyReport, error) {
	rep := &RekeyReport{DryRun: dryRun}
	for _, sp := range sealedColumns {
		col, err := s.rekeyColumn(ctx, sp, dryRun)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", sp.Name, err)
		}
		rep.Columns = append(rep.Columns, col)
	}
	files, err := s.rekeyAttachmentFiles(ctx, dryRun)
	rep.Columns = append(rep.Columns, files)
	if err != nil {
		return rep, fmt.Errorf("inbound attachment files: %w", err)
	}
	return rep, nil
}

func (s *Service) rekeyColumn(ctx context.Context, sp sealedSpec, dryRun bool) (RekeyColumn, error) {
	out := RekeyColumn{Name: sp.Name}
	last := ""
	for {
		rows, err := s.Pool.Query(ctx, `SELECT `+sp.Key+`, `+sp.Col+`, `+sp.AAD+` FROM `+sp.Table+
			` WHERE `+sp.Col+` IS NOT NULL AND octet_length(`+sp.Col+`) > 0 AND `+sp.Key+` > $1 ORDER BY `+sp.Key+` LIMIT $2`, last, rekeyBatch)
		if err != nil {
			return out, err
		}
		type row struct {
			key, aad string
			enc      []byte
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.key, &r.enc, &r.aad); err != nil {
				rows.Close()
				return out, err
			}
			batch = append(batch, r)
		}
		rows.Close()
		for _, r := range batch {
			out.Scanned++
			pt, old, err := s.Box.OpenReport(r.enc, []byte(r.aad))
			switch {
			case err != nil:
				out.Unreadable++
				if len(out.Examples) < 5 {
					out.Examples = append(out.Examples, r.key)
				}
			case old:
				out.Resealed++
				if dryRun {
					continue
				}
				sealed, err := s.Box.Seal(pt, []byte(r.aad))
				if err != nil {
					return out, err
				}
				// Compare-and-swap: a concurrent change wins and is picked up next run.
				if _, err := s.Pool.Exec(ctx, `UPDATE `+sp.Table+` SET `+sp.Col+`=$2 WHERE `+sp.Key+`=$1 AND `+sp.Col+`=$3`, r.key, sealed, r.enc); err != nil {
					return out, err
				}
			}
			last = r.key
		}
		if len(batch) < rekeyBatch {
			return out, nil
		}
	}
}

// rekeyAttachmentFiles re-seals sealed inbound attachment files in place
// (temp file, then rename, so a reader never sees a half-written file).
func (s *Service) rekeyAttachmentFiles(ctx context.Context, dryRun bool) (RekeyColumn, error) {
	out := RekeyColumn{Name: "inbound attachment files"}
	if s.Opts.DataDir == "" {
		return out, nil
	}
	last := ""
	for {
		rows, err := s.Pool.Query(ctx, `SELECT id::text, storage_path FROM inbound_attachments WHERE encrypted AND id::text > $1 ORDER BY id::text LIMIT $2`, last, rekeyBatch)
		if err != nil {
			return out, err
		}
		type item struct{ id, rel string }
		var batch []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.rel); err != nil {
				rows.Close()
				return out, err
			}
			batch = append(batch, it)
		}
		rows.Close()
		for _, it := range batch {
			last = it.id
			full, err := attachmentPath(s.Opts.DataDir, it.rel)
			if err != nil {
				continue
			}
			raw, err := os.ReadFile(full)
			if err != nil {
				continue // purged
			}
			out.Scanned++
			pt, old, err := s.Box.OpenReport(raw, attachmentAAD(it.id))
			switch {
			case err != nil:
				out.Unreadable++
				if len(out.Examples) < 5 {
					out.Examples = append(out.Examples, it.id)
				}
			case old:
				out.Resealed++
				if dryRun {
					continue
				}
				sealed, err := s.Box.Seal(pt, attachmentAAD(it.id))
				if err != nil {
					return out, err
				}
				tmp := filepath.Join(filepath.Dir(full), "."+filepath.Base(full)+".rekey")
				if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
					return out, err
				}
				if err := os.Rename(tmp, full); err != nil {
					os.Remove(tmp)
					return out, err
				}
			}
		}
		if len(batch) < rekeyBatch {
			return out, nil
		}
	}
}
