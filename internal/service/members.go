package service

import (
	"context"
	"errors"

	"github.com/njiwa-tz/mail/internal/db"
)

// Project membership. A project is opened only by its members and by installation operators
// (who count as owners everywhere). The role decides what a member may do; the API layer maps
// each route to the weakest role that may call it.

const (
	MemberViewer    = "viewer"    // read everything in the project
	MemberDeveloper = "developer" // also send mail and edit content: templates, contacts, campaigns, replies
	MemberAdmin     = "admin"     // also keys, domains, senders, webhooks, settings and the personal-data tools
	MemberOwner     = "owner"     // also manages members
)

var roleRank = map[string]int{MemberViewer: 1, MemberDeveloper: 2, MemberAdmin: 3, MemberOwner: 4}

// RoleAtLeast reports whether have is at least as strong as need.
func RoleAtLeast(have, need string) bool {
	return roleRank[have] >= roleRank[need] && roleRank[have] > 0
}

func validMemberRole(r string) bool { return roleRank[r] > 0 }

// ProjectRole is the user's role in a project: "owner" for installation operators, the member
// role for members, "" for anyone else.
func (s *Service) ProjectRole(ctx context.Context, u *AdminUser, projectID string) (string, error) {
	if u.Operator {
		return MemberOwner, nil
	}
	var role string
	err := s.Pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, u.ID).Scan(&role)
	if errors.Is(err, db.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", unavailable("project_role", err)
	}
	return role, nil
}

// ProjectsOf lists the projects a user may open (all of them for operators) with their role.
// archived selects the archived projects instead of the working ones. One
// query counts what each project holds.
func (s *Service) ProjectsOf(ctx context.Context, u *AdminUser, archived bool, pg Page) ([]map[string]any, bool, error) {
	return s.page(ctx, pg, `SELECT p.id::text AS id, p.name, p.enabled, p.send_per_minute, p.send_per_day, p.recipient_per_minute, p.recipient_per_hour, p.created_at,
  CASE WHEN $1 THEN 'owner' ELSE m.role END AS role,
  p.name AS slug, p.archived_at, p.purge_after,
  (SELECT jsonb_build_object(
     'live_active', count(*) FILTER (WHERE e.mode='live' AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())),
     'live_revoked', count(*) FILTER (WHERE e.mode='live' AND NOT (k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now()))),
     'sandbox_active', count(*) FILTER (WHERE e.mode='sandbox' AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())),
     'sandbox_revoked', count(*) FILTER (WHERE e.mode='sandbox' AND NOT (k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now()))))
   FROM api_keys k JOIN environments e ON e.id=k.environment_id WHERE k.project_id=p.id) AS keys,
  (SELECT count(DISTINCT si.email) FROM sender_identities si WHERE si.project_id=p.id) AS senders,
  (SELECT count(DISTINCT d.name) FROM domains d WHERE d.project_id=p.id) AS domains,
  (SELECT count(*) FROM project_members pm WHERE pm.project_id=p.id) AS members,
  (SELECT count(*) FROM messages mm WHERE mm.project_id=p.id AND mm.created_at > now() - interval '30 days') AS messages_30d,
  (SELECT jsonb_agg(jsonb_build_object('mode', e.mode, 'enabled', e.enabled, 'approved_origins', to_jsonb(e.approved_origins),
     'restrict_recipients', e.restrict_recipients, 'recipient_allowlist', to_jsonb(e.recipient_allowlist)) ORDER BY e.mode)
   FROM environments e WHERE e.project_id=p.id) AS environments
FROM projects p LEFT JOIN project_members m ON m.project_id=p.id AND m.user_id=NULLIF($2,'')::uuid
WHERE ($1 OR m.user_id IS NOT NULL) AND (p.archived_at IS NOT NULL) = $3 ORDER BY p.name`, u.Operator, u.ID, archived)
}

// CanManageProject is true for operators and for owners and admins of the project.
func (s *Service) CanManageProject(ctx context.Context, u *AdminUser, projectName string) (bool, error) {
	var id string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM projects WHERE name=$1`, projectName).Scan(&id); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return false, newErr(404, "project_not_found")
		}
		return false, unavailable("project_role", err)
	}
	role, err := s.ProjectRole(ctx, u, id)
	if err != nil {
		return false, err
	}
	return RoleAtLeast(role, MemberAdmin), nil
}

// AddCreatorAsOwner makes a person who just created a project its owner. Operators need no row.
func (s *Service) AddCreatorAsOwner(ctx context.Context, u *AdminUser, projectName string) error {
	if u.Operator {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id, role, added_by)
SELECT id, $2, 'owner', $3 FROM projects WHERE name=$1 ON CONFLICT DO NOTHING`, projectName, u.ID, u.Email)
	if err != nil {
		return unavailable("project_members", err)
	}
	return nil
}

func (s *Service) ListMembers(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT u.id::text AS user_id, u.email, m.role, m.added_by, m.created_at, u.disabled
FROM project_members m JOIN users u ON u.id=m.user_id WHERE m.project_id=$1
ORDER BY (m.role='owner') DESC, u.email`, sc.ProjectID)
}

// AddMember adds an existing, enabled user to the project (creating users is an operator task).
// Only an owner may grant owner.
func (s *Service) AddMember(ctx context.Context, sc *Scope, actor *AdminUser, actorRole, email, role string) error {
	if !validMemberRole(role) {
		return badRequest("the role is viewer, developer, admin or owner")
	}
	if role == MemberOwner && actorRole != MemberOwner {
		return &Error{Status: 403, Code: "insufficient_role", Message: "only an owner can add an owner"}
	}
	e, ok := NormalizeEmail(email)
	if !ok {
		return badRequest("enter the email address of an existing user")
	}
	var uid string
	var disabled bool
	if err := s.Pool.QueryRow(ctx, `SELECT id::text, disabled FROM users WHERE email=$1`, e).Scan(&uid, &disabled); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return &Error{Status: 404, Code: "user_not_found", Message: "no user has this address; an operator creates users"}
		}
		return unavailable("project_members", err)
	}
	if disabled {
		return badRequest("this user is disabled")
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id, role, added_by) VALUES ($1,$2,$3,$4)
ON CONFLICT (project_id, user_id) DO UPDATE SET role=EXCLUDED.role`, sc.ProjectID, uid, role, actor.Email); err != nil {
		return unavailable("project_members", err)
	}
	return nil
}

// SetMemberRole changes a member's role. The last owner cannot be demoted.
func (s *Service) SetMemberRole(ctx context.Context, sc *Scope, userID, role string) error {
	if !validMemberRole(role) {
		return badRequest("the role is viewer, developer, admin or owner")
	}
	if !uuidRe.MatchString(userID) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var cur string
	if err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR UPDATE`, sc.ProjectID, userID).Scan(&cur); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return newErr(404, "not_found")
		}
		return unavailable("project_members", err)
	}
	if cur == MemberOwner && role != MemberOwner {
		if err := lastOwnerCheck(ctx, tx, sc.ProjectID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE project_members SET role=$3 WHERE project_id=$1 AND user_id=$2`, sc.ProjectID, userID, role); err != nil {
		return unavailable("project_members", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

// RemoveMember removes a member. The last owner cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, sc *Scope, userID string) error {
	if !uuidRe.MatchString(userID) {
		return newErr(404, "not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	var cur string
	if err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR UPDATE`, sc.ProjectID, userID).Scan(&cur); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return newErr(404, "not_found")
		}
		return unavailable("project_members", err)
	}
	if cur == MemberOwner {
		if err := lastOwnerCheck(ctx, tx, sc.ProjectID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, sc.ProjectID, userID); err != nil {
		return unavailable("project_members", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("commit", err)
	}
	return nil
}

func lastOwnerCheck(ctx context.Context, tx *db.Tx, projectID string) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id=$1 AND role='owner'`, projectID).Scan(&n); err != nil {
		return unavailable("project_members", err)
	}
	if n <= 1 {
		return &Error{Status: 409, Code: "last_owner", Message: "a project keeps at least one owner: make someone else an owner first"}
	}
	return nil
}
