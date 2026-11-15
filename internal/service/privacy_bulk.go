package service

import (
	"context"
)

// MaxBulkPrivacy is how many addresses one bulk request may name.
const MaxBulkPrivacy = 500

// BulkRecipientResult is one address of a bulk privacy request. The address is returned masked.
type BulkRecipientResult struct {
	Email  string         `json:"email"`
	Status string         `json:"status"` // deleted, would_delete, nothing_found, invalid, error
	Found  map[string]int `json:"found,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// recipientFootprint counts what is stored about an address, without returning any of it.
func recipientFootprint(x map[string]any) map[string]int {
	n := map[string]int{}
	for _, k := range []string{"messages", "bounces", "inbound", "contacts", "form_submissions", "chats"} {
		switch v := x[k].(type) {
		case []map[string]any:
			if len(v) > 0 {
				n[k] = len(v)
			}
		}
	}
	if sup, _ := x["suppressed"].(bool); sup {
		n["suppressions"] = 1
	}
	return n
}

// BulkRecipients deletes everything stored about each address (messages and their evidence,
// bounces, suppressions, inbound mail, contacts, form submissions and chats), or with dryRun
// only reports what would go. Each address is handled on its own: one bad address does not
// stop the others. Duplicates are handled once. Addresses are never returned in clear.
func (s *Service) BulkRecipients(ctx context.Context, project, mode string, emails []string, dryRun bool) ([]BulkRecipientResult, error) {
	if len(emails) == 0 {
		return nil, badRequest("list the email addresses")
	}
	if len(emails) > MaxBulkPrivacy {
		return nil, badRequest("at most 500 addresses per request")
	}
	if _, _, err := s.envID(ctx, project, mode); err != nil {
		return nil, notFound(err)
	}
	seen := map[string]bool{}
	out := make([]BulkRecipientResult, 0, len(emails))
	for _, raw := range emails {
		e, ok := NormalizeEmail(raw)
		if !ok {
			out = append(out, BulkRecipientResult{Email: "(invalid)", Status: "invalid"})
			continue
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		r := BulkRecipientResult{Email: maskEmail(e)}
		x, err := s.ExportRecipient(ctx, project, mode, e)
		if err != nil {
			r.Status, r.Error = "error", errorCode(err)
			out = append(out, r)
			continue
		}
		r.Found = recipientFootprint(x)
		switch {
		case len(r.Found) == 0:
			r.Status = "nothing_found"
		case dryRun:
			r.Status = "would_delete"
		default:
			if err := s.DeleteRecipient(ctx, project, mode, e); err != nil {
				r.Status, r.Error = "error", errorCode(err)
			} else {
				r.Status = "deleted"
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func errorCode(err error) string {
	if e, ok := IsError(err); ok {
		return e.Code
	}
	return "service_unavailable"
}
