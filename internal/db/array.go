package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// textArray scans a PostgreSQL text array. The pgx database/sql driver returns
// arrays as their text form on Go 1.24.
type textArray []string

func (a *textArray) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*a = nil
		return nil
	case string:
		parsed, err := parsePGArray(v)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case []byte:
		parsed, err := parsePGArray(string(v))
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case []string:
		*a = append([]string(nil), v...)
		return nil
	default:
		return fmt.Errorf("text array: unsupported %T", src)
	}
}

// parsePGArray parses a PostgreSQL array literal such as {a,"b,c"}.
func parsePGArray(s string) ([]string, error) {
	if s == "{}" {
		return []string{}, nil
	}
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, fmt.Errorf("text array: %q", s)
	}
	body := s[1 : len(s)-1]
	var out []string
	var b strings.Builder
	inQuote := false
	quoted := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inQuote {
			if c == '\\' && i+1 < len(body) {
				i++
				b.WriteByte(body[i])
				continue
			}
			if c == '"' {
				inQuote = false
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			inQuote = true
			quoted = true
		case ',':
			out = append(out, arrayElem(b.String(), quoted))
			b.Reset()
			quoted = false
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("text array: %q", s)
	}
	out = append(out, arrayElem(b.String(), quoted))
	return out, nil
}

func arrayElem(s string, quoted bool) string {
	if !quoted && s == "NULL" {
		return ""
	}
	return s
}

// CollectStrings reads a single text column and closes rows.
func CollectStrings(rows *Rows) ([]string, error) {
	if rows == nil {
		return nil, sql.ErrNoRows
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CollectMaps reads every column into a JSON-ready map and closes rows.
// jsonb values are decoded. text[] values become []string.
func CollectMaps(rows *Rows) ([]map[string]any, error) {
	if rows == nil || rows.r == nil {
		return nil, sql.ErrNoRows
	}
	defer rows.Close()
	cols, err := rows.r.Columns()
	if err != nil {
		return nil, err
	}
	types, err := rows.r.ColumnTypes()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.r.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.r.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, name := range cols {
			m[name] = normalizeValue(types[i], vals[i])
		}
		out = append(out, m)
	}
	return out, rows.r.Err()
}

func normalizeValue(ct *sql.ColumnType, v any) any {
	if v == nil {
		return nil
	}
	name := ""
	if ct != nil {
		name = strings.ToUpper(ct.DatabaseTypeName())
	}
	switch name {
	case "JSON", "JSONB":
		if decoded, ok := decodeJSON(v); ok {
			return decoded
		}
	}
	if strings.HasPrefix(name, "_") || strings.HasSuffix(name, "[]") {
		if s, ok := v.(string); ok {
			if arr, err := parsePGArray(s); err == nil {
				return arr
			}
		}
		if b, ok := v.([]byte); ok {
			if arr, err := parsePGArray(string(b)); err == nil {
				return arr
			}
		}
	}
	if b, ok := v.([]byte); ok && (name == "JSON" || name == "JSONB" || jsonLike(b)) {
		if decoded, ok := decodeJSON(b); ok {
			return decoded
		}
	}
	return v
}

func decodeJSON(v any) (any, bool) {
	var b []byte
	switch t := v.(type) {
	case []byte:
		b = t
	case string:
		b = []byte(t)
	default:
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func jsonLike(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	switch b[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}
