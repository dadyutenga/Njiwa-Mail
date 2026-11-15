package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Form extras: routing rules, tags and priority, spam marking, export and a summary.

var formPriorities = map[string]bool{"low": true, "normal": true, "high": true}

const (
	maxFormRules  = 50
	maxFormTags   = 10
	maxTagLen     = 30
	maxExportRows = 20000
)

// ---------------------------------------------------------------- rules

type FormRuleInput struct {
	Name        string   `json:"name"`
	Field       string   `json:"field"`
	Op          string   `json:"op"`
	Value       string   `json:"value"`
	SetPriority string   `json:"set_priority"`
	AddTags     []string `json:"add_tags"`
	AssignTo    string   `json:"assign_to"` // user id, "" for nobody
	Stop        bool     `json:"stop"`
	Enabled     *bool    `json:"enabled"`
}

type formRule struct {
	ID, Name, Field, Op, Value, SetPriority, AssignTo string
	AddTags                                           []string
	Stop                                              bool
}

func normalizeTags(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if len(t) > maxTagLen || strings.ContainsAny(t, " ,;\t\r\n") {
			return nil, badRequest("a tag is at most 30 characters, without spaces or commas")
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) > maxFormTags {
		return nil, badRequest("at most 10 tags")
	}
	return out, nil
}

func (in *FormRuleInput) check() error {
	in.Name = clean(strings.TrimSpace(in.Name), 80)
	if in.Name == "" {
		return badRequest("give the rule a name")
	}
	in.Field = strings.TrimSpace(in.Field)
	if in.Field != "" && !formKeyRe.MatchString(in.Field) {
		return badRequest("a field name is letters, digits, _ . -")
	}
	switch in.Op {
	case "contains", "equals", "regex", "domain":
		if strings.TrimSpace(in.Value) == "" || len(in.Value) > 200 {
			return badRequest("enter a value (up to 200 characters)")
		}
		if in.Op == "regex" {
			if _, err := regexp.Compile(in.Value); err != nil {
				return badRequest("the regular expression is not valid: " + clean(err.Error(), 120))
			}
		}
		if in.Op == "domain" {
			in.Value = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(in.Value), "@"))
		}
	case "exists":
		if in.Field == "" {
			return badRequest("\"is filled in\" needs a field name")
		}
	default:
		return badRequest("the condition is contains, equals, regex, domain or exists")
	}
	if in.SetPriority != "" && !formPriorities[in.SetPriority] {
		return badRequest("the priority is low, normal or high")
	}
	tags, err := normalizeTags(in.AddTags)
	if err != nil {
		return err
	}
	in.AddTags = tags
	if in.AssignTo != "" && !uuidRe.MatchString(in.AssignTo) {
		return badRequest("unknown user")
	}
	if in.SetPriority == "" && len(in.AddTags) == 0 && in.AssignTo == "" {
		return badRequest("a rule needs something to do: set a priority, add tags or assign it")
	}
	return nil
}

func (s *Service) formInEnv(ctx context.Context, sc *Scope, formID string) error {
	if !uuidRe.MatchString(formID) {
		return errFormFound
	}
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM forms WHERE id=$1 AND environment_id=$2)`, formID, sc.EnvID).Scan(&ok); err != nil {
		return unavailable("form", err)
	}
	if !ok {
		return errFormFound
	}
	return nil
}

func (s *Service) ListFormRules(ctx context.Context, sc *Scope, formID string) ([]map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return nil, err
	}
	return s.maps(ctx, `SELECT r.id::text AS id, r.position, r.name, r.field, r.op, r.value, r.set_priority, r.add_tags,
  COALESCE(r.assign_to::text,'') AS assign_to, COALESCE(u.email,'') AS assign_to_email, r.stop, r.enabled
FROM form_rules r LEFT JOIN users u ON u.id=r.assign_to WHERE r.form_id=$1 ORDER BY r.position, r.created_at`, formID)
}

func (s *Service) checkAssignee(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND NOT disabled)`, id).Scan(&ok); err != nil || !ok {
		return badRequest("unknown user")
	}
	return nil
}

func (s *Service) CreateFormRule(ctx context.Context, sc *Scope, formID string, in FormRuleInput) (string, error) {
	if err := s.formsOn(); err != nil {
		return "", err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return "", err
	}
	if err := in.check(); err != nil {
		return "", err
	}
	if err := s.checkAssignee(ctx, in.AssignTo); err != nil {
		return "", err
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM form_rules WHERE form_id=$1`, formID).Scan(&n); err != nil {
		return "", unavailable("form_rules", err)
	}
	if n >= maxFormRules {
		return "", badRequest("at most 50 rules per form")
	}
	enabled := in.Enabled == nil || *in.Enabled
	id := secure.NewUUID()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO form_rules(id, form_id, position, name, field, op, value, set_priority, add_tags, assign_to, stop, enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid,$11,$12)`, id, formID, n, in.Name, in.Field, in.Op, in.Value, in.SetPriority, in.AddTags, in.AssignTo, in.Stop, enabled); err != nil {
		return "", unavailable("form_rules", err)
	}
	return id, nil
}

func (s *Service) UpdateFormRule(ctx context.Context, sc *Scope, formID, ruleID string, in FormRuleInput) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return err
	}
	if !uuidRe.MatchString(ruleID) {
		return errFormFound
	}
	if err := in.check(); err != nil {
		return err
	}
	if err := s.checkAssignee(ctx, in.AssignTo); err != nil {
		return err
	}
	enabled := in.Enabled == nil || *in.Enabled
	tag, err := s.Pool.Exec(ctx, `UPDATE form_rules SET name=$3, field=$4, op=$5, value=$6, set_priority=$7, add_tags=$8, assign_to=NULLIF($9,'')::uuid, stop=$10, enabled=$11
WHERE id=$1 AND form_id=$2`, ruleID, formID, in.Name, in.Field, in.Op, in.Value, in.SetPriority, in.AddTags, in.AssignTo, in.Stop, enabled)
	if err != nil {
		return unavailable("form_rules", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

func (s *Service) DeleteFormRule(ctx context.Context, sc *Scope, formID, ruleID string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return err
	}
	if !uuidRe.MatchString(ruleID) {
		return errFormFound
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM form_rules WHERE id=$1 AND form_id=$2`, ruleID, formID)
	if err != nil {
		return unavailable("form_rules", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

// ReorderFormRules sets the order rules are tried in. ids must be exactly the form's rules.
func (s *Service) ReorderFormRules(ctx context.Context, sc *Scope, formID string, ids []string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id::text FROM form_rules WHERE form_id=$1 FOR UPDATE`, formID)
	if err != nil {
		return unavailable("form_rules", err)
	}
	have, err := db.CollectStrings(rows)
	if err != nil {
		return unavailable("form_rules", err)
	}
	a, b := append([]string{}, have...), append([]string{}, ids...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) || strings.Join(a, ",") != strings.Join(b, ",") {
		return badRequest("list every rule of the form exactly once")
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE form_rules SET position=$3 WHERE id=$1 AND form_id=$2`, id, formID, i); err != nil {
			return unavailable("form_rules", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) loadFormRules(ctx context.Context, formID string) ([]formRule, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text, name, field, op, value, set_priority, add_tags, COALESCE(assign_to::text,''), stop
FROM form_rules WHERE form_id=$1 AND enabled ORDER BY position, created_at`, formID)
	if err != nil {
		return nil, unavailable("form_rules", err)
	}
	defer rows.Close()
	var out []formRule
	for rows.Next() {
		var r formRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Field, &r.Op, &r.Value, &r.SetPriority, &r.AddTags, &r.AssignTo, &r.Stop); err != nil {
			return nil, unavailable("form_rules", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (r formRule) matches(fields map[string]string) bool {
	values := func() []string {
		if r.Field != "" {
			return []string{fields[r.Field]}
		}
		out := make([]string, 0, len(fields))
		for _, v := range fields {
			out = append(out, v)
		}
		return out
	}()
	switch r.Op {
	case "exists":
		return strings.TrimSpace(fields[r.Field]) != ""
	case "domain":
		e := strings.ToLower(fields["email"])
		if r.Field != "" {
			e = strings.ToLower(fields[r.Field])
		}
		at := strings.LastIndexByte(e, '@')
		return at > 0 && (e[at+1:] == r.Value || strings.HasSuffix(e[at+1:], "."+r.Value))
	}
	for _, v := range values {
		switch r.Op {
		case "contains":
			if strings.Contains(strings.ToLower(v), strings.ToLower(r.Value)) {
				return true
			}
		case "equals":
			if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(r.Value)) {
				return true
			}
		case "regex":
			if re, err := regexp.Compile(r.Value); err == nil && re.MatchString(v) {
				return true
			}
		}
	}
	return false
}

// RuleOutcome is what the matching rules decide: the priority (last one wins), the union of tags,
// the assignee (first rule that names one wins) and which rules matched.
type RuleOutcome struct {
	Priority string   `json:"priority"`
	Tags     []string `json:"tags"`
	Assignee string   `json:"assignee"`
	Matched  []string `json:"matched"`
}

func applyFormRules(rules []formRule, fields map[string]string) RuleOutcome {
	out := RuleOutcome{Priority: "normal", Tags: []string{}, Matched: []string{}}
	seen := map[string]bool{}
	for _, r := range rules {
		if !r.matches(fields) {
			continue
		}
		out.Matched = append(out.Matched, r.Name)
		if r.SetPriority != "" {
			out.Priority = r.SetPriority
		}
		for _, t := range r.AddTags {
			if !seen[t] && len(out.Tags) < maxFormTags {
				seen[t] = true
				out.Tags = append(out.Tags, t)
			}
		}
		if out.Assignee == "" && r.AssignTo != "" {
			out.Assignee = r.AssignTo
		}
		if r.Stop {
			break
		}
	}
	return out
}

// TestFormRules shows what the form's rules would do to a submission with these fields.
func (s *Service) TestFormRules(ctx context.Context, sc *Scope, formID string, fields map[string]string) (RuleOutcome, error) {
	if err := s.formsOn(); err != nil {
		return RuleOutcome{}, err
	}
	if err := s.formInEnv(ctx, sc, formID); err != nil {
		return RuleOutcome{}, err
	}
	rules, err := s.loadFormRules(ctx, formID)
	if err != nil {
		return RuleOutcome{}, err
	}
	return applyFormRules(rules, fields), nil
}

// ---------------------------------------------------------------- tags, priority, spam marking

// SetFormSubmissionLabels sets the tags and/or the priority of a submission.
func (s *Service) SetFormSubmissionLabels(ctx context.Context, sc *Scope, id string, tags *[]string, priority *string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	if priority != nil && !formPriorities[*priority] {
		return badRequest("the priority is low, normal or high")
	}
	var norm []string
	if tags != nil {
		var err error
		if norm, err = normalizeTags(*tags); err != nil {
			return err
		}
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE form_submissions SET tags=CASE WHEN $3::boolean THEN $4::text[] ELSE tags END, priority=COALESCE($5, priority)
WHERE id=$1 AND environment_id=$2`, id, sc.EnvID, tags != nil, norm, priority)
	if err != nil {
		return unavailable("form_submission", err)
	}
	if tag.RowsAffected() == 0 {
		return errFormFound
	}
	return nil
}

// MarkFormSpam files a submission as spam. With blockSender the sender's address also becomes a
// workspace filter that quarantines their next messages.
func (s *Service) MarkFormSpam(ctx context.Context, sc *Scope, id string, blockSender bool) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	fields, err := s.submissionFieldsInEnv(ctx, sc, id)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE form_submissions SET state='spam' WHERE id=$1 AND environment_id=$2`, id, sc.EnvID); err != nil {
		return unavailable("form_submission", err)
	}
	if blockSender {
		if e, ok := NormalizeEmail(fields["email"]); ok {
			if _, err := s.Pool.Exec(ctx, `INSERT INTO form_filters(project_id, environment_id, kind, pattern, action)
SELECT $1,$2,'email',$3,'quarantine' WHERE NOT EXISTS (SELECT 1 FROM form_filters WHERE environment_id=$2 AND kind='email' AND pattern=$3)`, sc.ProjectID, sc.EnvID, e); err != nil {
				return unavailable("form_filters", err)
			}
		}
	}
	return nil
}

// MarkFormNotSpam brings a submission back to the inbox, and removes the filter that blocks the
// sender's address, if one exists.
func (s *Service) MarkFormNotSpam(ctx context.Context, sc *Scope, id string) error {
	if err := s.formsOn(); err != nil {
		return err
	}
	if !uuidRe.MatchString(id) {
		return errFormFound
	}
	fields, err := s.submissionFieldsInEnv(ctx, sc, id)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE form_submissions SET state='new' WHERE id=$1 AND environment_id=$2 AND state='spam'`, id, sc.EnvID); err != nil {
		return unavailable("form_submission", err)
	}
	if e, ok := NormalizeEmail(fields["email"]); ok {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM form_filters WHERE environment_id=$1 AND kind='email' AND pattern=$2`, sc.EnvID, e); err != nil {
			return unavailable("form_filters", err)
		}
	}
	return nil
}

func (s *Service) submissionFieldsInEnv(ctx context.Context, sc *Scope, id string) (map[string]string, error) {
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM form_submissions WHERE id=$1 AND environment_id=$2)`, id, sc.EnvID).Scan(&ok); err != nil {
		return nil, unavailable("form_submission", err)
	}
	if !ok {
		return nil, errFormFound
	}
	f, err := s.submissionFields(ctx, id)
	if err != nil {
		return nil, unavailable("form_submission", err)
	}
	return f, nil
}

// ---------------------------------------------------------------- summary and export

// FormsSummary counts submissions: by state, by form, per day, and how many are unread or flagged.
func (s *Service) FormsSummary(ctx context.Context, sc *Scope, days int) (map[string]any, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if days < 1 || days > 365 {
		days = 30
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	tot, err := s.one(ctx, `SELECT count(*) AS submissions,
  count(*) FILTER (WHERE state='new' AND action<>'quarantine') AS unread,
  count(*) FILTER (WHERE state='spam') AS spam,
  count(*) FILTER (WHERE action='flag') AS flagged,
  count(*) FILTER (WHERE priority='high' AND state NOT IN ('closed','spam')) AS high_priority_open,
  count(*) FILTER (WHERE state='replied') AS replied
FROM form_submissions WHERE environment_id=$1 AND received_at >= $2`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	byForm, err := s.maps(ctx, `SELECT f.name AS form, count(*) AS submissions, count(*) FILTER (WHERE x.state='spam') AS spam
FROM form_submissions x JOIN forms f ON f.id=x.form_id WHERE x.environment_id=$1 AND x.received_at >= $2 GROUP BY f.name ORDER BY 2 DESC, 1`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	daily, err := s.maps(ctx, `SELECT to_char(date_trunc('day', received_at), 'YYYY-MM-DD') AS day, count(*) AS submissions
FROM form_submissions WHERE environment_id=$1 AND received_at >= $2 GROUP BY 1 ORDER BY 1`, sc.EnvID, since)
	if err != nil {
		return nil, err
	}
	return map[string]any{"days": days, "from": since, "totals": tot, "by_form": byForm, "daily": daily}, nil
}

// ExportFormSubmissions is a CSV of submissions (newest first, at most 20,000): the fields are
// decrypted, so who may call it matters. Cells that start like a spreadsheet formula are protected.
func (s *Service) ExportFormSubmissions(ctx context.Context, sc *Scope, f FormSubmissionFilter) ([]byte, error) {
	if err := s.formsOn(); err != nil {
		return nil, err
	}
	if f.FormID != "" && !uuidRe.MatchString(f.FormID) {
		return nil, errFormFound
	}
	rows, err := s.maps(ctx, `SELECT x.id::text AS id, f.name AS form, x.received_at, x.state, x.priority, x.tags, x.spam_score, x.action, u.email AS assigned_to
FROM form_submissions x JOIN forms f ON f.id=x.form_id LEFT JOIN users u ON u.id=x.assigned_to
WHERE x.environment_id=$1 AND ($2='' OR x.form_id=NULLIF($2,'')::uuid) AND (($3='' AND x.state<>'spam') OR ($3<>'' AND x.state=$3)) AND ($4='' OR $4=ANY(x.tags)) AND ($5='' OR x.priority=$5) ORDER BY x.received_at DESC LIMIT $6`, sc.EnvID, f.FormID, f.State, strings.ToLower(f.Tag), f.Priority, maxExportRows)
	if err != nil {
		return nil, err
	}
	fieldSet := map[string]bool{}
	all := make([]map[string]string, len(rows))
	for i, r := range rows {
		fields, _ := s.submissionFields(ctx, fmt.Sprint(r["id"]))
		all[i] = fields
		for k := range fields {
			fieldSet[k] = true
		}
	}
	names := make([]string, 0, len(fieldSet))
	for k := range fieldSet {
		names = append(names, k)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	head := append([]string{"id", "form", "received_at", "state", "priority", "tags", "spam_score", "action", "assigned_to"}, names...)
	_ = w.Write(head)
	cell := func(v any) string {
		switch t := v.(type) {
		case nil:
			return ""
		case time.Time:
			return t.UTC().Format(time.RFC3339)
		case []string:
			return csvSafe(strings.Join(t, " "))
		default:
			return csvSafe(fmt.Sprint(t))
		}
	}
	for i, r := range rows {
		line := []string{cell(r["id"]), cell(r["form"]), cell(r["received_at"]), cell(r["state"]), cell(r["priority"]), cell(r["tags"]), cell(r["spam_score"]), cell(r["action"]), cell(r["assigned_to"])}
		for _, k := range names {
			line = append(line, csvSafe(all[i][k]))
		}
		_ = w.Write(line)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
