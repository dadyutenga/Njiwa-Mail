package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// UsagePlanInput is a usage plan's limits. 0 for monthly_messages, max_contacts
// and the bulk limits means unlimited (bulk: none allowed when 0 per day).
type UsagePlanInput struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	SendPerMinute      int    `json:"send_per_minute"`
	SendPerDay         int    `json:"send_per_day"`
	RecipientPerMinute int    `json:"recipient_per_minute"`
	RecipientPerHour   int    `json:"recipient_per_hour"`
	BulkMaxPerCampaign int    `json:"bulk_max_per_campaign"`
	BulkMaxPerDay      int    `json:"bulk_max_per_day"`
	MonthlyMessages    int    `json:"monthly_messages"`
	MaxContacts        int    `json:"max_contacts"`
}

func (in *UsagePlanInput) check() error {
	in.Name = clean(strings.TrimSpace(in.Name), 80)
	if in.Name == "" {
		return badRequest("give the plan a name")
	}
	for _, v := range []int{in.SendPerMinute, in.SendPerDay, in.RecipientPerMinute, in.RecipientPerHour} {
		if v < 1 || v > 10_000_000 {
			return badRequest("sending limits must be between 1 and 10,000,000")
		}
	}
	for _, v := range []int{in.BulkMaxPerCampaign, in.BulkMaxPerDay, in.MonthlyMessages, in.MaxContacts} {
		if v < 0 || v > 1_000_000_000 {
			return badRequest("limits cannot be negative")
		}
	}
	return nil
}

const planCols = `name, description, send_per_minute, send_per_day, recipient_per_minute, recipient_per_hour,
  bulk_max_per_campaign, bulk_max_per_day, monthly_messages, max_contacts`

func planArgs(in UsagePlanInput) []any {
	return []any{in.Name, clean(in.Description, 500), in.SendPerMinute, in.SendPerDay, in.RecipientPerMinute, in.RecipientPerHour,
		in.BulkMaxPerCampaign, in.BulkMaxPerDay, in.MonthlyMessages, in.MaxContacts}
}

func (s *Service) CreatePlan(ctx context.Context, in UsagePlanInput) (string, error) {
	if err := in.check(); err != nil {
		return "", err
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO usage_plans(`+planCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (name) DO NOTHING RETURNING id::text`, planArgs(in)...).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "plan_exists", Message: "a plan with this name exists"}
	}
	if err != nil {
		return "", unavailable("plan", err)
	}
	return id, nil
}

// UpdatePlan changes a plan and applies it again to every project on it.
func (s *Service) UpdatePlan(ctx context.Context, id string, in UsagePlanInput) (int, error) {
	if !uuidRe.MatchString(id) {
		return 0, newErr(404, "not_found")
	}
	if err := in.check(); err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE usage_plans SET (`+planCols+`) = ($2,$3,$4,$5,$6,$7,$8,$9,$10,$11) WHERE id=$1`, append([]any{id}, planArgs(in)...)...)
	if err != nil {
		if strings.Contains(err.Error(), "usage_plans_name_key") {
			return 0, &Error{Status: 409, Code: "plan_exists", Message: "a plan with this name exists"}
		}
		return 0, unavailable("plan", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, newErr(404, "not_found")
	}
	n, err := applyPlanTx(ctx, tx, id, "")
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// applyPlanTx copies a plan's limits onto its projects (or one project).
func applyPlanTx(ctx context.Context, tx *db.Tx, planID, project string) (int, error) {
	tag, err := tx.Exec(ctx, `UPDATE projects p SET plan_id=u.id, send_per_minute=u.send_per_minute, send_per_day=u.send_per_day,
  recipient_per_minute=u.recipient_per_minute, recipient_per_hour=u.recipient_per_hour,
  bulk_max_per_campaign=u.bulk_max_per_campaign, bulk_max_per_day=u.bulk_max_per_day,
  monthly_messages=u.monthly_messages, max_contacts=u.max_contacts
FROM usage_plans u WHERE u.id=$1 AND (($2='' AND p.plan_id=u.id) OR p.name=$2)`, planID, project)
	if err != nil {
		return 0, unavailable("plan_apply", err)
	}
	return int(tag.RowsAffected()), nil
}

// AssignPlan puts a project on a plan, taking its limits now.
func (s *Service) AssignPlan(ctx context.Context, project, planID string) error {
	if !uuidRe.MatchString(planID) {
		return newErr(404, "plan_not_found")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	n, err := applyPlanTx(ctx, tx, planID, project)
	if err != nil {
		return err
	}
	if n == 0 {
		return newErr(404, "not_found")
	}
	return tx.Commit(ctx)
}

func (s *Service) DeletePlan(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	var used int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM projects WHERE plan_id=$1`, id).Scan(&used); err != nil {
		return unavailable("plan", err)
	}
	if used > 0 {
		return &Error{Status: 409, Code: "plan_in_use", Message: "move its projects to another plan first"}
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM usage_plans WHERE id=$1`, id)
	if err != nil {
		return unavailable("plan", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) ListPlans(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT u.id::text AS id, `+planCols+`, u.created_at,
  (SELECT count(*) FROM projects p WHERE p.plan_id=u.id) AS projects
FROM usage_plans u ORDER BY u.name`)
}

// Usage is each project's plan, this month's Live messages against its
// monthly quota, and its contacts against the contact limit.
func (s *Service) Usage(ctx context.Context) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT p.name AS project, u.name AS plan, p.monthly_messages, p.max_contacts,
  coalesce((SELECT m.messages FROM monthly_usage m WHERE m.project_id=p.id AND m.month=date_trunc('month', now() AT TIME ZONE 'UTC')::date), 0) AS messages_this_month,
  (SELECT count(*) FROM contacts c WHERE c.project_id=p.id) AS contacts
FROM projects p LEFT JOIN usage_plans u ON u.id=p.plan_id ORDER BY p.name`)
}

// chargeMonthly counts one Live message against the project's monthly
// quota inside the accept transaction; over the quota it refuses until the
// next month (UTC).
func chargeMonthly(ctx context.Context, tx *db.Tx, projectID string) error {
	var limit int
	if err := tx.QueryRow(ctx, `SELECT monthly_messages FROM projects WHERE id=$1`, projectID).Scan(&limit); err != nil {
		return unavailable("monthly_quota", err)
	}
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var n int
	if err := tx.QueryRow(ctx, `INSERT INTO monthly_usage(project_id, month, messages) VALUES ($1,$2,1)
ON CONFLICT (project_id, month) DO UPDATE SET messages=monthly_usage.messages+1 RETURNING messages`, projectID, month).Scan(&n); err != nil {
		return unavailable("monthly_quota", err)
	}
	if limit > 0 && n > limit {
		return &Error{Status: 429, Code: "monthly_quota_exceeded", RetryAfter: month.AddDate(0, 1, 0).Sub(now),
			Message: "this project's monthly message quota is used up"}
	}
	return nil
}

// checkContactLimitTx refuses a new contact over the project's limit.
func checkContactLimitTx(ctx context.Context, tx *db.Tx, projectID string) error {
	var limit, n int
	if err := tx.QueryRow(ctx, `SELECT max_contacts, (SELECT count(*) FROM contacts WHERE project_id=$1) FROM projects WHERE id=$1`, projectID).Scan(&limit, &n); err != nil {
		return unavailable("contact_limit", err)
	}
	if limit > 0 && n >= limit {
		return &Error{Status: 422, Code: "contact_limit_reached", Message: "this project's plan allows no more contacts"}
	}
	return nil
}
