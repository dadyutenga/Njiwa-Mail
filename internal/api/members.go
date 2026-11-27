package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

// NeededRole is the weakest project role that may call a workspace route. rel is the route
// pattern after /admin/v1/w/:project/:mode.
//
//	viewer     reads, and the read-only checks that use POST (previews, address checks)
//	developer  every other change: send, templates, contacts, campaigns, replies
//	admin      keys, domains, senders, webhooks, mailboxes, suppressions, DNS, chat and form
//	           settings, and the personal-data tools
//	owner      members
func NeededRole(method, rel string) string {
	write := method != "GET" && method != "HEAD"
	under := func(p string) bool { return rel == p || strings.HasPrefix(rel, p+"/") }
	switch {
	case under("/members"):
		if write {
			return service.MemberOwner
		}
		return service.MemberViewer
	case under("/privacy"), under("/transport-test"), under("/dns/connections"), under("/dns/oauth"),
		method == "GET" && strings.HasSuffix(rel, "/export"):
		return service.MemberAdmin
	}
	if !write {
		return service.MemberViewer
	}
	switch rel {
	case "/recipients/check", "/recipients/verify", "/segments/preview", "/campaigns/check":
		return service.MemberViewer
	}
	if strings.HasSuffix(rel, "/preview") {
		return service.MemberViewer
	}
	// Settings of forms and chats are admin work; handling what they receive is developer work.
	if under("/forms/submissions") || under("/chat/conversations") {
		return service.MemberDeveloper
	}
	for _, p := range []string{"/keys", "/webhooks", "/senders", "/domains", "/dns-setups", "/mailboxes", "/suppressions",
		"/chat/widgets", "/forms", "/blocked-senders", "/receiving-domains"} {
		if under(p) {
			return service.MemberAdmin
		}
	}
	return service.MemberDeveloper
}

func (s *Server) memberRoutes(app *fiber.App) {
	svc := s.Svc
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { route(app, method, base+path, s.ws(fn)) }
	h("GET", "/members", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListMembers(reqCtx(c), sc)
		return map[string]any{"items": items, "your_role": sc.Role}, err
	})
	h("POST", "/members", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.AddMember(reqCtx(c), sc, u, sc.Role, in.Email, in.Role); err != nil {
			return nil, err
		}
		s.audit(c, u, "member.add", "project", sc.Project, scoped(sc, map[string]any{"role": in.Role}))
		return nil, nil
	})
	h("PATCH", "/members/:uid", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Role string `json:"role"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.SetMemberRole(reqCtx(c), sc, c.Params("uid"), in.Role); err != nil {
			return nil, err
		}
		s.audit(c, u, "member.role", "project", sc.Project, scoped(sc, map[string]any{"user": c.Params("uid"), "role": in.Role}))
		return nil, nil
	})
	h("DELETE", "/members/:uid", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.RemoveMember(reqCtx(c), sc, c.Params("uid")); err != nil {
			return nil, err
		}
		s.audit(c, u, "member.remove", "project", sc.Project, scoped(sc, map[string]any{"user": c.Params("uid")}))
		return nil, nil
	})
}
