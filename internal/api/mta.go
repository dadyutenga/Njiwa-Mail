package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/mtaagent"
	"github.com/njiwa-tz/mail/internal/service"
)

// MTAAgent is what the API needs from the host agent (E10).
type MTAAgent interface {
	Queue(ctx context.Context) ([]mtaagent.QueueEntry, error)
	Status(ctx context.Context) (*mtaagent.Status, error)
	Act(ctx context.Context, id string, a mtaagent.Action) error
	Flush(ctx context.Context) error
}

func mtaErr(err error) error {
	var ae *mtaagent.AgentError
	switch {
	case errors.Is(err, mtaagent.ErrUnreachable):
		return &service.Error{Status: 503, Code: "mta_agent_unreachable"}
	case errors.As(err, &ae) && ae.Status == 400:
		return &service.Error{Status: 404, Code: "not_found"}
	case errors.As(err, &ae) && ae.Status == 401:
		return &service.Error{Status: 502, Code: "mta_agent_rejected_token"}
	default:
		return &service.Error{Status: 502, Code: "mta_agent_failed"}
	}
}

// mtaRoutes: the Postfix queue as seen and controlled from the dashboard.
// Installation operators only; every action is audited; deleting needs the
// queue id typed back. Addresses are masked: the page shows what is stuck and
// why, not who was writing to whom.
func (s *Server) mtaRoutes(app *fiber.App) {
	svc := s.Svc
	h := func(method, path string, fn adminFn) {
		app.Add([]string{method}, "/admin/v1/mta"+path, s.adm(s.op(fn)))
	}
	agent := func() (MTAAgent, error) {
		if s.MTA == nil {
			return nil, &service.Error{Status: 409, Code: "mta_agent_not_configured"}
		}
		return s.MTA, nil
	}

	h("GET", "/status", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		if s.MTA == nil {
			return map[string]any{"configured": false}, nil
		}
		st, err := s.MTA.Status(reqCtx(c))
		if err != nil {
			return map[string]any{"configured": true, "reachable": false}, nil
		}
		return map[string]any{"configured": true, "reachable": true, "status": st}, nil
	})
	h("GET", "/queue", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		a, err := agent()
		if err != nil {
			return nil, err
		}
		q, err := a.Queue(reqCtx(c))
		if err != nil {
			return nil, mtaErr(err)
		}
		ids := make([]string, 0, len(q))
		for _, e := range q {
			ids = append(ids, e.QueueID)
		}
		known, err := svc.MessagesByQueueID(reqCtx(c), ids)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(q))
		for _, e := range q {
			rc := make([]map[string]any, 0, len(e.Recipients))
			for _, r := range e.Recipients {
				rc = append(rc, map[string]any{"address": service.MaskEmail(r.Address), "delay_reason": safeReason(r.DelayReason)})
			}
			it := map[string]any{"queue_id": e.QueueID, "queue": e.QueueName, "size": e.MessageSize,
				"arrived_at": time.Unix(e.ArrivalTime, 0).UTC(), "sender": service.MaskEmail(e.Sender), "recipients": rc}
			if m, ok := known[e.QueueID]; ok {
				it["message"] = m
			}
			items = append(items, it)
		}
		return map[string]any{"items": items}, nil
	})
	for _, act := range []mtaagent.Action{mtaagent.Hold, mtaagent.Release, mtaagent.Requeue} {
		act := act
		h("POST", "/queue/:id/"+string(act), func(c fiber.Ctx, u *service.AdminUser) (any, error) {
			return s.mtaAct(c, u, act)
		})
	}
	h("POST", "/queue/:id/delete", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		var in struct {
			Confirm string `json:"confirm"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		// Typed confirmation: the queue id itself. A stray click or a replayed request cannot delete mail.
		if !strings.EqualFold(strings.TrimSpace(in.Confirm), c.Params("id")) {
			return nil, &service.Error{Status: 422, Code: "confirmation_required", Message: "type the queue id to confirm"}
		}
		return s.mtaAct(c, u, mtaagent.Delete)
	})
	h("POST", "/flush", func(c fiber.Ctx, u *service.AdminUser) (any, error) {
		a, err := agent()
		if err != nil {
			return nil, err
		}
		if err := a.Flush(reqCtx(c)); err != nil {
			return nil, mtaErr(err)
		}
		s.audit(c, u, "mta.queue.flush", "mta_queue", "", nil)
		return nil, nil
	})
}

func (s *Server) mtaAct(c fiber.Ctx, u *service.AdminUser, act mtaagent.Action) (any, error) {
	if s.MTA == nil {
		return nil, &service.Error{Status: 409, Code: "mta_agent_not_configured"}
	}
	id := c.Params("id")
	if !mtaagent.ValidQueueID(id) {
		return nil, &service.Error{Status: 404, Code: "not_found"}
	}
	if err := s.MTA.Act(reqCtx(c), id, act); err != nil {
		return nil, mtaErr(err)
	}
	s.audit(c, u, "mta.queue."+string(act), "mta_queue", id, nil)
	_ = s.Svc.NoteQueueAction(reqCtx(c), id, string(act))
	return nil, nil
}

// safeReason keeps the reason a message waits (a DNS or connection problem)
// but drops anything that looks like an address.
func safeReason(r string) string {
	if len(r) > 200 {
		r = r[:200]
	}
	fields := strings.Fields(r)
	for i, f := range fields {
		if strings.Contains(f, "@") {
			fields[i] = "…"
		}
	}
	return strings.Join(fields, " ")
}
