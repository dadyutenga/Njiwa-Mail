package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/db"
)

// SegmentRules describe a dynamic audience. Conditions:
//
//	status     eq|neq        active | unsubscribed | bounced | complained
//	list       in|not_in     a list id (an unsubscribe from that list counts as not in it)
//	domain     eq|neq        the address's domain, e.g. gmail.com
//	attribute  eq|neq|contains|exists|not_exists   key = attribute name
//	created    before|after  RFC 3339 time
type SegmentRules struct {
	Match      string             `json:"match"` // all | any
	Conditions []SegmentCondition `json:"conditions"`
}

type SegmentCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// segmentSQL turns rules into a WHERE fragment over contacts c. Every value
// is a bind parameter; only fixed SQL is concatenated. args continue from
// first.
func segmentSQL(r SegmentRules, first int) (string, []any, error) {
	if len(r.Conditions) == 0 {
		return "", nil, badRequest("add at least one condition")
	}
	if len(r.Conditions) > 20 {
		return "", nil, badRequest("at most 20 conditions")
	}
	join := " AND "
	switch r.Match {
	case "", "all":
	case "any":
		join = " OR "
	default:
		return "", nil, badRequest("match must be all or any")
	}
	var parts []string
	var args []any
	p := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", first+len(args)-1)
	}
	for _, c := range r.Conditions {
		var part string
		switch c.Field {
		case "status":
			switch c.Value {
			case "active", "unsubscribed", "bounced", "complained":
			default:
				return "", nil, badRequest("status is active, unsubscribed, bounced or complained")
			}
			switch c.Op {
			case "eq":
				part = "(" + contactStatusSQL + ") = " + p(c.Value)
			case "neq":
				part = "(" + contactStatusSQL + ") <> " + p(c.Value)
			}
		case "list":
			if !uuidRe.MatchString(c.Value) {
				return "", nil, badRequest("choose a list")
			}
			in := "EXISTS (SELECT 1 FROM contact_list_members m WHERE m.contact_id=c.id AND m.list_id=" + p(c.Value) + "::uuid AND m.unsubscribed_at IS NULL)"
			switch c.Op {
			case "in":
				part = in
			case "not_in":
				part = "NOT " + in
			}
		case "domain":
			v := strings.ToLower(strings.TrimSpace(c.Value))
			if v == "" {
				return "", nil, badRequest("enter a domain")
			}
			switch c.Op {
			case "eq":
				part = "c.email_domain = " + p(v)
			case "neq":
				part = "c.email_domain <> " + p(v)
			}
		case "attribute":
			if !attrKeyRe.MatchString(c.Key) {
				return "", nil, badRequest("attribute names are lower case letters, digits and _")
			}
			k := p(c.Key)
			switch c.Op {
			case "eq":
				part = "c.attributes->>" + k + " = " + p(c.Value)
			case "neq":
				part = "coalesce(c.attributes->>" + k + ",'') <> " + p(c.Value)
			case "contains":
				part = "position(lower(" + p(c.Value) + ") in lower(c.attributes->>" + k + ")) > 0"
			case "exists":
				part = "c.attributes ? " + k
			case "not_exists":
				part = "NOT (c.attributes ? " + k + ")"
			}
		case "created":
			t, err := time.Parse(time.RFC3339, c.Value)
			if err != nil {
				return "", nil, badRequest("created needs a date and time (RFC 3339)")
			}
			switch c.Op {
			case "before":
				part = "c.created_at < " + p(t)
			case "after":
				part = "c.created_at > " + p(t)
			}
		default:
			return "", nil, badRequest("unknown condition " + clean(c.Field, 40))
		}
		if part == "" {
			return "", nil, badRequest("unknown operator " + clean(c.Op, 20) + " for " + c.Field)
		}
		parts = append(parts, "("+part+")")
	}
	return "(" + strings.Join(parts, join) + ")", args, nil
}

// SegmentPreview counts the contacts that match, and how many of them can
// receive marketing mail now (status active).
func (s *Service) SegmentPreview(ctx context.Context, sc *Scope, r SegmentRules) (map[string]any, error) {
	where, args, err := segmentSQL(r, 2)
	if err != nil {
		return nil, err
	}
	var total, sendable int64
	err = s.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE (`+contactStatusSQL+`)='active')
FROM contacts c WHERE c.environment_id=$1 AND `+where, append([]any{sc.EnvID}, args...)...).Scan(&total, &sendable)
	if err != nil {
		return nil, unavailable("segment_preview", err)
	}
	sample, err := s.maps(ctx, `SELECT c.email_masked, c.email_domain, `+contactStatusSQL+` AS status FROM contacts c
WHERE c.environment_id=$1 AND `+where+` ORDER BY c.created_at DESC LIMIT 5`, append([]any{sc.EnvID}, args...)...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"matches": total, "sendable": sendable, "sample": sample}, nil
}

func (s *Service) CreateSegment(ctx context.Context, sc *Scope, name string, r SegmentRules) (string, error) {
	name = clean(strings.TrimSpace(name), 120)
	if name == "" {
		return "", badRequest("give the segment a name")
	}
	if _, _, err := segmentSQL(r, 2); err != nil {
		return "", err
	}
	rj, _ := json.Marshal(r)
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO contact_segments(project_id, environment_id, name, rules) VALUES ($1,$2,$3,$4)
ON CONFLICT (environment_id, name) DO NOTHING RETURNING id::text`, sc.ProjectID, sc.EnvID, name, rj).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "segment_exists", Message: "a segment with this name exists"}
	}
	if err != nil {
		return "", unavailable("segment_create", err)
	}
	return id, nil
}

func (s *Service) ListSegments(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT id::text AS id, name, rules, created_at FROM contact_segments WHERE environment_id=$1 ORDER BY name`, sc.EnvID)
}

func (s *Service) DeleteSegment(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM contact_segments WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("segment_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

func (s *Service) segmentRules(ctx context.Context, sc *Scope, id string) (SegmentRules, error) {
	var r SegmentRules
	var raw []byte
	if !uuidRe.MatchString(id) {
		return r, newErr(404, "segment_not_found")
	}
	err := s.Pool.QueryRow(ctx, `SELECT rules FROM contact_segments WHERE id=$1 AND environment_id=$2`, id, sc.EnvID).Scan(&raw)
	if errors.Is(err, db.ErrNoRows) {
		return r, newErr(404, "segment_not_found")
	}
	if err != nil {
		return r, unavailable("segment", err)
	}
	return r, json.Unmarshal(raw, &r)
}
