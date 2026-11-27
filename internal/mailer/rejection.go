package mailer

import (
	"net/textproto"
	"regexp"
	"strings"
)

// Rejection is a 4xx or 5xx answer from the mail server to a submission. It is
// evidence: what was refused, as a stable problem code, and a short reason that
// is safe to store and show.
type Rejection struct {
	Code    int    // SMTP reply code
	Problem string // one of the SMTP_REJECT_* codes
	Reason  string // server text without addresses or long tokens, at most 200 bytes
}

const (
	RejectConnection = "SMTP_REJECT_CONNECTION" // refused before any message: greeting, EHLO, TLS, 421
	RejectAuth       = "SMTP_REJECT_AUTH"
	RejectRelay      = "SMTP_REJECT_RELAY"
	RejectSender     = "SMTP_REJECT_SENDER"
	RejectRecipient  = "SMTP_REJECT_RECIPIENT"
	RejectPolicy     = "SMTP_REJECT_POLICY"
	RejectRateLimit  = "SMTP_REJECT_RATE_LIMIT"
	RejectTemporary  = "SMTP_REJECT_TEMPORARY"
	RejectOther      = "SMTP_REJECT_OTHER"
)

// ConnectionLevelProblems are the refusals that stop every message, not one.
var ConnectionLevelProblems = []string{RejectConnection, RejectAuth, RejectRelay}

// ConnectionLevel reports whether the refusal would hit every message.
func (r *Rejection) ConnectionLevel() bool {
	if r == nil {
		return false
	}
	for _, p := range ConnectionLevelProblems {
		if r.Problem == p {
			return true
		}
	}
	return false
}

var (
	enhancedRe  = regexp.MustCompile(`\b[245]\.\d{1,3}\.\d{1,3}\b`)
	rateRe      = regexp.MustCompile(`(?i)rate.?limit|sending rate|too many|throttl|quota exceeded|limit exceeded|exceeded .{0,30}limit`)
	relayDenyRe = regexp.MustCompile(`(?i)relay(ing)?( access)? (is )?(denied|not (permitted|allowed))|unable to relay|not (permitted|allowed) to relay`)
	senderRe    = regexp.MustCompile(`(?i)sender( address)? (rejected|refused|denied|not allowed)|not owned by|not allowed to send (as|from)|send as`)
	recipientRe = regexp.MustCompile(`(?i)recipient( address)? (rejected|refused)|user unknown|unknown user|no such (user|recipient|mailbox)|mailbox (unavailable|not found|does not exist|full)`)
	policyRe    = regexp.MustCompile(`(?i)spam|policy|block(ed|list)|blacklist|dmarc|\bspf\b|dkim|content rejected|virus|reputation`)

	addressRe = regexp.MustCompile(`[^\s<>()\[\]"',;:=]+@[^\s<>()\[\]"',;:=]+`)
	tokenRe   = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
)

// SafeReplyText removes addresses and anything long enough to be a credential
// or token from a server's reply, and bounds its length.
func SafeReplyText(s string) string {
	s = addressRe.ReplaceAllString(s, "<address>")
	s = tokenRe.ReplaceAllString(s, "<redacted>")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = strings.ToValidUTF8(s[:200], "")
	}
	return s
}

// classifyRejection names what a server refused. stage is where in the
// conversation the reply came: greeting, ehlo, tls, auth, mail, rcpt, data.
func classifyRejection(te *textproto.Error, stage string) *Rejection {
	if te.Code < 400 || te.Code > 599 {
		return nil
	}
	msg := te.Msg
	enh := enhancedRe.FindString(msg)
	class := ""
	if p := strings.Split(enh, "."); len(p) == 3 {
		class = p[1] + "." + p[2]
	}
	temp := te.Code < 500
	r := &Rejection{Code: te.Code, Reason: SafeReplyText(msg)}
	switch {
	case te.Code == 421 || stage == "greeting" || stage == "ehlo" || stage == "tls" || (stage == "auth" && temp):
		r.Problem = RejectConnection
	case stage == "auth" || te.Code == 530 || te.Code == 535:
		r.Problem = RejectAuth
	case rateRe.MatchString(msg):
		r.Problem = RejectRateLimit
	case relayDenyRe.MatchString(msg):
		r.Problem = RejectRelay
	case senderRe.MatchString(msg) || class == "1.7" || class == "1.8":
		r.Problem = RejectSender
	case recipientRe.MatchString(msg) || (stage == "rcpt" && (strings.HasPrefix(class, "1.") || strings.HasPrefix(class, "2."))):
		r.Problem = RejectRecipient
	case strings.HasPrefix(class, "7.") || policyRe.MatchString(msg):
		r.Problem = RejectPolicy
	case temp:
		r.Problem = RejectTemporary
	case stage == "rcpt":
		r.Problem = RejectRecipient
	case stage == "mail":
		r.Problem = RejectSender
	default:
		r.Problem = RejectOther
	}
	if stage == "auth" {
		// Never keep what a server says about a sign-in: only its status.
		r.Reason = strings.TrimSpace("sign-in refused " + enh)
	}
	return r
}
