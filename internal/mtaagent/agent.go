// Package mtaagent is the small host-side service behind the dashboard's mail
// queue controls (E10). The API runs in a container and must not hold host
// root, so the only way it reaches Postfix is through this agent: a root
// process on the mail host that listens on a Unix socket, requires a shared
// token, and runs exactly five fixed commands. A queue id is validated before
// it gets anywhere near a command line, and commands are executed without a shell.
package mtaagent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Runner runs a host command. The real one is ExecRunner; tests use a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands without a shell, with a timeout and no inherited input.
type ExecRunner struct{ Timeout time.Duration }

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	t := r.Timeout
	if t == 0 {
		t = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

var queueIDRe = regexp.MustCompile(`^[0-9A-Za-z]{6,20}$`)

// ValidQueueID reports whether s can be a Postfix queue id. Anything else is
// refused before it reaches a command.
func ValidQueueID(s string) bool { return queueIDRe.MatchString(s) }

// Action is something an operator may do to one queued message.
type Action string

const (
	Hold    Action = "hold"
	Release Action = "release"
	Requeue Action = "requeue"
	Delete  Action = "delete"
)

// postsuperFlag maps each allowed action to its postsuper option.
var postsuperFlag = map[Action]string{Hold: "-h", Release: "-H", Requeue: "-r", Delete: "-d"}

// Recipient is one queued recipient with the reason it is still waiting.
type Recipient struct {
	Address     string `json:"address"`
	DelayReason string `json:"delay_reason,omitempty"`
}

// QueueEntry is one message in the Postfix queue (from `postqueue -j`).
type QueueEntry struct {
	QueueName   string      `json:"queue_name"`
	QueueID     string      `json:"queue_id"`
	ArrivalTime int64       `json:"arrival_time"`
	MessageSize int64       `json:"message_size"`
	Sender      string      `json:"sender"`
	Recipients  []Recipient `json:"recipients"`
}

// ParseQueue parses `postqueue -j`: one JSON object per line. Malformed lines
// are skipped so one odd entry never hides the rest of the queue.
func ParseQueue(out []byte) []QueueEntry {
	var list []QueueEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var e QueueEntry
		if json.Unmarshal([]byte(line), &e) != nil || !ValidQueueID(e.QueueID) {
			continue
		}
		list = append(list, e)
	}
	return list
}

// Status is the mail server's health as the agent can see it.
type Status struct {
	Queue         map[string]int `json:"queue"` // by queue name: active, deferred, hold, incoming
	Total         int            `json:"total"`
	PostfixActive bool           `json:"postfix_active"`
	SignerActive  bool           `json:"signer_active"`
}

// Agent serves the control API.
type Agent struct {
	Runner Runner
	Token  string
	// Signer is the DKIM signing unit checked for Status (default opendkim).
	Signer string
	// Postfix is the unit checked for Status (default postfix).
	Postfix string

	mu sync.Mutex // one command at a time: postsuper and postqueue must not interleave
}

func (a *Agent) signer() string {
	if a.Signer == "" {
		return "opendkim"
	}
	return a.Signer
}
func (a *Agent) postfix() string {
	if a.Postfix == "" {
		return "postfix"
	}
	return a.Postfix
}

// Handler returns the HTTP handler. Every route needs the token.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /queue", a.guard(a.list))
	mux.HandleFunc("GET /status", a.guard(a.status))
	mux.HandleFunc("POST /queue/{id}/{action}", a.guard(a.act))
	mux.HandleFunc("POST /flush", a.guard(a.flush))
	return mux
}

func (a *Agent) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.Token)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *Agent) queue(ctx context.Context) ([]QueueEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out, err := a.Runner.Run(ctx, "postqueue", "-j")
	if err != nil {
		return nil, err
	}
	return ParseQueue(out), nil
}

func (a *Agent) list(w http.ResponseWriter, r *http.Request) {
	q, err := a.queue(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "postqueue_failed"})
		return
	}
	if q == nil {
		q = []QueueEntry{}
	}
	writeJSON(w, 200, map[string]any{"items": q})
}

func (a *Agent) status(w http.ResponseWriter, r *http.Request) {
	st := Status{Queue: map[string]int{}}
	if q, err := a.queue(r.Context()); err == nil {
		for _, e := range q {
			st.Queue[e.QueueName]++
			st.Total++
		}
	} else {
		writeJSON(w, 502, map[string]string{"error": "postqueue_failed"})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, perr := a.Runner.Run(r.Context(), "systemctl", "is-active", "--quiet", a.postfix())
	_, serr := a.Runner.Run(r.Context(), "systemctl", "is-active", "--quiet", a.signer())
	st.PostfixActive, st.SignerActive = perr == nil, serr == nil
	writeJSON(w, 200, st)
}

func (a *Agent) act(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), Action(r.PathValue("action"))
	flag, ok := postsuperFlag[action]
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown_action"})
		return
	}
	if !ValidQueueID(id) {
		writeJSON(w, 400, map[string]string{"error": "invalid_queue_id"})
		return
	}
	a.mu.Lock()
	_, err := a.Runner.Run(r.Context(), "postsuper", flag, id)
	a.mu.Unlock()
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "postsuper_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "queue_id": id, "action": action})
}

// flush asks Postfix to retry every deferred message now (postqueue -f).
func (a *Agent) flush(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	_, err := a.Runner.Run(r.Context(), "postqueue", "-f")
	a.mu.Unlock()
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "postqueue_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// Listen serves on a Unix socket owned by root, group-readable by gid when
// gid >= 0, and removes a stale socket first. It blocks until ctx is done.
func (a *Agent) Listen(ctx context.Context, socket string, mode os.FileMode, gid int) error {
	if a.Token == "" || len(a.Token) < 16 {
		return errors.New("the agent token must be at least 16 characters")
	}
	if fi, err := os.Lstat(socket); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("%s exists and is not a socket", socket)
		}
		os.Remove(socket)
	}
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(socket, mode); err != nil {
		l.Close()
		return err
	}
	if gid >= 0 {
		if err := os.Chown(socket, 0, gid); err != nil {
			l.Close()
			return err
		}
	}
	srv := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
