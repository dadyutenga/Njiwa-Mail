package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
	"github.com/njiwa-tz/mail/internal/secure"
)

// Contacts are the people marketing mail goes to. A contact's status is not
// stored: it is read from the same tables sending uses, so it can never
// disagree with them. A suppression (hard bounce, complaint, manual block) or
// a global unsubscribe outranks any list membership.
const contactStatusSQL = `CASE
  WHEN EXISTS (SELECT 1 FROM suppressions x WHERE x.environment_id=c.environment_id AND x.recipient_hash=c.email_hash AND x.source='complaint') THEN 'complained'
  WHEN EXISTS (SELECT 1 FROM suppressions x WHERE x.environment_id=c.environment_id AND x.recipient_hash=c.email_hash) THEN 'bounced'
  WHEN c.unsubscribed_at IS NOT NULL
    OR EXISTS (SELECT 1 FROM bulk_unsubscribes u WHERE u.environment_id=c.environment_id AND u.recipient_hash=c.email_hash) THEN 'unsubscribed'
  ELSE 'active' END`

// MaxImportRows is the most rows one import may carry.
const MaxImportRows = 50000

var attrKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

type contactData struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// ContactInput creates or updates a contact (matched by address).
type ContactInput struct {
	Email      string         `json:"email"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
	Lists      []string       `json:"lists"` // list ids to add the contact to
}

func cleanAttributes(in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if len(in) > 50 {
		return nil, badRequest("at most 50 attributes")
	}
	for k, v := range in {
		if !attrKeyRe.MatchString(k) {
			return nil, badRequest("attribute names are lower case letters, digits and _ (" + clean(k, 40) + ")")
		}
		switch x := v.(type) {
		case nil:
		case string:
			out[k] = clean(x, 500)
		case bool, float64, json.Number:
			out[k] = x
		case int:
			out[k] = x
		default:
			return nil, badRequest("attribute " + k + " must be text, a number or true/false")
		}
	}
	return out, nil
}

// UpsertContact creates the contact or, when the address exists, updates the
// name and merges attributes. It never resubscribes anyone.
func (s *Service) UpsertContact(ctx context.Context, sc *Scope, in ContactInput) (string, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", false, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	id, created, err := s.upsertContactTx(ctx, tx, sc, in, true)
	if err != nil {
		return "", false, err
	}
	for _, l := range in.Lists {
		if err := addMemberTx(ctx, tx, sc, l, id); err != nil {
			return "", false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, unavailable("commit", err)
	}
	return id, created, nil
}

func (s *Service) upsertContactTx(ctx context.Context, tx *db.Tx, sc *Scope, in ContactInput, update bool) (string, bool, error) {
	email, ok := NormalizeEmail(in.Email)
	if !ok {
		return "", false, invalid("invalid_email")
	}
	attrs, err := cleanAttributes(in.Attributes)
	if err != nil {
		return "", false, err
	}
	name := clean(strings.TrimSpace(in.Name), 200)
	hash := s.Box.MAC("recipient", []byte(email))
	var id string
	var enc []byte
	err = tx.QueryRow(ctx, `SELECT id::text, data_enc FROM contacts WHERE environment_id=$1 AND email_hash=$2`, sc.EnvID, hash).Scan(&id, &enc)
	switch {
	case errors.Is(err, db.ErrNoRows):
		id = secure.NewUUID()
		sealed, err := s.Box.Seal(mustJSON(contactData{Email: email, Name: name}), []byte("contact:"+id))
		if err != nil {
			return "", false, unavailable("seal", err)
		}
		aj, _ := json.Marshal(attrs)
		if _, err := tx.Exec(ctx, `INSERT INTO contacts(id, project_id, environment_id, email_hash, email_masked, email_domain, data_enc, attributes)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, sc.ProjectID, sc.EnvID, hash, maskEmail(email), email[strings.LastIndexByte(email, '@')+1:], sealed, aj); err != nil {
			return "", false, unavailable("contact_create", err)
		}
		return id, true, nil
	case err != nil:
		return "", false, unavailable("contact_lookup", err)
	}
	if !update {
		return id, false, nil
	}
	if name != "" {
		sealed, err := s.Box.Seal(mustJSON(contactData{Email: email, Name: name}), []byte("contact:"+id))
		if err != nil {
			return "", false, unavailable("seal", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE contacts SET data_enc=$2, updated_at=now() WHERE id=$1`, id, sealed); err != nil {
			return "", false, unavailable("contact_update", err)
		}
	}
	if len(attrs) > 0 {
		aj, _ := json.Marshal(attrs)
		if _, err := tx.Exec(ctx, `UPDATE contacts SET attributes=attributes || $2::jsonb, updated_at=now() WHERE id=$1`, id, aj); err != nil {
			return "", false, unavailable("contact_update", err)
		}
	}
	return id, false, nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func addMemberTx(ctx context.Context, tx *db.Tx, sc *Scope, listID, contactID string) error {
	if !uuidRe.MatchString(listID) {
		return badRequest("unknown list")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO contact_list_members(list_id, contact_id)
SELECT l.id, $2 FROM contact_lists l WHERE l.id=$1 AND l.environment_id=$3
ON CONFLICT DO NOTHING`, listID, contactID, sc.EnvID)
	if err != nil {
		return unavailable("list_member", err)
	}
	if tag.RowsAffected() == 0 {
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contact_lists WHERE id=$1 AND environment_id=$2)`, listID, sc.EnvID).Scan(&ok)
		if !ok {
			return newErr(404, "list_not_found")
		}
	}
	return nil
}

// ContactFilter narrows the contact list.
type ContactFilter struct {
	ListID, Status, Domain, Query string
}

func (s *Service) ListContacts(ctx context.Context, sc *Scope, f ContactFilter, pg Page) ([]map[string]any, bool, error) {
	if f.ListID != "" && !uuidRe.MatchString(f.ListID) {
		return nil, false, badRequest("unknown list")
	}
	return s.page(ctx, pg, `SELECT * FROM (SELECT c.id::text AS id, c.email_masked, c.email_domain, c.attributes, c.created_at, c.updated_at,
  `+contactStatusSQL+` AS status,
  (SELECT count(*) FROM contact_list_members m WHERE m.contact_id=c.id AND m.unsubscribed_at IS NULL) AS lists
FROM contacts c WHERE c.environment_id=$1
  AND ($2='' OR EXISTS (SELECT 1 FROM contact_list_members m WHERE m.contact_id=c.id AND m.list_id=NULLIF($2,'')::uuid))
  AND ($3='' OR c.email_domain=lower($3))
  AND ($4='' OR c.email_hash=$5)) x
WHERE ($6='' OR x.status=$6) ORDER BY x.created_at DESC, x.id`,
		sc.EnvID, f.ListID, f.Domain, f.Query, s.queryHash(f.Query), f.Status)
}

// queryHash lets a search by exact address find a contact without storing
// addresses in plain text.
func (s *Service) queryHash(q string) []byte {
	if e, ok := NormalizeEmail(q); ok {
		return s.Box.MAC("recipient", []byte(e))
	}
	return []byte{0}
}

// GetContact returns one contact with its address, lists and sending history
// (from the messages sent to that address in this environment).
func (s *Service) GetContact(ctx context.Context, sc *Scope, id string) (map[string]any, error) {
	if !uuidRe.MatchString(id) {
		return nil, newErr(404, "not_found")
	}
	m, err := s.one(ctx, `SELECT c.id::text AS id, c.email_masked, c.email_domain, c.attributes, c.created_at, c.updated_at, c.unsubscribed_at,
  `+contactStatusSQL+` AS status,
  (SELECT count(*) FROM messages g WHERE g.environment_id=c.environment_id AND g.recipient_hash=c.email_hash) AS messages,
  (SELECT count(*) FROM messages g WHERE g.environment_id=c.environment_id AND g.recipient_hash=c.email_hash AND g.state='submitted') AS submitted,
  (SELECT count(*) FROM messages g WHERE g.environment_id=c.environment_id AND g.recipient_hash=c.email_hash AND g.state IN ('failed','expired')) AS failed,
  (SELECT count(*) FROM messages g WHERE g.environment_id=c.environment_id AND g.recipient_hash=c.email_hash AND g.bounce_state IS NOT NULL) AS bounced,
  (SELECT max(g.created_at) FROM messages g WHERE g.environment_id=c.environment_id AND g.recipient_hash=c.email_hash) AS last_message_at
FROM contacts c WHERE c.id=$1 AND c.environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return nil, err
	}
	var enc []byte
	if err := s.Pool.QueryRow(ctx, `SELECT data_enc FROM contacts WHERE id=$1`, id).Scan(&enc); err != nil {
		return nil, notFound(err)
	}
	var d contactData
	if pt, err := s.Box.Open(enc, []byte("contact:"+id)); err == nil {
		_ = json.Unmarshal(pt, &d)
	}
	m["email"], m["name"] = d.Email, d.Name
	lists, err := s.maps(ctx, `SELECT l.id::text AS id, l.name, m.added_at, m.unsubscribed_at FROM contact_list_members m
JOIN contact_lists l ON l.id=m.list_id WHERE m.contact_id=$1 ORDER BY l.name`, id)
	if err != nil {
		return nil, err
	}
	m["list_memberships"] = lists
	return m, nil
}

func (s *Service) DeleteContact(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM contacts WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("contact_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// Resubscribe lifts the contact's own unsubscribe: from one list (listID) or
// the global one ("" ). A suppression (bounce, complaint, block) is never
// lifted here; that is a separate, deliberate action.
func (s *Service) Resubscribe(ctx context.Context, sc *Scope, contactID, listID string) error {
	if !uuidRe.MatchString(contactID) {
		return newErr(404, "not_found")
	}
	var hash []byte
	if err := s.Pool.QueryRow(ctx, `SELECT email_hash FROM contacts WHERE id=$1 AND environment_id=$2`, contactID, sc.EnvID).Scan(&hash); err != nil {
		return notFound(err)
	}
	if listID != "" {
		if !uuidRe.MatchString(listID) {
			return newErr(404, "list_not_found")
		}
		tag, err := s.Pool.Exec(ctx, `UPDATE contact_list_members SET unsubscribed_at=NULL WHERE list_id=$1 AND contact_id=$2`, listID, contactID)
		if err != nil {
			return unavailable("resubscribe", err)
		}
		if tag.RowsAffected() == 0 {
			return newErr(404, "list_not_found")
		}
		return nil
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE contacts SET unsubscribed_at=NULL, updated_at=now() WHERE id=$1`, contactID); err != nil {
		return unavailable("resubscribe", err)
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM bulk_unsubscribes WHERE environment_id=$1 AND recipient_hash=$2`, sc.EnvID, hash)
	return err
}

// ---------------------------------------------------------------- lists

func (s *Service) CreateList(ctx context.Context, sc *Scope, name, description string) (string, error) {
	name = clean(strings.TrimSpace(name), 120)
	if name == "" {
		return "", badRequest("give the list a name")
	}
	var id string
	err := s.Pool.QueryRow(ctx, `INSERT INTO contact_lists(project_id, environment_id, name, description) VALUES ($1,$2,$3,$4)
ON CONFLICT (environment_id, name) DO NOTHING RETURNING id::text`, sc.ProjectID, sc.EnvID, name, clean(description, 500)).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return "", &Error{Status: 409, Code: "list_exists", Message: "a list with this name exists"}
	}
	if err != nil {
		return "", unavailable("list_create", err)
	}
	return id, nil
}

func (s *Service) ListLists(ctx context.Context, sc *Scope) ([]map[string]any, error) {
	return s.maps(ctx, `SELECT l.id::text AS id, l.name, l.description, l.created_at,
  (SELECT count(*) FROM contact_list_members m WHERE m.list_id=l.id) AS members,
  (SELECT count(*) FROM contact_list_members m WHERE m.list_id=l.id AND m.unsubscribed_at IS NOT NULL) AS unsubscribed
FROM contact_lists l WHERE l.environment_id=$1 ORDER BY l.name`, sc.EnvID)
}

func (s *Service) DeleteList(ctx context.Context, sc *Scope, id string) error {
	if !uuidRe.MatchString(id) {
		return newErr(404, "not_found")
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM contact_lists WHERE id=$1 AND environment_id=$2`, id, sc.EnvID)
	if err != nil {
		return unavailable("list_delete", err)
	}
	if tag.RowsAffected() == 0 {
		return newErr(404, "not_found")
	}
	return nil
}

// SetMembership adds contacts to a list or removes them from it.
func (s *Service) SetMembership(ctx context.Context, sc *Scope, listID string, contactIDs []string, add bool) (int, error) {
	if len(contactIDs) > 1000 {
		return 0, badRequest("at most 1000 contacts at a time")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, unavailable("begin", err)
	}
	defer tx.Rollback(ctx)
	n := 0
	for _, c := range contactIDs {
		if !uuidRe.MatchString(c) {
			continue
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contacts WHERE id=$1 AND environment_id=$2)`, c, sc.EnvID).Scan(&ok); err != nil || !ok {
			continue
		}
		if add {
			if err := addMemberTx(ctx, tx, sc, listID, c); err != nil {
				return 0, err
			}
		} else if _, err := tx.Exec(ctx, `DELETE FROM contact_list_members m USING contact_lists l
WHERE m.list_id=l.id AND l.id=$1 AND l.environment_id=$3 AND m.contact_id=$2`, listID, c, sc.EnvID); err != nil {
			return 0, unavailable("list_member", err)
		}
		n++
	}
	return n, tx.Commit(ctx)
}

// ---------------------------------------------------------------- import

// ImportInput is a CSV (with a header row) or JSON array import. Mapping
// names the source column (CSV header or JSON key) for each field; the
// default is "email" and "name", and every other column becomes an attribute
// when Attributes is empty.
type ImportInput struct {
	Format  string `json:"format"` // csv | json
	Data    string `json:"data"`
	Mapping struct {
		Email      string            `json:"email"`
		Name       string            `json:"name"`
		Attributes map[string]string `json:"attributes"` // attribute -> column
	} `json:"mapping"`
	ListID         string `json:"list_id"`
	UpdateExisting bool   `json:"update_existing"`
}

type ImportReport struct {
	Rows     int            `json:"rows"`
	Created  int            `json:"created"`
	Updated  int            `json:"updated"`
	Existing int            `json:"existing"` // already there, left unchanged
	Invalid  int            `json:"invalid"`
	Errors   []ImportError  `json:"errors"`
	Columns  []string       `json:"columns"`
	Sample   map[string]any `json:"sample,omitempty"`
}

type ImportError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

func importRows(in ImportInput) ([]string, []map[string]string, error) {
	switch in.Format {
	case "", "csv":
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(in.Data, "\ufeff")))
		r.FieldsPerRecord = -1
		r.TrimLeadingSpace = true
		recs, err := r.ReadAll()
		if err != nil {
			return nil, nil, badRequest("the CSV could not be read: " + err.Error())
		}
		if len(recs) < 1 {
			return nil, nil, badRequest("the CSV is empty")
		}
		head := recs[0]
		for i := range head {
			head[i] = strings.TrimSpace(head[i])
		}
		var rows []map[string]string
		for _, rec := range recs[1:] {
			m := map[string]string{}
			for i, v := range rec {
				if i < len(head) {
					m[head[i]] = strings.TrimSpace(v)
				}
			}
			rows = append(rows, m)
		}
		return head, rows, nil
	case "json":
		var arr []map[string]any
		if err := json.Unmarshal([]byte(in.Data), &arr); err != nil {
			return nil, nil, badRequest("the JSON must be an array of objects")
		}
		seen := map[string]bool{}
		var head []string
		var rows []map[string]string
		for _, o := range arr {
			m := map[string]string{}
			for k, v := range o {
				if !seen[k] {
					seen[k] = true
					head = append(head, k)
				}
				if v != nil {
					m[k] = strings.TrimSpace(fmt.Sprint(v))
				}
			}
			rows = append(rows, m)
		}
		sort.Strings(head)
		return head, rows, nil
	}
	return nil, nil, badRequest("format must be csv or json")
}

// ImportContacts adds or updates contacts from a file. With dryRun nothing
// is written; the report shows the columns, the first mapped row and the
// problems, so the mapping can be checked first.
func (s *Service) ImportContacts(ctx context.Context, sc *Scope, in ImportInput, dryRun bool) (*ImportReport, error) {
	head, rows, err := importRows(in)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxImportRows {
		return nil, badRequest(fmt.Sprintf("at most %d rows per import", MaxImportRows))
	}
	ec, nc := in.Mapping.Email, in.Mapping.Name
	if ec == "" {
		for _, h := range head {
			if strings.EqualFold(h, "email") || strings.EqualFold(h, "e-mail") {
				ec = h
			}
		}
	}
	if nc == "" {
		for _, h := range head {
			if strings.EqualFold(h, "name") {
				nc = h
			}
		}
	}
	if ec == "" {
		return nil, badRequest("choose the column that holds the email address")
	}
	attrMap := in.Mapping.Attributes
	if len(attrMap) == 0 {
		attrMap = map[string]string{}
		for _, h := range head {
			k := strings.Trim(regexp.MustCompile(`[^a-z0-9_]+`).ReplaceAllString(strings.ToLower(h), "_"), "_")
			if h != ec && h != nc && attrKeyRe.MatchString(k) {
				attrMap[k] = h
			}
		}
	}
	rep := &ImportReport{Rows: len(rows), Columns: head, Errors: []ImportError{}}
	var tx *db.Tx
	if !dryRun {
		if tx, err = s.Pool.Begin(ctx); err != nil {
			return nil, unavailable("begin", err)
		}
		defer tx.Rollback(ctx)
		if in.ListID != "" {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contact_lists WHERE id=$1 AND environment_id=$2)`, in.ListID, sc.EnvID).Scan(&ok); err != nil || !ok {
				return nil, newErr(404, "list_not_found")
			}
		}
	}
	addErr := func(row int, reason string) {
		rep.Invalid++
		if len(rep.Errors) < 100 {
			rep.Errors = append(rep.Errors, ImportError{Row: row, Reason: reason})
		}
	}
	seen := map[string]bool{}
	for i, r := range rows {
		rowNo := i + 2 // the header is row 1
		if in.Format == "json" {
			rowNo = i + 1
		}
		email, ok := NormalizeEmail(r[ec])
		if !ok {
			addErr(rowNo, "invalid_email")
			continue
		}
		if seen[email] {
			addErr(rowNo, "duplicate_in_file")
			continue
		}
		seen[email] = true
		attrs := map[string]any{}
		for k, col := range attrMap {
			if v := r[col]; v != "" {
				attrs[k] = v
			}
		}
		ci := ContactInput{Email: email, Name: r[nc], Attributes: attrs}
		if rep.Sample == nil {
			rep.Sample = map[string]any{"email": maskEmail(email), "name": ci.Name, "attributes": attrs}
		}
		if dryRun {
			continue
		}
		id, created, err := s.upsertContactTx(ctx, tx, sc, ci, in.UpdateExisting)
		if err != nil {
			var e *Error
			if errors.As(err, &e) && e.Status < 500 {
				addErr(rowNo, e.Code)
				continue
			}
			return nil, err
		}
		switch {
		case created:
			rep.Created++
		case in.UpdateExisting:
			rep.Updated++
		default:
			rep.Existing++
		}
		if in.ListID != "" {
			if err := addMemberTx(ctx, tx, sc, in.ListID, id); err != nil {
				return nil, err
			}
		}
	}
	if !dryRun {
		if err := tx.Commit(ctx); err != nil {
			return nil, unavailable("commit", err)
		}
	}
	return rep, nil
}

// contactVars are a contact's merge fields for a campaign: name and every
// attribute, as text.
func (s *Service) contactVars(id string, enc []byte, attrs map[string]any) (string, map[string]string) {
	var d contactData
	if pt, err := s.Box.Open(enc, []byte("contact:"+id)); err == nil {
		_ = json.Unmarshal(pt, &d)
	}
	vars := map[string]string{}
	for k, v := range attrs {
		vars[k] = fmt.Sprint(v)
	}
	if d.Name != "" {
		vars["name"] = d.Name
	}
	return d.Email, vars
}
