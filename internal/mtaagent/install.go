package mtaagent

// Installing the managed mail server from the dashboard. These routes exist
// only when the agent is started with -allow-install. The agent still takes no
// shortcuts: it applies exactly the plan whose digest was approved, with the
// installer's own backup, validation, rollback and refusal of a host that
// already runs a mail service. It never replaces a configuration (-adopt) and
// never removes packages.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/managed"
)

// InstallRequest is what the dashboard may decide. Everything else is fixed.
type InstallRequest struct {
	Hostname       string   `json:"hostname"`
	BounceDomain   string   `json:"bounce_domain"`
	Domains        []string `json:"domains"`
	Selector       string   `json:"selector"`
	SubmissionBind string   `json:"submission_bind"`
	InboundRelay   string   `json:"inbound_relay"`
	ComposeSubnet  string   `json:"compose_subnet"`
	Digest         string   `json:"digest,omitempty"`     // apply: the approved plan
	ReceiptID      string   `json:"receipt_id,omitempty"` // rollback
}

// InstallPlan is the reviewable plan.
type InstallPlan struct {
	Digest   string   `json:"digest"`
	Summary  string   `json:"summary"`
	Packages []string `json:"packages"`
	Blockers []string `json:"blockers"`
	Warnings []string `json:"warnings"`
	Conflict string   `json:"conflict"`
}

// InstallMaterial is what the API stores so the worker can submit mail. It
// travels only over the token-protected socket.
type InstallMaterial struct {
	ClientCert []byte `json:"client_cert"`
	ClientKey  []byte `json:"client_key"`
	CA         []byte `json:"ca"`
}

// InstallJob is one apply or rollback, running or finished.
type InstallJob struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`  // apply | rollback
	State    string           `json:"state"` // idle | running | done | failed
	Lines    []string         `json:"lines"`
	Error    string           `json:"error,omitempty"`
	Receipt  *managed.Receipt `json:"receipt,omitempty"`
	Material *InstallMaterial `json:"material,omitempty"`
}

// Installer does the work on the host.
type Installer interface {
	Plan(InstallRequest) (*InstallPlan, error)
	Apply(r InstallRequest, log func(string)) (*managed.Receipt, *InstallMaterial, error)
	Rollback(r InstallRequest, log func(string)) (*managed.Receipt, error)
}

type installState struct {
	mu  sync.Mutex
	job InstallJob
}

func (a *Agent) installRoutes(mux *http.ServeMux) {
	need := func(h func(http.ResponseWriter, *http.Request, InstallRequest)) http.HandlerFunc {
		return a.guard(func(w http.ResponseWriter, r *http.Request) {
			if a.Installer == nil {
				writeJSON(w, 403, map[string]string{"error": "host_install_disabled"})
				return
			}
			var in InstallRequest
			dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
			dec.DisallowUnknownFields()
			if dec.Decode(&in) != nil || len(in.Domains) > 1000 {
				writeJSON(w, 400, map[string]string{"error": "invalid_request"})
				return
			}
			h(w, r, in)
		})
	}
	mux.HandleFunc("POST /install/plan", need(func(w http.ResponseWriter, _ *http.Request, in InstallRequest) {
		a.mu.Lock()
		defer a.mu.Unlock()
		pl, err := a.Installer.Plan(in)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "plan_failed", "message": err.Error()})
			return
		}
		writeJSON(w, 200, pl)
	}))
	mux.HandleFunc("POST /install/apply", need(func(w http.ResponseWriter, _ *http.Request, in InstallRequest) {
		if in.Digest == "" {
			writeJSON(w, 400, map[string]string{"error": "digest_required"})
			return
		}
		a.startJob(w, "apply", func(log func(string)) (*managed.Receipt, *InstallMaterial, error) {
			return a.Installer.Apply(in, log)
		})
	}))
	mux.HandleFunc("POST /install/rollback", need(func(w http.ResponseWriter, _ *http.Request, in InstallRequest) {
		a.startJob(w, "rollback", func(log func(string)) (*managed.Receipt, *InstallMaterial, error) {
			rc, err := a.Installer.Rollback(in, log)
			return rc, nil, err
		})
	}))
	mux.HandleFunc("GET /install/job", a.guard(func(w http.ResponseWriter, _ *http.Request) {
		if a.Installer == nil {
			writeJSON(w, 403, map[string]string{"error": "host_install_disabled"})
			return
		}
		a.install.mu.Lock()
		defer a.install.mu.Unlock()
		j := a.install.job
		if j.State == "" {
			j.State = "idle"
		}
		j.Lines = append([]string{}, j.Lines...)
		writeJSON(w, 200, j)
	}))
}

// startJob runs one installer action in the background; only one at a time.
func (a *Agent) startJob(w http.ResponseWriter, kind string, run func(func(string)) (*managed.Receipt, *InstallMaterial, error)) {
	a.install.mu.Lock()
	if a.install.job.State == "running" {
		a.install.mu.Unlock()
		writeJSON(w, 409, map[string]string{"error": "install_running"})
		return
	}
	id := kind + "-" + time.Now().UTC().Format("20060102T150405.000000000")
	a.install.job = InstallJob{ID: id, Kind: kind, State: "running", Lines: []string{}}
	a.install.mu.Unlock()
	log := func(line string) {
		a.install.mu.Lock()
		defer a.install.mu.Unlock()
		if len(a.install.job.Lines) < 2000 {
			a.install.job.Lines = append(a.install.job.Lines, line)
		}
	}
	go func() {
		a.mu.Lock() // no queue command runs while the mail server changes
		rc, material, err := run(log)
		a.mu.Unlock()
		a.install.mu.Lock()
		defer a.install.mu.Unlock()
		a.install.job.Receipt, a.install.job.State = rc, "done"
		if err != nil {
			a.install.job.State, a.install.job.Error = "failed", err.Error()
			return
		}
		a.install.job.Material = material
	}()
	writeJSON(w, 202, map[string]string{"id": id})
}

// ManagedInstaller is the real installer on this host.
type ManagedInstaller struct {
	Runner managed.Runner
}

func (m *ManagedInstaller) params(r InstallRequest) managed.Params {
	p := managed.Params{Hostname: r.Hostname, BounceDomain: r.BounceDomain, Domains: r.Domains, Selector: r.Selector,
		SubmissionBind: r.SubmissionBind, InboundRelay: r.InboundRelay, ComposeSubnet: r.ComposeSubnet}
	p.Platform = managed.DetectPlatform("/", m.Runner.Run)
	return p
}

// Plan reads the host and changes nothing.
func (m *ManagedInstaller) Plan(r InstallRequest) (*InstallPlan, error) {
	pl, err := managed.BuildPlan(m.params(r))
	if err != nil {
		return nil, err
	}
	out := &InstallPlan{Digest: pl.Digest, Summary: pl.Summary(), Packages: pl.MissingPackages, Warnings: pl.Warnings, Conflict: pl.Conflict}
	for _, b := range pl.Blockers {
		out.Blockers = append(out.Blockers, b.Detail)
	}
	return out, nil
}

type loggingRunner struct {
	r   managed.Runner
	log func(string)
}

func (l loggingRunner) Run(name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	l.log("$ " + cmd)
	out, err := l.r.Run(name, args...)
	if err != nil {
		l.log("failed: " + cmd + ": " + err.Error())
		if out != "" {
			l.log(out)
		}
	}
	return out, err
}

func logSteps(rc *managed.Receipt, log func(string)) {
	if rc == nil {
		return
	}
	for _, s := range rc.Steps {
		state := "ok"
		if !s.OK {
			state = "FAIL"
		}
		log(strings.TrimSpace(state + " " + s.Name + " " + s.Detail))
	}
	log("result: " + rc.Result + " (receipt " + rc.ID + ")")
}

// Apply executes the approved plan and reads the worker's certificate.
func (m *ManagedInstaller) Apply(r InstallRequest, log func(string)) (*managed.Receipt, *InstallMaterial, error) {
	rc, err := managed.Apply(m.params(r), r.Digest, loggingRunner{m.Runner, log}, time.Now())
	logSteps(rc, log)
	if err != nil {
		return rc, nil, err
	}
	mat := &InstallMaterial{}
	for path, into := range map[string]*[]byte{"/etc/njiwa-mail/submission/client.crt": &mat.ClientCert,
		"/etc/njiwa-mail/submission/client.key": &mat.ClientKey, "/etc/njiwa-mail/submission/ca.crt": &mat.CA} {
		if *into, err = os.ReadFile(path); err != nil {
			return rc, nil, err
		}
	}
	return rc, mat, nil
}

// Rollback restores the host from a receipt.
func (m *ManagedInstaller) Rollback(r InstallRequest, log func(string)) (*managed.Receipt, error) {
	rc, err := managed.Rollback(m.params(r), r.ReceiptID, loggingRunner{m.Runner, log})
	logSteps(rc, log)
	return rc, err
}

func (c *Client) send(ctx context.Context, path string, in InstallRequest, want int, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://agent"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnreachable
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != want {
		var e struct{ Error, Message string }
		_ = json.Unmarshal(body, &e)
		return &AgentError{Status: resp.StatusCode, Code: e.Error, Message: e.Message}
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

// InstallPlan asks the host what installing would change.
func (c *Client) InstallPlan(ctx context.Context, in InstallRequest) (*InstallPlan, error) {
	var out InstallPlan
	err := c.send(ctx, "/install/plan", in, 200, &out)
	return &out, err
}

// InstallApply starts applying the approved plan.
func (c *Client) InstallApply(ctx context.Context, in InstallRequest) error {
	return c.send(ctx, "/install/apply", in, 202, nil)
}

// InstallRollback starts a rollback.
func (c *Client) InstallRollback(ctx context.Context, in InstallRequest) error {
	return c.send(ctx, "/install/rollback", in, 202, nil)
}

// InstallJob is the current or last job.
func (c *Client) InstallJob(ctx context.Context) (*InstallJob, error) {
	var out InstallJob
	err := c.do(ctx, "GET", "/install/job", &out)
	return &out, err
}
