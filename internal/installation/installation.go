// Package installation computes installation-level readiness: is this Njiwa
// install able to hand mail to the world? It is deliberately separate from
// per-project onboarding, so one project's history can never turn the
// installation (or another project) green. Readiness is derived from facts and
// is a pure function, so every predicate is unit-testable without a database.
package installation

import "time"

type Status string

const (
	NotStarted            Status = "not_started"
	Checking              Status = "checking"
	Ready                 Status = "ready"
	NeedsAction           Status = "needs_action"
	BlockedExternalAction Status = "blocked_external_action"
	Error                 Status = "error"
	SkippedOptional       Status = "skipped_optional"
	Stale                 Status = "stale"
)

// Stable problem codes. The UI maps these to fix instructions; they never
// carry secrets or raw server text.
const (
	CodeNoProfile        = "TRANSPORT_NOT_SELECTED"
	CodeSMTPIncomplete   = "SMTP_CONFIG_INCOMPLETE"
	CodeCheckMissing     = "CHECK_NOT_RUN"
	CodeCheckStale       = "CHECK_STALE"
	CodeCheckFailed      = "CHECK_FAILED"
	CodeInstallerMissing = "INSTALLER_NOT_AVAILABLE"
	CodeEgressBlocked    = "SMTP_EGRESS_BLOCKED"
	CodeTransportFailing = "TRANSPORT_FAILING"
	CodeBouncesNotSetUp  = "BOUNCES_NOT_CONFIGURED"
	CodeBounceListener   = "BOUNCE_LISTENER_DOWN"
	CodeInboundSources   = "INBOUND_SOURCES_WRONG"
	CodeCertExpiring     = "CLIENT_CERT_EXPIRING"
	CodeCertExpired      = "CLIENT_CERT_EXPIRED"
)

// DefaultCheckTTL is how long a passing check stays trustworthy.
const DefaultCheckTTL = 24 * time.Hour

const (
	ModeManaged  = "managed_mta"
	ModeExisting = "existing_smtp"
)

// Check is the latest recorded result of one check key for the active profile.
type Check struct {
	Result      string // pass | fail | skipped
	Revision    int64
	CheckedAt   time.Time
	ExpiresAt   *time.Time
	ProblemCode string
	External    bool // failure needs action outside Njiwa (provider, DNS, VPS)
}

type Facts struct {
	Now             time.Time
	Mode            string // "" when no profile is active
	ProfileRevision int64
	ConfigComplete  bool
	Checks          map[string]Check // latest per check key (any revision)
	// ManagedInstalled is true only when a verified installer receipt exists.
	ManagedInstalled bool
	// ClientCertNotAfter is when the worker's submission certificate
	// (managed mode) expires; nil when unknown.
	ClientCertNotAfter *time.Time
	// Delivery is what the worker saw recently on the active profile. Checks
	// can pass while mail is not going out; this keeps the status honest.
	Delivery *DeliveryHealth
	// Bounces says whether bounce reports can reach Njiwa (C08). Optional:
	// sending works without it, but hard bounces are then not suppressed
	// automatically and delivery evidence is limited.
	Bounces *BounceFacts
}

// BounceFacts describe the bounce path.
type BounceFacts struct {
	Configured bool       // BOUNCE_ADDRESS is set
	ListenerUp bool       // the inbound listener reported in recently
	LastReport *time.Time // the last bounce report received
	// SourcesProblem is set when the inbound listener would refuse the host
	// that relays bounces to it (managed mode): what is wrong and the value
	// INBOUND_ALLOWED_SOURCES needs.
	SourcesProblem string
}

// DeliveryHealth summarises recent Live send attempts.
type DeliveryHealth struct {
	TransportFailures int        // connection-level temporary failures in the window
	LastFailure       *time.Time // most recent of those
	LastCode          string     // its reason code
	LastSubmitted     *time.Time // most recent successful hand-over
}

// CertWarnBefore is how early readiness warns about the worker's managed
// submission certificate. The installer renews it within 60 days, so seeing
// this means the installer has not been re-run.
const CertWarnBefore = 30 * 24 * time.Hour

// FailureThreshold failures with no successful submission since make the
// installation degraded.
const FailureThreshold = 3

// Failing reports whether recent attempts show mail is not going out.
func (d *DeliveryHealth) Failing() bool {
	if d == nil || d.TransportFailures < FailureThreshold || d.LastFailure == nil {
		return false
	}
	return d.LastSubmitted == nil || d.LastSubmitted.Before(*d.LastFailure)
}

type Action struct {
	Label string `json:"label"`
	Route string `json:"route"`
}

type Step struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      Status     `json:"status"`
	Required    bool       `json:"required"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	ProblemCode string     `json:"problem_code,omitempty"`
	Message     string     `json:"message,omitempty"`
	Action      *Action    `json:"action,omitempty"`
}

type Progress struct {
	Complete int `json:"complete"`
	Required int `json:"required"`
}

type Readiness struct {
	Scope    string   `json:"scope"`
	Mode     string   `json:"mode,omitempty"`
	Status   Status   `json:"status"`
	Progress Progress `json:"progress"`
	Steps    []Step   `json:"steps"`
}

// Compute derives installation readiness. Counts are computed here, never
// hard-coded in a client.
func Compute(f Facts) Readiness {
	if f.Now.IsZero() {
		f.Now = time.Now()
	}
	r := Readiness{Scope: "installation", Mode: f.Mode}
	choose := Step{ID: "choose_mode", Title: "Choose how mail is sent", Required: true,
		Action: &Action{Label: "Choose delivery mode", Route: "/app/installation"}}
	if f.Mode == "" {
		choose.Status, choose.ProblemCode = NotStarted, CodeNoProfile
		choose.Message = "No delivery mode is selected yet."
		r.Steps = []Step{choose}
		return finish(r)
	}
	choose.Status = Ready
	r.Steps = append(r.Steps, choose)

	switch f.Mode {
	case ModeExisting:
		cfg := Step{ID: "configure", Title: "Connect your mail server", Required: true,
			Action: &Action{Label: "Edit mail server settings", Route: "/app/installation"}}
		if f.ConfigComplete {
			cfg.Status = Ready
		} else {
			cfg.Status, cfg.ProblemCode = NeedsAction, CodeSMTPIncomplete
			cfg.Message = "Enter the SMTP host, port, encryption, username and password."
		}
		r.Steps = append(r.Steps, cfg)
		r.Steps = append(r.Steps,
			checkStep(f, "verify_connection", "Verify connection and sign-in", "connection", cfg.Status == Ready),
			checkStep(f, "verify_sender", "Send a test message", "test_send", cfg.Status == Ready))
	case ModeManaged:
		pre := checkStep(f, "preflight", "Check this server can send directly", "preflight", true)
		inst := Step{ID: "install", Title: "Install the mail delivery service", Required: true}
		if f.ManagedInstalled {
			inst.Status = Ready
			if na := f.ClientCertNotAfter; na != nil {
				switch {
				case !f.Now.Before(*na):
					inst.Status, inst.ProblemCode = Error, CodeCertExpired
					inst.Message = "The worker's submission certificate expired on " + na.Format("2006-01-02") + ". Mail cannot be handed to the mail delivery service until it is renewed."
				case na.Sub(f.Now) < CertWarnBefore:
					inst.Status, inst.ProblemCode = NeedsAction, CodeCertExpiring
					inst.Message = "The worker's submission certificate expires on " + na.Format("2006-01-02") + ". Renew it on the host before then."
				}
			}
		} else {
			inst.Status, inst.ProblemCode = NotStarted, CodeInstallerMissing
			inst.Message = "The managed installer has not been run on this host."
		}
		r.Steps = append(r.Steps, pre, inst,
			checkStep(f, "verify_transport", "Verify local submission", "connection", f.ManagedInstalled))
	}
	if b := f.Bounces; b != nil {
		st := Step{ID: "bounce_reports", Title: "Bounce reports reach Njiwa-Mail", Required: false,
			Action: &Action{Label: "How to set up bounces", Route: "/app/installation"}}
		switch {
		case !b.Configured:
			st.Status, st.ProblemCode = NeedsAction, CodeBouncesNotSetUp
			st.Message = "No bounce address is set, so bounce reports do not come back to Njiwa-Mail. Mail is still sent, but hard bounces are not suppressed automatically and you see less delivery evidence."
		case b.SourcesProblem != "":
			st.Status, st.ProblemCode = NeedsAction, CodeInboundSources
			st.Message = b.SourcesProblem
		case !b.ListenerUp:
			st.Status, st.ProblemCode = NeedsAction, CodeBounceListener
			st.Message = "A bounce address is set, but the inbound listener has not reported in. Bounce reports cannot be received until it runs."
		default:
			st.Status = Ready
			if b.LastReport != nil {
				t := *b.LastReport
				st.CheckedAt = &t
				st.Message = "Bounce reports are received."
			} else {
				st.Message = "Ready to receive bounce reports; none has arrived yet."
			}
		}
		r.Steps = append(r.Steps, st)
	}
	// Shown only while mail is failing to go out, so a quiet installation is
	// not given a green it has no evidence for.
	if f.Delivery.Failing() {
		t := *f.Delivery.LastFailure
		r.Steps = append(r.Steps, Step{ID: "live_delivery", Title: "Live mail is going out", Status: Error, Required: true,
			CheckedAt: &t, ProblemCode: CodeTransportFailing,
			Message: "Recent Live messages could not reach the mail server (" + f.Delivery.LastCode + "). They stay queued and are retried; fix the mail server and test again.",
			Action:  &Action{Label: "Test connection", Route: "/app/installation"}})
	}
	return finish(r)
}

func checkStep(f Facts, id, title, key string, prerequisiteOK bool) Step {
	s := Step{ID: id, Title: title, Required: true, Action: &Action{Label: "Run check", Route: "/app/installation"}}
	if !prerequisiteOK {
		s.Status, s.ProblemCode, s.Message = NotStarted, CodeCheckMissing, "Complete the previous step first."
		return s
	}
	c, ok := f.Checks[key]
	if !ok {
		s.Status, s.ProblemCode, s.Message = NeedsAction, CodeCheckMissing, "This check has not been run."
		return s
	}
	t := c.CheckedAt
	s.CheckedAt = &t
	switch {
	case c.Revision != f.ProfileRevision:
		s.Status, s.ProblemCode = Stale, CodeCheckStale
		s.Message = "Settings changed after this check. Run it again."
	case c.ExpiresAt != nil && !f.Now.Before(*c.ExpiresAt):
		s.Status, s.ProblemCode = Stale, CodeCheckStale
		s.Message = "This result is out of date. Run it again."
	case c.Result == "pass":
		s.Status = Ready
	case c.Result == "skipped":
		s.Status, s.ProblemCode, s.Message = NeedsAction, CodeCheckMissing, "This check was skipped."
	default:
		s.Status, s.ProblemCode = NeedsAction, c.ProblemCode
		if s.ProblemCode == "" {
			s.ProblemCode = CodeCheckFailed
		}
		if c.External {
			s.Status = BlockedExternalAction
		}
		s.Message = "The last check failed. Fix the problem and run it again."
	}
	return s
}

func finish(r Readiness) Readiness {
	var worst Status = Ready
	rank := map[Status]int{Ready: 0, SkippedOptional: 0, Checking: 1, Stale: 2, NeedsAction: 3, NotStarted: 3, BlockedExternalAction: 4, Error: 5}
	for _, s := range r.Steps {
		if !s.Required {
			continue
		}
		r.Progress.Required++
		if s.Status == Ready {
			r.Progress.Complete++
		}
		if rank[s.Status] > rank[worst] {
			worst = s.Status
		}
	}
	if worst == NotStarted && r.Progress.Complete > 0 {
		worst = NeedsAction
	}
	r.Status = worst
	return r
}
