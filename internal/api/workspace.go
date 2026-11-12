package api

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jhillyerd/enmime/v2"
	"github.com/njiwa-tz/mail/internal/mailer"
	"github.com/njiwa-tz/mail/internal/service"
)

// Workspace API: every route is under /admin/v1/w/{project}/{mode}. The scope
// is resolved from the path before the handler runs; an unknown project or
// mode is 404 project_not_found, and every query filters by that scope.

type wsFn func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error)

func (s *Server) ws(h wsFn) fiber.Handler {
	return s.adm(func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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

func boolQuery(c *fiber.Ctx, k string) bool {
	v := c.Query(k)
	return v == "1" || v == "true"
}

func (s *Server) workspaceRoutes(app *fiber.App) {
	svc := s.Svc
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { app.Add(method, base+path, s.ws(fn)) }
	ctx := reqCtx

	// ---- context, checklist, search, overview
	h("GET", "/context", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WorkspaceContext(ctx(c), sc)
	})
	h("GET", "/checklist", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/search", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.Search(ctx(c), sc, c.Query("q"))
		return map[string]any{"items": items}, err
	})
	h("GET", "/overview", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WorkspaceOverview(ctx(c), sc)
	})

	// ---- messages (Sent, Scheduled, Message log, Unknown queue)
	h("GET", "/messages", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/messages/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WSMessage(ctx(c), sc, c.Params("id"))
	})
	h("GET", "/messages/:id/export", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		m, err := svc.ExportMessage(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.export_message", "message", c.Params("id"), scoped(sc, nil))
		return rawResponse{header: map[string]string{"Content-Type": "application/json",
			"Content-Disposition": `attachment; filename="njiwa-mail-message-` + c.Params("id") + `.json"`}, body: mustJSON(m)}, nil
	})
	h("DELETE", "/messages/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteMessage(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "privacy.delete_message", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/messages/:id/copy", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.SentCopy(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/messages/:id/cancel", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSCancel(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "message.cancel", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/messages/:id/send-now", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.SendNow(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "message.send_now", "message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/messages/:id/retry", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		acc, err := svc.Retry(ctx(c), sc, c.Params("id"), u.Email)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "message.retry", "message", acc.ID, scoped(sc, map[string]any{"original": c.Params("id")}))
		return acc, nil
	})
	// F02: unknown messages with a suggested outcome from the evidence we hold.
	h("GET", "/unknown", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ReconciliationQueue(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/messages/:id/resolve", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/compose", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/transport-test", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/recipients/check", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/drafts", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListDraftsMail(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/drafts/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetDraftMail(ctx(c), sc, c.Params("id"))
	})
	saveDraft := func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/drafts/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.DeleteDraftMail(ctx(c), sc, c.Params("id"))
	})

	// ---- inbox and mailboxes
	h("GET", "/inbox", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.Inbox(ctx(c), sc, service.InboxFilter{Mailbox: c.Query("mailbox"), Query: c.Query("q"), Label: c.Query("label"),
			Unread: boolQuery(c, "unread"), Archived: boolQuery(c, "archived")}, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/inbox/unread", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.UnreadCounts(ctx(c), sc)
	})
	h("GET", "/inbox/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.OpenInbox(ctx(c), sc, c.Params("id"))
	})
	h("GET", "/inbox/:id/html", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		m, err := svc.GetInbound(ctx(c), sc.EnvID, c.Params("id"), true)
		if err != nil {
			return nil, err
		}
		html, _ := m["html"].(string)
		return sandboxedHTML(html), nil
	})
	h("GET", "/inbox/:id/attachments/:aid", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		f, err := svc.OpenAttachment(ctx(c), sc.EnvID, c.Params("id"), c.Params("aid"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.attachment_download", "inbound_attachment", c.Params("aid"), scoped(sc, nil))
		return download(f), nil
	})
	h("PATCH", "/inbox/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Read     *bool `json:"read"`
			Archived *bool `json:"archived"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return nil, svc.UpdateInbox(ctx(c), sc, c.Params("id"), in.Read, in.Archived)
	})
	h("DELETE", "/inbox/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteInbox(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.delete", "inbound_message", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/inbox/:id/block", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		sender, err := svc.BlockSender(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "inbound.block_sender", "blocked_sender", "", scoped(sc, nil))
		return map[string]string{"sender": sender}, nil
	})
	h("GET", "/blocked-senders", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListBlocked(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("DELETE", "/blocked-senders/:sender", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.Unblock(ctx(c), sc, c.Params("sender"))
	})
	h("GET", "/mailboxes", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListMailboxes(ctx(c), sc, false, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/receiving-domains", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ReceivingDomains(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/mailboxes", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("PATCH", "/mailboxes/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/mailboxes/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteMailbox(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "mailbox.delete", "inbound_address", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/temp-inboxes", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListMailboxes(ctx(c), sc, true, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/temp-inboxes", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/temp-inboxes/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return nil, svc.DeleteMailbox(ctx(c), sc, c.Params("id"))
	})

	// ---- domains
	h("GET", "/domains", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSDomains(ctx(c), sc, c.Query("q"), boolQuery(c, "attention"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/domains", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/domains/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.WSDomain(ctx(c), sc, c.Params("id"))
	})
	// Signed-mail evidence (D06): check a received message's DKIM signature.
	h("POST", "/domains/:id/verify-message", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/domains/:id/check", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/domains/:id/mailcow", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		steps, err := svc.ProvisionDomain(ctx(c), sc, c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.mailcow_provision", "domain", c.Params("id"), scoped(sc, nil))
		return map[string]any{"steps": steps}, nil
	})
	h("DELETE", "/domains/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSDeleteDomain(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "domain.delete", "domain", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})

	// ---- senders
	h("GET", "/senders", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSSenders(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/bulk-senders", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.BulkSenders(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/senders", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/senders/:id/default", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.SetDefaultSender(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.default", "sender", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/senders/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.WSDeleteSender(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "sender.delete", "sender", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})

	// ---- templates (this project's and shared ones)
	h("GET", "/templates", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSTemplates(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/templates", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
		return func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/templates/:id", tpl(false, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetTemplate(ctx(c), c.Params("id"))
	}))
	h("PATCH", "/templates/:id", tpl(true, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("PUT", "/templates/:id/draft", tpl(true, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/templates/:id/versions/:version/publish", tpl(true, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		v, _ := strconv.Atoi(c.Params("version"))
		if err := svc.PublishVersion(ctx(c), c.Params("id"), v); err != nil {
			return nil, err
		}
		s.audit(c, u, "template.publish", "template", c.Params("id"), scoped(sc, map[string]any{"version": v}))
		return nil, nil
	}))
	h("POST", "/templates/:id/versions/:version/preview", tpl(false, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/templates/:id/versions/:version/test-send", tpl(false, func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/templates/:id/duplicate", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/automations", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListRules(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/automations", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/automations/reorder", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("PATCH", "/automations/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/automations/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteRule(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "automation.delete", "automation_rule", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/automations/:id/test", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			InboundID string `json:"inbound_id"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.TestRule(ctx(c), sc, c.Params("id"), in.InboundID)
	})
	h("GET", "/automations/:id/runs", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.RuleRuns(ctx(c), sc, c.Params("id"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- bulk announcements (admin)
	h("GET", "/campaigns", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListCampaigns(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/campaigns/check", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Recipients string `json:"recipients"`
		}
		if err := decodeLarge(c, &in); err != nil {
			return nil, err
		}
		return svc.CheckCampaignRecipients(ctx(c), sc, in.Recipients)
	})
	h("POST", "/campaigns", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/campaigns/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetCampaign(ctx(c), sc, c.Params("id"))
	})
	h("POST", "/campaigns/:id/:action", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.CampaignAction(ctx(c), sc, c.Params("id"), c.Params("action")); err != nil {
			return nil, err
		}
		s.audit(c, u, "campaign."+c.Params("action"), "campaign", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/unsubscribes", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSUnsubscribes(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})

	// ---- suppressions, bounces, complaints
	h("GET", "/suppressions", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSSuppressions(ctx(c), sc, c.Query("source"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/suppressions", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/suppressions/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/bounces", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSBounces(ctx(c), sc, c.Query("type"), pageOf(c))
		return list(c, items, more, err)
	})

	// ---- developers: API keys, webhooks, sandbox
	h("GET", "/keys", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSKeys(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/keys", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/keys/:prefix/rotate", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/keys/:prefix/revoke", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.KeyInScope(ctx(c), sc, c.Params("prefix")); err != nil {
			return nil, err
		}
		if err := svc.RevokeKey(ctx(c), c.Params("prefix")); err != nil {
			return nil, service.OpErr(err)
		}
		s.audit(c, u, "api_key.revoke", "api_key", c.Params("prefix"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/webhooks", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSWebhooks(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/webhooks", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
		return func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
			if err := svc.InScope(ctx(c), sc, "webhook_endpoints", c.Params("id")); err != nil {
				return nil, err
			}
			return fn(c, u, sc)
		}
	}
	h("PATCH", "/webhooks/:id", inEndpoint(func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("DELETE", "/webhooks/:id", inEndpoint(func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteWebhook(ctx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.delete", "webhook_endpoint", c.Params("id"), scoped(sc, nil))
		return nil, nil
	}))
	h("POST", "/webhooks/:id/rotate-secret", inEndpoint(func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		secret, err := svc.RotateWebhookSecret(ctx(c), c.Params("id"))
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.rotate_secret", "webhook_endpoint", c.Params("id"), scoped(sc, nil))
		return map[string]string{"signing_secret": secret}, nil
	}))
	h("GET", "/webhook-deliveries", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSDeliveries(ctx(c), sc, c.Query("endpoint_id"), c.Query("state"), pageOf(c))
		return list(c, items, more, err)
	})
	h("POST", "/webhook-deliveries/:id/replay", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeliveryInScope(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		if err := svc.ReplayDelivery(ctx(c), c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "webhook.replay", "webhook_delivery", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/sandbox", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.WSCaptures(ctx(c), sc, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/sandbox/:id", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("GET", "/analytics", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.Analytics(ctx(c), sc.Project, sc.Mode, c.Query("template"), c.Query("domain"))
		return map[string]any{"items": items}, err
	})
	h("POST", "/privacy/export", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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
	h("POST", "/privacy/delete", func(c *fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
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

	// ---- public one-click unsubscribe (RFC 8058). Bulk mail only.
	app.Get("/v1/unsubscribe/:token", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
		return c.SendString(unsubscribePage(false))
	})
	app.Post("/v1/unsubscribe/:token", func(c *fiber.Ctx) error {
		err := s.Svc.Unsubscribe(reqCtx(c), c.Params("token"))
		if err != nil {
			if e, ok := service.IsError(err); ok && e.Status == 404 {
				c.Status(404)
			} else {
				return writeErr(c, err)
			}
		}
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		return c.SendString(unsubscribePage(err == nil))
	})
}

func unsubscribePage(done bool) string {
	body := `<form method="post"><p>Stop receiving announcements from this sender? Account and security messages are not affected.</p><button type="submit">Unsubscribe</button></form>`
	if done {
		body = `<p>You are unsubscribed from these announcements. Account and security messages are not affected.</p>`
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1f2937}button{font:inherit;padding:.6rem 1.2rem;border-radius:.5rem;border:0;background:#0f766e;color:#fff}</style>
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
func decodeLarge(c *fiber.Ctx, v any) error {
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
	h := func(method, path string, fn adminFn) { app.Add(method, "/admin/v1"+path, s.adm(s.op(fn))) }

	h("GET", "/mail-server", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h("PUT", "/mail-server", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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
	cfgFor := func(c *fiber.Ctx) (mailer.SMTPConfig, string, service.TransportPin, error) {
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
	h("POST", "/mail-server/test", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h("POST", "/mail-server/test-send", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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
	h("GET", "/mail-server/status", func(c *fiber.Ctx, u *service.AdminUser) (any, error) {
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
