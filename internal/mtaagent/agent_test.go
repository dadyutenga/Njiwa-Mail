package mtaagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	out   map[string]string // by "name args"
	fail  map[string]bool
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{name}, args...))
	key := name + " " + strings.Join(args, " ")
	if f.fail[key] {
		return nil, errors.New("boom")
	}
	return []byte(f.out[key]), nil
}

const sampleQueue = `{"queue_name":"deferred","queue_id":"4C1D2E3F4A","arrival_time":1731398762,"message_size":900,"sender":"bounces@bounce.example.com","recipients":[{"address":"user@example.org","delay_reason":"connect to mx.example.org[192.0.2.5]:25: Connection timed out"}]}
not json at all
{"queue_name":"hold","queue_id":"BAD ID; rm -rf /","arrival_time":1,"recipients":[]}
{"queue_name":"active","queue_id":"0000AAAA11","arrival_time":1731398999,"message_size":120,"sender":"x@y.example","recipients":[{"address":"a@b.example"}]}
`

func TestParseQueueSkipsOddLines(t *testing.T) {
	q := ParseQueue([]byte(sampleQueue))
	if len(q) != 2 || q[0].QueueID != "4C1D2E3F4A" || q[0].Recipients[0].DelayReason == "" || q[1].QueueName != "active" {
		t.Fatalf("%+v", q)
	}
}

func TestQueueIDValidation(t *testing.T) {
	for _, ok := range []string{"4C1D2E3F4A", "0000AAAA11", "abcdef", "A1B2C3D4E5F6G7H8I9J0"} {
		if !ValidQueueID(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "short", "abc def1", "ABC;rm -rf", "../../etc", "ABCDEF\n-d", "-ABCDEFG", strings.Repeat("A", 21), "ABC$(id)"} {
		if ValidQueueID(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func newAgent(f *fakeRunner) (*Agent, *httptest.Server) {
	a := &Agent{Runner: f, Token: "0123456789abcdef-token"}
	return a, httptest.NewServer(a.Handler())
}

func do(t *testing.T, srv *httptest.Server, method, path, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAgentRequiresTheToken(t *testing.T) {
	f := &fakeRunner{}
	_, srv := newAgent(f)
	defer srv.Close()
	for _, tok := range []string{"", "wrong", "0123456789abcdef-toke", "0123456789abcdef-token-x"} {
		for _, r := range [][2]string{{"GET", "/queue"}, {"GET", "/status"}, {"POST", "/queue/4C1D2E3F4A/delete"}, {"POST", "/flush"}} {
			if resp := do(t, srv, r[0], r[1], tok); resp.StatusCode != 401 {
				t.Errorf("%s %s with token %q: %d", r[0], r[1], tok, resp.StatusCode)
			}
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a command ran without authorisation: %v", f.calls)
	}
	// An agent without a token refuses everything.
	a := &Agent{Runner: f}
	s2 := httptest.NewServer(a.Handler())
	defer s2.Close()
	if resp := do(t, s2, "GET", "/queue", ""); resp.StatusCode != 401 {
		t.Fatalf("tokenless agent answered %d", resp.StatusCode)
	}
}

func TestAgentRunsOnlyTheFixedCommands(t *testing.T) {
	f := &fakeRunner{out: map[string]string{"postqueue -j": sampleQueue}}
	_, srv := newAgent(f)
	defer srv.Close()
	tok := "0123456789abcdef-token"
	for path, want := range map[string]string{
		"/queue/4C1D2E3F4A/hold":    "postsuper -h 4C1D2E3F4A",
		"/queue/4C1D2E3F4A/release": "postsuper -H 4C1D2E3F4A",
		"/queue/4C1D2E3F4A/requeue": "postsuper -r 4C1D2E3F4A",
		"/queue/4C1D2E3F4A/delete":  "postsuper -d 4C1D2E3F4A",
		"/flush":                    "postqueue -f",
	} {
		f.calls = nil
		if resp := do(t, srv, "POST", path, tok); resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		if len(f.calls) != 1 || strings.Join(f.calls[0], " ") != want {
			t.Fatalf("%s ran %v, want %q", path, f.calls, want)
		}
	}
	// Injection attempts and unknown actions never reach a command.
	f.calls = nil
	for _, path := range []string{
		"/queue/ABC;rm%20-rf/delete", "/queue/%2E%2E%2Fetc/delete", "/queue/ABC$(id)xx/hold", "/queue/-ABCDEFG/delete",
		"/queue/4C1D2E3F4A/postsuper", "/queue/4C1D2E3F4A/delete-all", "/queue/4C1D2E3F4A/-d",
	} {
		if resp := do(t, srv, "POST", path, tok); resp.StatusCode == 200 {
			t.Errorf("%s was accepted", path)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a refused request ran a command: %v", f.calls)
	}
	// A failing command is reported, not hidden.
	f.fail = map[string]bool{"postsuper -d 4C1D2E3F4A": true}
	if resp := do(t, srv, "POST", "/queue/4C1D2E3F4A/delete", tok); resp.StatusCode != 502 {
		t.Fatalf("failed postsuper answered %d", resp.StatusCode)
	}
}

func TestAgentStatusAndSocketClient(t *testing.T) {
	f := &fakeRunner{out: map[string]string{"postqueue -j": sampleQueue}, fail: map[string]bool{"systemctl is-active --quiet opendkim": true}}
	a := &Agent{Runner: f, Token: "0123456789abcdef-token"}
	sock := t.TempDir() + "/a.sock"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Listen(ctx, sock, 0o600, -1) }()
	c := NewClient(sock, "0123456789abcdef-token")
	var st *Status
	var err error
	for i := 0; i < 100; i++ {
		if st, err = c.Status(ctx); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Queue["deferred"] != 1 || st.Queue["active"] != 1 || !st.PostfixActive || st.SignerActive {
		t.Fatalf("status: %+v", st)
	}
	q, err := c.Queue(ctx)
	if err != nil || len(q) != 2 {
		t.Fatalf("%v %v", q, err)
	}
	if err := c.Act(ctx, "bad id!", Delete); err == nil {
		t.Fatal("client sent an invalid queue id")
	}
	if err := NewClient(sock, "wrong-token-wrong-token").Flush(ctx); err == nil {
		t.Fatal("wrong token accepted")
	}
	cancel()
	<-done
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a stopped agent should be unreachable, got %v", err)
	}
	// Refuses a short token and a non-socket path.
	if err := (&Agent{Runner: f, Token: "short"}).Listen(context.Background(), t.TempDir()+"/b.sock", 0o600, -1); err == nil {
		t.Fatal("short token accepted")
	}
}
