package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jhillyerd/enmime/v2"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

// Workspace API: every route is under /admin/v1/w/{project}/{mode}. The scope
// is resolved from the path before the handler runs; an unknown project or
// mode is 404 project_not_found, and every query filters by that scope.

type wsFn func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error)

func (s *Server) ws(h wsFn) fiber.Handler {
	return s.adm(func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		sc, err := s.Svc.ResolveScope(reqCtx(c), c.Params("project"), c.Params("mode"))
		if err != nil {
			return nil, err
		}
		return h(c, u, sc)
	})
}

func scoped(sc *service.Scope, extra map[string]any) map[string]any {
	out := map[string]any{"project": sc.Project, "mode": sc.Mode}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func boolQuery(c fiber.Ctx, k string) bool {
	v := c.Query(k)
	return v == "1" || v == "true"
}

func (s *Server) workspaceRoutes(app *fiber.App) {
	svc := s.Svc
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { route(app, method, base+path, s.ws(fn)) }
	ctx := reqCtx

	// ---- context, checklist, search, overview
	h("GET", "/context", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WorkspaceContext(ctx(c), sc)
	})
	h("GET", "/checklist", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		o, err := svc.Checklist(ctx(c), sc)
		if err != nil {
			return nil, err
		}
		// Installation readiness is reported beside, never inside, the project
		// list: a summary only, the details live on the operator page.
		inst, err := svc.InstallationSummary(ctx(c))
		if err != nil {
			return nil, err
		}
		inst["environment_override"] = s.EnvSMTP != nil
		return map[string]any{"scope": o.Scope, "steps": o.Steps, "complete": o.Complete, "progress": o.Progress, "installation": inst}, nil
	})
	h("GET", "/search", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.Search(ctx(c), sc, c.Query("q"))
		return map[string]any{"items": items}, err
	})
	h("GET", "/overview", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WorkspaceOverview(ctx(c), sc)
	})

	// ---- messages (Sent, Scheduled, Message log, Unknown queue)
	h("GET", "/messages", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		from, err := timeParam(c, "from")
		if err != nil {
			return nil, err
		}
		to, err := timeParam(c, "to")
		if err != nil {
			return nil, err
		}
		items, more, err := svc.WSMessages(ctx(c), sc, service.WSMessageFilter{State: c.Query("state"), Source: c.Query("source"),
			Sender: c.Query("sender"), Template: c.Query("template"), Recipient: c.Query("recipient"), APIKey: c.Query("api_key"),
			Campaign: c.Query("campaign"), From: from, To: to}, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/messages/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WSMessage(ctx(c), sc, c.Params("id"))
	})
	h("GET", "/messages/:id/export", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		m, err := svc.ExportMessage(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.export_message", "message", c.Params("id"), scoped(sc, nil))
		return rawResponse{header: map[string]string{"Content-Type": "application/json",
			"Content-Disposition": `attachment; filename="njiwa-mail-message-` + c.Params("id") + `.json"`}, body: mustJSON(m)}, nil
	})
	h("DELETE", "/messages/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteMessage(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.delete_message", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/messages/:id/copy", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.SentCopy(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/messages/:id/cancel", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSCancel(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "message.cancel", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/messages/:id/send-now", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.SendNow(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "message.send_now", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/messages/:id/retry", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		acc, err := svc.Retry(ctx(c), sc, c.Params("id"), u.Email)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "message.retry", "message", acc.ID, scoped(sc, map[string]any{"original": c.Params("id")}))
		return acc, nil
	})
	// F02: unknown messages with a suggested outcome from the evidence we hold.
	h("GET", "/unknown", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ReconciliationQueue(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/messages/:id/resolve", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			As   string `json:"as"`
			Note string `json:"note"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.MessageInScope(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		if err := svc.ResolveUnknown(ctx(c), c.Params("id"), in.As, in.Note); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "message.resolve_unknown", "message", c.Params("id"), scoped(sc, map[string]any{"as": in.As}))
		return nil, nil
	})

	// ---- compose and drafts
	h("POST", "/compose", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ComposeInput
		if len(c.Body()) > 8<<20 {
			return nil, &service.Error{Status: 413, Code: "body_too_large"}
		}
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		acc, err := svc.Compose(ctx(c), sc, u.Email, c.Get("Idempotency-Key"), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "message.compose", "message", acc.ID, scoped(sc, map[string]any{"reply": in.InReplyTo != ""}))
		return acc, nil
	})
	// Controlled transport test through the real queue (C04).
	h("POST", "/transport-test", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		acc, err := svc.QueueTransportTest(ctx(c), sc, u.Email, in.From, in.To)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "transport.test_queued", "message", acc.ID, scoped(sc, nil))
		return acc, nil
	})
	h("POST", "/recipients/verify", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Emails []string `json:"emails"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		res, err := svc.VerifyAddresses(ctx(c), sc.EnvID, in.Emails)
		if err != nil {
			return nil, err
		}
		return map[string]any{"results": res}, nil
	})
	h("POST", "/recipients/check", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Emails []string `json:"emails"`
			Bulk   bool     `json:"bulk"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		blocked, err := svc.CheckRecipients(ctx(c), sc, in.Emails, in.Bulk)
		return map[string]any{"blocked": blocked}, err
	})
	h("GET", "/drafts", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListDraftsMail(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/drafts/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetDraftMail(ctx(c), sc, c.Params("id"))
	})
	saveDraft := func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		in := map[string]any{}
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.SaveDraftMail(ctx(c), sc, u.ID, c.Params("id"), in)
		if err != nil {
			return nil, err
		}
		return map[string]string{"id": id}, nil
	}
	h("POST", "/drafts", saveDraft)
	h("PUT", "/drafts/:id", saveDraft)
	h("DELETE", "/drafts/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.DeleteDraftMail(ctx(c), sc, c.Params("id"))
	})

	// ---- inbox and mailboxes
	h("GET", "/inbox", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.Inbox(ctx(c), sc, service.InboxFilter{Mailbox: c.Query("mailbox"), Query: c.Query("q"), Label: c.Query("label"),
			Unread: boolQuery(c, "unread"), Archived: boolQuery(c, "archived")}, pageOf(c))
		return list(c, items, more, err)
	})
	// Live stream of new inbound mail (server-sent events). The browser's
	// EventSource reconnects by itself and resumes from Last-Event-ID.
	h("GET", "/inbox/stream", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		after, afterID := time.Now().UTC(), ""
		if last := c.Get("Last-Event-ID"); last != "" {
			if ns, id, ok := strings.Cut(last, ":"); ok {
				if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
					after, afterID = time.Unix(0, n).UTC(), id
				}
			}
		}
		return s.eventStream(c, func(ctx context.Context) ([]sseEvent, error) {
			items, err := svc.InboxSince(ctx, sc.EnvID, after, afterID, 50)
			out := make([]sseEvent, 0, len(items))
			for _, it := range items {
				after, afterID = it.ReceivedAt, it.ID
				out = append(out, sseEvent{Name: "inbound", ID: strconv.FormatInt(it.ReceivedAt.UnixNano(), 10) + ":" + it.ID, Data: it})
			}
			return out, err
		}), nil
	})
	// ---- contacts and lists (marketing audiences)
	h("GET", "/contacts", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListContacts(ctx(c), sc, service.ContactFilter{ListID: c.Query("list"), Status: c.Query("status"),
			Domain: c.Query("domain"), Query: c.Query("q")}, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/contacts", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ContactInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, created, err := svc.UpsertContact(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "contact.saved", "contact", id, scoped(sc, nil))
		return map[string]any{"id": id, "created": created}, nil
	})
	h("POST", "/contacts/import", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.ImportInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		dry := boolQuery(c, "dry_run")
		rep, err := svc.ImportContacts(ctx(c), sc, in, dry)
		if err != nil {
			return nil, err
		}
		if !dry {
			s.audit(c, u, "contact.imported", "contact", "", scoped(sc, map[string]any{"created": rep.Created, "updated": rep.Updated, "invalid": rep.Invalid}))
		}
		return rep, nil
	})
	h("GET", "/contacts/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetContact(ctx(c), sc, c.Params("id"))
	})
	h("DELETE", "/contacts/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteContact(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "contact.deleted", "contact", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/contacts/:id/resubscribe", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			ListID string `json:"list_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.Resubscribe(ctx(c), sc, c.Params("id"), in.ListID); err != nil {
			return nil, err
		}
		s.audit(c, u, "contact.resubscribed", "contact", c.Params("id"), scoped(sc, map[string]any{"list_id": in.ListID}))
		return nil, nil
	})
	h("GET", "/lists", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListLists(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/lists", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct{ Name, Description string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateList(ctx(c), sc, in.Name, in.Description)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "list.created", "list", id, scoped(sc, nil))
		return map[string]any{"id": id}, nil
	})
	h("DELETE", "/lists/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteList(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "list.deleted", "list", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/lists/:id/members", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			ContactIDs []string `json:"contact_ids"`
			Remove     bool     `json:"remove"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		n, err := svc.SetMembership(ctx(c), sc, c.Params("id"), in.ContactIDs, !in.Remove)
		if err != nil {
			return nil, err
		}
		return map[string]any{"changed": n}, nil
	})

	h("GET", "/segments", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListSegments(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/segments/preview", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.SegmentRules
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.SegmentPreview(ctx(c), sc, in)
	})
	h("POST", "/segments", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Name  string               `json:"name"`
			Rules service.SegmentRules `json:"rules"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateSegment(ctx(c), sc, in.Name, in.Rules)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "segment.created", "segment", id, scoped(sc, nil))
		return map[string]any{"id": id}, nil
	})
	h("DELETE", "/segments/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteSegment(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "segment.deleted", "segment", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})

	// ---- HTTP ingest of inbound mail (signed with a per-environment secret)
	h("GET", "/inbound-ingest", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.IngestStatus(ctx(c), sc.EnvID)
	})
	h("POST", "/inbound-ingest/secret", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		secret, err := svc.RotateIngestSecret(ctx(c), sc.EnvID)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound_ingest.secret_rotated", "environment", sc.EnvID, scoped(sc, nil))
		st, err := svc.IngestStatus(ctx(c), sc.EnvID)
		if err != nil {
			return nil, err
		}
		st["secret"] = secret // shown once
		return st, nil
	})
	h("DELETE", "/inbound-ingest", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DisableIngest(ctx(c), sc.EnvID); err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound_ingest.disabled", "environment", sc.EnvID, scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/inbox/unread", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.UnreadCounts(ctx(c), sc)
	})
	h("GET", "/inbox/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.OpenInbox(ctx(c), sc, c.Params("id"))
	})
	h("GET", "/inbox/:id/html", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		m, err := svc.GetInbound(ctx(c), sc.EnvID, c.Params("id"), true)
		if err != nil {
			return nil, err
		}
		html, _ := m["html"].(string)
		return sandboxedHTML(html), nil
	})
	h("GET", "/inbox/:id/attachments/:aid", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		f, err := svc.OpenAttachment(ctx(c), sc.EnvID, c.Params("id"), c.Params("aid"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.attachment_download", "inbound_attachment", c.Params("aid"), scoped(sc, nil))
		return download(f), nil
	})
	h("PATCH", "/inbox/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Read     *bool `json:"read"`
			Archived *bool `json:"archived"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return nil, svc.UpdateInbox(ctx(c), sc, c.Params("id"), in.Read, in.Archived)
	})
	h("DELETE", "/inbox/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteInbox(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.delete", "inbound_message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/inbox/:id/block", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		sender, err := svc.BlockSender(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.block_sender", "blocked_sender", "", scoped(sc, nil))
		return map[string]string{"sender": sender}, nil
	})
	h("GET", "/blocked-senders", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListBlocked(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("DELETE", "/blocked-senders/:sender", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.Unblock(ctx(c), sc, c.Params("sender"))
	})
	h("GET", "/mailboxes", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListMailboxes(ctx(c), sc, false, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/receiving-domains", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ReceivingDomains(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/mailboxes", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.MailboxInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateMailbox(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "mailbox.create", "inbound_address", id, scoped(sc, map[string]any{"domain": in.Domain}))
		return map[string]string{"id": id}, nil
	})
	h("PATCH", "/mailboxes/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.SetMailboxEnabled(ctx(c), sc, c.Params("id"), in.Enabled); err != nil {
			return nil, err
		}
		s.audit(c, u, "mailbox.update", "inbound_address", c.Params("id"), scoped(sc, map[string]any{"enabled": in.Enabled}))
		return nil, nil
	})
	h("DELETE", "/mailboxes/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteMailbox(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "mailbox.delete", "inbound_address", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/temp-inboxes", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListMailboxes(ctx(c), sc, true, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/temp-inboxes", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			TTL string `json:"ttl"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		out, err := svc.CreateTempInbox(ctx(c), sc, in.TTL)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "temp_inbox.create", "inbound_address", out["id"].(string), scoped(sc, map[string]any{"ttl": in.TTL}))
		return out, nil
	})
	h("DELETE", "/temp-inboxes/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.DeleteMailbox(ctx(c), sc, c.Params("id"))
	})

	// ---- domains
	h("GET", "/domains", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSDomains(ctx(c), sc, c.Query("q"), boolQuery(c, "attention"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/domains", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Name    string `json:"name"`
			Purpose string `json:"purpose"`
			Bulk    bool   `json:"bulk"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, txt, err := svc.WSCreateDomain(ctx(c), sc, in.Name, in.Purpose, in.Bulk)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.create", "domain", id, scoped(sc, map[string]any{"name": in.Name, "purpose": in.Purpose, "bulk": in.Bulk}))
		return map[string]string{"id": id, "txt": txt}, nil
	})
	h("GET", "/domains/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WSDomain(ctx(c), sc, c.Params("id"))
	})
	// Signed-mail evidence (D06): check a received message's DKIM signature.
	h("POST", "/domains/:id/verify-message", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Raw string `json:"raw"`
		}
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		res, err := svc.VerifyReceivedMessage(ctx(c), sc, c.Params("id"), []byte(in.Raw), s.Resolver)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.verify_message", "domain", c.Params("id"), scoped(sc, map[string]any{"aligned": res.Aligned, "recorded": res.Recorded}))
		return res, nil
	})
	h("PUT", "/domains/:id/return-path", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			BounceDomain string `json:"bounce_domain"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.SetDomainReturnPath(ctx(c), sc, c.Params("id"), in.BounceDomain); err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.return_path", "domain", c.Params("id"), scoped(sc, map[string]any{"bounce_domain": strings.ToLower(strings.TrimSpace(in.BounceDomain))}))
		return svc.WSDomain(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/domains/:id/check", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if _, err := svc.WSDomain(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		recs, err := svc.CheckDomain(ctx(c), c.Params("id"), s.Resolver)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.check", "domain", c.Params("id"), scoped(sc, nil))
		return map[string]any{"records": recs}, nil
	})
	h("POST", "/domains/:id/mailcow", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		steps, err := svc.ProvisionDomain(ctx(c), sc, u.Actor(), c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.mailcow_provision", "domain", c.Params("id"), scoped(sc, nil))
		return map[string]any{"steps": steps}, nil
	})
	h("DELETE", "/domains/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSDeleteDomain(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.delete", "domain", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})

	// ---- senders
	h("GET", "/senders", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSSenders(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/bulk-senders", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.BulkSenders(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/senders", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.WSAddSender(ctx(c), sc, in.Email, in.DisplayName)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.upsert", "sender", id, scoped(sc, nil))
		return map[string]string{"id": id}, nil
	})
	h("POST", "/senders/:id/default", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.SetDefaultSender(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.default", "sender", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/senders/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSDeleteSender(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.delete", "sender", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})

	// ---- templates (this project's and shared ones)
	h("GET", "/templates", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSTemplates(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/templates", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.TemplateInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		in.Project = sc.Project
		id, err := svc.CreateTemplate(ctx(c), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.create", "template", id, scoped(sc, map[string]any{"slug": in.Slug}))
		return map[string]string{"id": id}, nil
	})
	tpl := func(write bool, fn wsFn) wsFn {
		return func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
			own, err := svc.TemplateInScope(ctx(c), sc, c.Params("id"))
			if err != nil {
				return nil, err
			}
			if write && !own {
				return nil, &service.Error{Status: 403, Code: "shared_template", Message: "shared templates are edited by an admin from the CLI; duplicate it to change it here"}
			}
			return fn(c, u, sc)
		}
	}
	h("GET", "/templates/:id", tpl(false, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetTemplate(ctx(c), c.Params("id"))
	}))
	h("PATCH", "/templates/:id", tpl(true, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Description *string `json:"description"`
			Archived    *bool   `json:"archived"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateTemplate(ctx(c), c.Params("id"), in.Description, in.Archived, nil, nil); err != nil {
			return nil, err
		}
		s.audit(c, u, "template.update", "template", c.Params("id"), scoped(sc, map[string]any{"archived": in.Archived}))
		return nil, nil
	}))
	h("PUT", "/templates/:id/draft", tpl(true, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.VersionInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		v, err := svc.SaveDraft(ctx(c), c.Params("id"), in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.save_draft", "template", c.Params("id"), scoped(sc, map[string]any{"version": v}))
		return map[string]int{"version": v}, nil
	}))
	h("POST", "/templates/:id/versions/:version/publish", tpl(true, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		v, _ := strconv.Atoi(c.Params("version"))
		if err := svc.PublishVersion(ctx(c), c.Params("id"), v); err != nil {
			return nil, err
		}
		s.audit(c, u, "template.publish", "template", c.Params("id"), scoped(sc, map[string]any{"version": v}))
		return nil, nil
	}))
	h("POST", "/templates/:id/versions/:version/preview", tpl(false, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		v, _ := strconv.Atoi(c.Params("version"))
		var in struct {
			Locale    string         `json:"locale"`
			Variables map[string]any `json:"variables"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.PreviewVersion(ctx(c), c.Params("id"), v, in.Locale, in.Variables)
	}))
	h("POST", "/templates/:id/versions/:version/test-send", tpl(false, func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		v, _ := strconv.Atoi(c.Params("version"))
		var in service.TestSend
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		in.Project, in.Mode = sc.Project, sc.Mode
		acc, err := svc.TestSendVersion(ctx(c), c.Params("id"), v, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.test_send", "message", acc.ID, scoped(sc, map[string]any{"template_id": c.Params("id"), "version": v}))
		return acc, nil
	}))
	h("POST", "/templates/:id/duplicate", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Slug string `json:"slug"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.DuplicateTemplate(ctx(c), sc, c.Params("id"), in.Slug)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "template.duplicate", "template", id, scoped(sc, map[string]any{"from": c.Params("id")}))
		return map[string]string{"id": id}, nil
	})

	// ---- automations
	h("GET", "/automations", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListRules(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/automations", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.RuleInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateRule(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "automation.create", "automation_rule", id, scoped(sc, nil))
		return map[string]string{"id": id}, nil
	})
	h("POST", "/automations/reorder", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.ReorderRules(ctx(c), sc, in.IDs); err != nil {
			return nil, err
		}
		s.audit(c, u, "automation.reorder", "automation_rule", "", scoped(sc, nil))
		return nil, nil
	})
	h("PATCH", "/automations/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.RuleInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateRule(ctx(c), sc, c.Params("id"), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "automation.update", "automation_rule", c.Params("id"), scoped(sc, map[string]any{"enabled": in.Enabled}))
		return nil, nil
	})
	h("DELETE", "/automations/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteRule(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "automation.delete", "automation_rule", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/automations/:id/test", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			InboundID string `json:"inbound_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.TestRule(ctx(c), sc, c.Params("id"), in.InboundID)
	})
	h("GET", "/automations/:id/runs", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.RuleRuns(ctx(c), sc, c.Params("id"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- bulk announcements (admin)
	h("GET", "/campaigns", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListCampaigns(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/campaigns/check", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Recipients string `json:"recipients"`
		}
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		return svc.CheckCampaignRecipients(ctx(c), sc, in.Recipients)
	})
	h("POST", "/campaigns", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.CampaignInput
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		id, rep, err := svc.CreateCampaign(ctx(c), sc, u.Email, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "campaign.create", "campaign", id, scoped(sc, map[string]any{"recipients": rep.Valid}))
		return map[string]any{"id": id, "report": rep}, nil
	})
	h("GET", "/campaigns/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetCampaign(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/campaigns/:id/:action", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.CampaignAction(ctx(c), sc, c.Params("id"), c.Params("action")); err != nil {
			return nil, err
		}
		s.audit(c, u, "campaign."+c.Params("action"), "campaign", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/unsubscribes", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSUnsubscribes(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})

	// ---- suppressions, bounces, complaints
	h("GET", "/suppressions", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSSuppressions(ctx(c), sc, c.Query("source"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/suppressions", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Email  string `json:"email"`
			Reason string `json:"reason"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.Suppress(ctx(c), sc.Project, sc.Mode, in.Email, in.Reason); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "suppression.add", "suppression", "", scoped(sc, map[string]any{"reason": in.Reason}))
		return nil, nil
	})
	h("DELETE", "/suppressions/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := decode(c, &in); err != nil || strings.TrimSpace(in.Reason) == "" {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "a reason is required to remove a suppression"}
		}
		if err := svc.DeleteSuppression(ctx(c), sc.EnvID, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "suppression.remove", "suppression", c.Params("id"), scoped(sc, map[string]any{"reason": in.Reason}))
		return nil, nil
	})
	h("GET", "/bounces", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSBounces(ctx(c), sc, c.Query("type"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- developers: API keys, webhooks, sandbox
	h("GET", "/keys", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSKeys(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/keys", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Scopes   []string `json:"scopes"`
			Label    string   `json:"label"`
			TTLHours int      `json:"ttl_hours"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if in.TTLHours < 0 || len(in.Label) > 100 {
			return nil, &service.Error{Status: 400, Code: "invalid_request", Message: "invalid lifetime or label"}
		}
		tok, prefix, err := svc.CreateKey(ctx(c), sc.Project, sc.Mode, in.Scopes, in.Label, time.Duration(in.TTLHours)*time.Hour)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.create", "api_key", prefix, scoped(sc, map[string]any{"scopes": in.Scopes}))
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST", "/keys/:prefix/rotate", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.KeyInScope(ctx(c), sc, c.Params("prefix")); err != nil {
			return nil, err
		}
		tok, prefix, err := svc.RotateKey(ctx(c), c.Params("prefix"), 24*time.Hour)
		if err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.rotate", "api_key", c.Params("prefix"), scoped(sc, map[string]any{"new_prefix": prefix}))
		return map[string]string{"prefix": prefix, "key": tok}, nil
	})
	h("POST", "/keys/:prefix/revoke", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.KeyInScope(ctx(c), sc, c.Params("prefix")); err != nil {
			return nil, err
		}
		if err := svc.RevokeKey(ctx(c), c.Params("prefix")); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.revoke", "api_key", c.Params("prefix"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/webhooks", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSWebhooks(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/webhooks", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, secret, err := svc.CreateWebhook(ctx(c), sc.Project, sc.Mode, in.URL, in.Events)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.create", "webhook_endpoint", id, scoped(sc, map[string]any{"events": in.Events}))
		return map[string]string{"id": id, "signing_secret": secret}, nil
	})
	inEndpoint := func(fn wsFn) wsFn {
		return func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
			if err := svc.InScope(ctx(c), sc, "webhook_endpoints", c.Params("id")); err != nil {
				return nil, err
			}
			return fn(c, u, sc)
		}
	}
	h("PATCH", "/webhooks/:id", inEndpoint(func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			URL           *string  `json:"url"`
			Events        []string `json:"events"`
			Enabled       *bool    `json:"enabled"`
			SchemaVersion *int     `json:"schema_version"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if in.SchemaVersion != nil {
			if err := svc.SetWebhookSchemaVersion(ctx(c), c.Params("id"), *in.SchemaVersion); err != nil {
				return nil, err
			}
		}
		if err := svc.UpdateWebhook(ctx(c), c.Params("id"), in.URL, in.Events, in.Enabled); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.update", "webhook_endpoint", c.Params("id"), scoped(sc, map[string]any{"enabled": in.Enabled}))
		return nil, nil
	}))
	h("DELETE", "/webhooks/:id", inEndpoint(func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteWebhook(ctx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.delete", "webhook_endpoint", c.Params("id"), scoped(sc, nil))
		return nil, nil
	}))
	h("POST", "/webhooks/:id/rotate-secret", inEndpoint(func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		secret, err := svc.RotateWebhookSecret(ctx(c), c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.rotate_secret", "webhook_endpoint", c.Params("id"), scoped(sc, nil))
		return map[string]string{"signing_secret": secret}, nil
	}))
	h("GET", "/webhook-deliveries", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSDeliveries(ctx(c), sc, c.Query("endpoint_id"), c.Query("state"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/webhook-deliveries/:id/replay", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeliveryInScope(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		if err := svc.ReplayDelivery(ctx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.replay", "webhook_delivery", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/sandbox", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSCaptures(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/sandbox/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.CaptureInScope(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		raw, err := svc.AdminCapture(ctx(c), c.Params("id"))
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": c.Params("id"), "raw": string(raw)}
		if env, err := enmime.ReadEnvelope(bytes.NewReader(raw)); err == nil {
			out["subject"], out["from"], out["to"] = env.GetHeader("Subject"), env.GetHeader("From"), env.GetHeader("To")
			out["text"], out["html"] = env.Text, env.HTML
		}
		s.audit(c, u, "sandbox.view_capture", "message", c.Params("id"), scoped(sc, nil))
		return out, nil
	})
	h("GET", "/analytics", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.Analytics(ctx(c), sc.Project, sc.Mode, c.Query("template"), c.Query("domain"))
		return map[string]any{"items": items}, err
	})
	h("POST", "/privacy/export", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct{ Email string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		x, err := svc.ExportRecipient(ctx(c), sc.Project, sc.Mode, in.Email)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.export", "recipient", "", scoped(sc, map[string]any{"format": c.Query("format", "json")}))
		if c.Query("format") == "csv" {
			b, err := service.RecipientExportCSV(x)
			if err != nil {
				return nil, err
			}
			return rawResponse{header: map[string]string{"Content-Type": "text/csv; charset=utf-8",
				"Content-Disposition": `attachment; filename="njiwa-mail-recipient-export.csv"`}, body: b}, nil
		}
		return x, nil
	})
	h("POST", "/privacy/delete", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct{ Email string }
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.DeleteRecipient(ctx(c), sc.Project, sc.Mode, in.Email); err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.delete", "recipient", "", scoped(sc, nil))
		return nil, nil
	})

	s.mailServerRoutes(app)
	s.installationRoutes(app)
	s.mtaRoutes(app)

	// ---- public "view in browser" copy (signed, expiring link). The sender's
	// HTML runs in a CSP sandbox: no script, no forms, no same-origin access.
	app.Get("/v1/view/:token", func(c fiber.Ctx) error {
		subject, body, err := s.Svc.OpenWebView(reqCtx(c), c.Params("token"))
		c.Set("Cache-Control", "private, no-store")
		c.Set("X-Robots-Tag", "noindex, nofollow")
		c.Set("Referrer-Policy", "no-referrer")
		if err != nil {
			if e, ok := service.IsError(err); ok && e.Status == 404 {
				c.Status(404)
				c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
				c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
				return c.SendString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Link expired</title></head><body style="font:16px system-ui;max-width:32rem;margin:4rem auto;padding:0 1rem"><h1>This link has expired</h1><p>The message is no longer available in the browser. Your inbox still has it.</p></body></html>`)
			}
			return writeErr(c, err)
		}
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "sandbox allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation; "+
			"default-src 'none'; img-src https: data:; style-src 'unsafe-inline' https:; font-src https: data:")
		if !strings.Contains(strings.ToLower(body), "<html") {
			body = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` +
				html.EscapeString(subject) + `</title></head><body>` + body + `</body></html>`
		}
		return c.SendString(body)
	})

	// ---- public one-click unsubscribe (RFC 8058). Bulk mail only.
	app.Get("/v1/unsubscribe/:token", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
		list, err := s.Svc.UnsubscribeScope(reqCtx(c), c.Params("token"))
		if err != nil {
			c.Status(404)
			return c.SendString(unsubscribePage("", false, false))
		}
		return c.SendString(unsubscribePage(list, false, false))
	})
	// The form posts scope=list or scope=all; a mail client's RFC 8058
	// one-click POST ("List-Unsubscribe=One-Click") means the list.
	app.Post("/v1/unsubscribe/:token", func(c fiber.Ctx) error {
		all := c.FormValue("scope") == "all"
		err := s.Svc.Unsubscribe(reqCtx(c), c.Params("token"), all)
		if err != nil {
			if e, ok := service.IsError(err); ok && e.Status == 404 {
				c.Status(404)
			} else {
				return writeErr(c, err)
			}
		}
		list := ""
		if !all {
			list, _ = s.Svc.UnsubscribeScope(reqCtx(c), c.Params("token"))
		}
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		return c.SendString(unsubscribePage(list, err == nil, all))
	})

	// ---- opt-in tracking: open pixel and click redirect (written campaigns)
	app.Get("/v1/t/o/:token", func(c fiber.Ctx) error {
		s.Svc.RecordOpen(reqCtx(c), c.Params("token"))
		c.Set(fiber.HeaderContentType, "image/gif")
		c.Set("Cache-Control", "no-store, private")
		c.Set("Referrer-Policy", "no-referrer")
		return c.Send(transparentGIF)
	})
	app.Get("/v1/t/c/:token", func(c fiber.Ctx) error {
		url, err := s.Svc.RecordClick(reqCtx(c), c.Params("token"))
		c.Set("Cache-Control", "no-store, private")
		c.Set("Referrer-Policy", "no-referrer")
		if err != nil {
			c.Status(404)
			c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
			c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			return c.SendString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Link not found</title></head><body style="font:16px system-ui;max-width:32rem;margin:4rem auto;padding:0 1rem"><h1>This link does not work</h1><p>It may have been copied incompletely.</p></body></html>`)
		}
		return c.Redirect().Status(302).To(url)
	})
}

// transparentGIF is a 1x1 transparent GIF.
var transparentGIF = []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b}

type sseEvent struct {
	Name, ID string
	Data     any
}

// eventStream answers with text/event-stream: next is polled for events
// until the client goes away or StreamMax passes. A comment line every 15
// seconds keeps proxies from closing an idle connection.
func (s *Server) eventStream(c fiber.Ctx, next func(context.Context) ([]sseEvent, error)) streamResponse {
	max, poll := s.StreamMax, s.StreamPoll
	if max <= 0 {
		max = 25 * time.Minute
	}
	if poll <= 0 {
		poll = 2 * time.Second
	}
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/event-stream")
		c.Set("Cache-Control", "no-store")
		c.Set("X-Accel-Buffering", "no") // nginx: pass events through at once
		return c.SendStreamWriter(func(w *bufio.Writer) {
			ctx, cancel := context.WithTimeout(context.Background(), max)
			defer cancel()
			fmt.Fprintf(w, "retry: 3000\n\n")
			if w.Flush() != nil {
				return
			}
			t := time.NewTicker(poll)
			defer t.Stop()
			idle := time.Now()
			for {
				evs, err := next(ctx)
				if err != nil {
					fmt.Fprintf(w, "event: error\ndata: {\"code\":\"stream_unavailable\"}\n\n")
					_ = w.Flush()
					return
				}
				for _, e := range evs {
					b, _ := json.Marshal(e.Data)
					fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Name, b)
				}
				if len(evs) == 0 && time.Since(idle) > 15*time.Second {
					fmt.Fprintf(w, ": keep-alive\n\n")
					idle = time.Now()
				}
				if w.Flush() != nil {
					return // the client went away
				}
				if len(evs) > 0 {
					idle = time.Now()
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		})
	}
}

// unsubscribePage confirms an unsubscribe. For a list campaign it offers the
// list (default) or all marketing mail; nothing changes without a click.
func unsubscribePage(list string, done, all bool) string {
	what := "these announcements"
	if list != "" {
		what = "the list “" + html.EscapeString(list) + "”"
	}
	body := `<form method="post"><input type="hidden" name="scope" value="list"><p>Stop receiving ` + what +
		` from this sender? Account and security messages are not affected.</p><button type="submit">Unsubscribe</button></form>`
	if list != "" {
		body += `<form method="post" style="margin-top:1rem"><input type="hidden" name="scope" value="all"><button type="submit" class="alt">Unsubscribe from all marketing mail</button></form>`
	}
	if done {
		if all {
			what = "all marketing mail from this sender"
		}
		body = `<p>You are unsubscribed from ` + what + `. Account and security messages are not affected.</p>`
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1f2937}button{font:inherit;padding:.6rem 1.2rem;border-radius:.5rem;border:0;background:#0f766e;color:#fff}button.alt{background:#fff;color:#0f766e;border:1px solid #0f766e}</style>
</head><body><h1>Unsubscribe</h1>` + body + `</body></html>`
}

func sandboxedHTML(html string) rawResponse {
	return rawResponse{body: []byte(html), header: map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:",
		"X-Frame-Options":         "SAMEORIGIN",
		"Referrer-Policy":         "no-referrer",
	}}
}

// decodeLarge is decode with a larger body cap (compose with attachments, CSV recipients).
func decodeLarge(c fiber.Ctx, v any) error {
	if len(c.Body()) > 12<<20 {
		return &service.Error{Status: 413, Code: "body_too_large"}
	}
	dec := jsonDecoder(c.Body())
	if err := dec.Decode(v); err != nil {
		return &service.Error{Status: 400, Code: "invalid_request", Message: "request body is not valid JSON for this endpoint"}
	}
	return nil
}

// ---------------------------------------------------------------- Mail server (Admin)

func (s *Server) mailServerRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path string, fn adminFn) { route(app, method, "/admin/v1"+path, s.adm(s.op(fn))) }

	h("GET", "/mail-server", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		m, err := svc.GetMailServer(reqCtx(c))
		if err != nil {
			return nil, err
		}
		m["environment_smtp"] = nil
		if s.EnvSMTP != nil {
			mode := "starttls"
			if s.EnvSMTP.Implicit {
				mode = "implicit"
			}
			m["environment_smtp"] = map[string]any{"host": s.EnvSMTP.Host, "port": s.EnvSMTP.Port, "username": s.EnvSMTP.Username, "tls_mode": mode}
		}
		return m, nil
	})
	h("PUT", "/mail-server", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in service.MailServerInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.SaveMailServer(reqCtx(c), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "mail_server.update", "mail_server", "1", map[string]any{"host": in.SMTPHost, "password_changed": in.Password != nil && *in.Password != "",
			"mailcow_api": in.MailcowAPIURL != ""})
		return svc.GetMailServer(reqCtx(c))
	})
	// cfgFor returns the settings a test will exercise and the profile revision
	// they belong to. The environment wins, as it does for the worker; its
	// result is attributed to the saved profile only when both are identical,
	// so a pass on environment SMTP never vouches for different saved settings.
	cfgFor := func(c fiber.Ctx) (mailer.SMTPConfig, string, service.TransportPin, error) {
		a, err := svc.LoadActiveSMTP(reqCtx(c))
		if err != nil {
			return a.Cfg, "", service.TransportPin{}, &service.Error{Status: 503, Code: "service_unavailable"}
		}
		cfg, ok, pin := a.Cfg, a.OK, a.Pin()
		if !a.OK {
			pin = service.TransportPin{}
		}
		if e := s.EnvSMTP; e != nil {
			if !(a.OK && e.Host == cfg.Host && e.Port == cfg.Port && e.Username == cfg.Username && e.Password == cfg.Password && e.Implicit == cfg.Implicit) {
				pin = service.TransportPin{}
			}
			cfg, ok = *e, true
		}
		if !ok {
			return cfg, "", pin, &service.Error{Status: 409, Code: "mail_server_not_configured", Message: "save the SMTP host, port, username and password first"}
		}
		return cfg, a.Sender, pin, nil
	}
	h("POST", "/mail-server/test", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		cfg, sender, pin, err := cfgFor(c)
		if err != nil {
			return nil, err
		}
		tctx, cancel := context.WithTimeout(reqCtx(c), 25*time.Second)
		defer cancel()
		steps := mailer.Diagnose(tctx, cfg, sender)
		ok := true
		for _, st := range steps {
			ok = ok && (st.OK || (st.Skipped && st.Key == "sender" && sender == ""))
		}
		_ = svc.RecordMailTest(reqCtx(c), pin, "connection", ok, steps)
		out := map[string]any{"ok": ok, "steps": steps}
		if mc := svc.MailcowConfigured(reqCtx(c)); mc {
			perr := svc.MailcowPing(tctx)
			out["mailcow_api"] = map[string]any{"ok": perr == nil, "detail": errText(perr)}
		}
		s.audit(c, u, "mail_server.test", "mail_server", "1", map[string]any{"ok": ok})
		return out, nil
	})
	h("POST", "/mail-server/test-send", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			To string `json:"to"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		cfg, sender, pin, err := cfgFor(c)
		if err != nil {
			return nil, err
		}
		out, err := svc.TestSendDirect(reqCtx(c), cfg, pin, sender, in.To)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "mail_server.test_send", "mail_server", "1", map[string]any{"state": out["state"]})
		return out, nil
	})
	h("GET", "/mail-server/status", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		m, err := svc.MailServerStatus(reqCtx(c))
		if err != nil {
			return nil, err
		}
		_, _, _, stored, _ := svc.StoredSMTP(reqCtx(c))
		m["outbound_source"] = map[bool]string{true: "environment", false: map[bool]string{true: "saved settings", false: "not configured"}[stored]}[s.EnvSMTP != nil]
		return m, nil
	})
	_ = http.MethodGet
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
