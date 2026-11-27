package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

// projectOwner admits installation operators and the project's owners. Anyone
// else who is not a member gets the same 404 as for a project that does not exist.
func (s *Server) projectOwner(c fiber.Ctx, u *service.AdminUser) (string, error) {
	name := c.Params("name")
	role, err := s.Svc.ProjectRoleByName(reqCtx(c), u, name)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", &service.Error{Status: 404, Code: "project_not_found"}
	}
	if role != service.MemberOwner {
		return "", &service.Error{Status: 403, Code: "insufficient_role", Message: "only an owner of the project may do this"}
	}
	return name, nil
}

// projectLifecycleRoutes: rename, revoke all keys, archive, restore, delete.
func (s *Server) projectLifecycleRoutes(h func(string, adminFn)) {
	svc := s.Svc
	owner := func(fn func(c fiber.Ctx, u *service.AdminUser, name string) (any, error)) adminFn {
		return func(c fiber.Ctx, u *service.AdminUser) (any, error) {
			name, err := s.projectOwner(c, u)
			if err != nil {
				return nil, err
			}
			return fn(c, u, name)
		}
	}
	confirmation := func(c fiber.Ctx) (string, error) {
		var in struct {
			Confirm string `json:"confirm"`
		}
		err := decode(c, &in)
		return in.Confirm, err
	}
	h("GET /projects/{name}/removal", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		return svc.ProjectRemoval(reqCtx(c), name)
	}))
	h("POST /projects/{name}/rename", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		to, err := svc.RenameProject(reqCtx(c), name, in.Name)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "project.rename", "project", to, map[string]any{"from": name})
		return map[string]any{"name": to}, nil
	}))
	h("POST /projects/{name}/revoke-keys", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		n, err := svc.RevokeProjectKeys(reqCtx(c), name)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "project.revoke_keys", "project", name, map[string]any{"revoked": n})
		return map[string]any{"revoked": n}, nil
	}))
	h("POST /projects/{name}/archive", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		confirm, err := confirmation(c)
		if err != nil {
			return nil, err
		}
		until, err := svc.ArchiveProject(reqCtx(c), name, confirm)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "project.archive", "project", name, map[string]any{"purge_after": until})
		return map[string]any{"purge_after": until}, nil
	}))
	h("POST /projects/{name}/restore", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		if err := svc.RestoreProject(reqCtx(c), name); err != nil {
			return nil, err
		}
		s.audit(c, u, "project.restore", "project", name, nil)
		return nil, nil
	}))
	h("DELETE /projects/{name}", owner(func(c fiber.Ctx, u *service.AdminUser, name string) (any, error) {
		confirm, err := confirmation(c)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteProject(reqCtx(c), name, confirm); err != nil {
			return nil, err
		}
		s.audit(c, u, "project.delete", "project", name, nil)
		return nil, nil
	}))
}
