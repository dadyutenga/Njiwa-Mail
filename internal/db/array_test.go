package db

import "testing"

func TestModels(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Models() {
		namer, ok := m.(interface{ TableName() string })
		if !ok {
			t.Fatalf("model %T has no table name", m)
		}
		name := namer.TableName()
		if name == "" || seen[name] {
			t.Fatalf("bad table %q", name)
		}
		seen[name] = true
	}
	if len(seen) < 20 {
		t.Fatalf("expected the mail schema, got %d tables", len(seen))
	}
}

func TestParsePGArray(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`{}`, []string{}},
		{`{a,b}`, []string{"a", "b"}},
		{`{emails:send,emails:read}`, []string{"emails:send", "emails:read"}},
		{`{"a,b",c}`, []string{"a,b", "c"}},
		{`{"quote\"d"}`, []string{`quote"d`}},
		{`{NULL}`, []string{""}},
	}
	for _, tc := range cases {
		got, err := parsePGArray(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %#v", tc.in, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %#v want %#v", tc.in, got, tc.want)
			}
		}
	}
}
