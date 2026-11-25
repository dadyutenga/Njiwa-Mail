package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/njiwa-tz/mail/internal/auditreport"
	"github.com/njiwa-tz/mail/internal/service"
)

func auditFilter(c fiber.Ctx) (service.AuditFilter, error) {
	f := service.AuditFilter{Actor: c.Query("actor"), Action: c.Query("action"), Query: c.Query("q")}
	var err error
	if f.From, err = timeParam(c, "from"); err != nil {
		return f, err
	}
	if f.To, err = timeParam(c, "to"); err != nil {
		return f, err
	}
	if len(f.Actor) > 320 || len(f.Action) > 100 || len(f.Query) > 200 {
		return f, &service.Error{Status: 400, Code: "invalid_request", Message: "filter too long"}
	}
	return f, nil
}

// exportAudit downloads the matching entries as pdf, csv or jsonl (one JSON object per
// line, for a SIEM). Every export is itself recorded in the audit log.
func (s *Server) exportAudit(c fiber.Ctx, u *service.AdminUser) (any, error) {
	format := c.Query("format", "pdf")
	if format != "pdf" && format != "csv" && format != "jsonl" {
		return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "format must be pdf, csv or jsonl"}
	}
	f, err := auditFilter(c)
	if err != nil {
		return nil, err
	}
	ctx := reqCtx(c)
	integrity, err := s.Svc.VerifyAudit(ctx)
	if err != nil {
		return nil, err
	}
	entries, truncated, err := s.Svc.ExportAudit(ctx, f)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	name := "njiwa-mail-audit-" + now.Format("20060102-150405")
	var body []byte
	var ctype string
	switch format {
	case "csv":
		body, err = auditCSV(entries)
		ctype = "text/csv; charset=utf-8"
	case "jsonl":
		body, err = auditJSONL(entries)
		ctype = "application/x-ndjson"
	default:
		body, err = auditreport.PDF(auditreport.Report{
			GeneratedAt: now, GeneratedBy: u.Email, Filter: f, Entries: entries, Truncated: truncated,
			Integrity: integrity, Max: service.MaxAuditExport,
		})
		ctype = "application/pdf"
	}
	if err != nil {
		return nil, &service.Error{Status: 500, Code: "export_failed", Message: "the export could not be generated"}
	}
	s.audit(c, u, "audit.export", "audit_log", "", map[string]any{
		"format": format, "entries": len(entries), "truncated": truncated, "integrity_ok": integrity.OK,
		"filter": map[string]any{"actor": f.Actor, "action": f.Action, "q": f.Query, "from": f.From, "to": f.To},
	})
	return rawResponse{header: map[string]string{
		"Content-Type":            ctype,
		"Content-Disposition":     fmt.Sprintf(`attachment; filename="%s.%s"`, name, format),
		"Cache-Control":           "no-store",
		"X-Audit-Entries":         strconv.Itoa(len(entries)),
		"X-Audit-Truncated":       strconv.FormatBool(truncated),
		"X-Audit-Integrity":       map[bool]string{true: "verified", false: "broken"}[integrity.OK],
		"X-Audit-Chain-Head-Hash": integrity.HeadHash,
	}, body: body}, nil
}

// csvSafe stops a spreadsheet from treating a cell as a formula.
func csvSafe(v string) string {
	if v != "" && (v[0] == '=' || v[0] == '+' || v[0] == '-' || v[0] == '@' || v[0] == '\t' || v[0] == '\r') {
		return "'" + v
	}
	return v
}

func auditCSV(entries []service.AuditEntry) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"seq", "at_utc", "actor", "action", "target_type", "target_id", "user_id", "api_key_id", "details", "prev_hash", "hash"})
	for _, e := range entries {
		d, _ := json.Marshal(e.Details)
		if err := w.Write([]string{strconv.FormatInt(e.Seq, 10), e.At.Format(time.RFC3339Nano), csvSafe(e.Actor), csvSafe(e.Action),
			csvSafe(e.TargetType), csvSafe(e.TargetID), e.UserID, e.APIKeyID, csvSafe(string(d)), e.PrevHash, e.Hash}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

func auditJSONL(entries []service.AuditEntry) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}
