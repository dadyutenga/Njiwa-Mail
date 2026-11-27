package mailer

import (
	"net/textproto"
	"strings"
	"testing"
)

func TestClassifyRejection(t *testing.T) {
	for _, c := range []struct {
		code  int
		msg   string
		stage string
		want  string
		wide  bool
	}{
		{535, "5.7.8 authentication failed", "auth", RejectAuth, true},
		{530, "5.7.0 Authentication required", "mail", RejectAuth, true},
		{454, "4.7.0 Temporary authentication failure", "auth", RejectConnection, true},
		{554, "5.7.1 no SMTP service here", "greeting", RejectConnection, true},
		{421, "4.7.0 try again later", "rcpt", RejectConnection, true},
		{554, "5.7.1 <a@b.tld>: Relay access denied", "rcpt", RejectRelay, true},
		{553, "5.7.1 <a@b.tld>: Sender address rejected: not owned by user x", "rcpt", RejectSender, false},
		{550, "5.1.8 bad sender", "mail", RejectSender, false},
		{550, "5.1.1 <a@b.tld>: Recipient address rejected: User unknown", "rcpt", RejectRecipient, false},
		{550, "5.7.1 Recipient address rejected: Access denied", "rcpt", RejectRecipient, false},
		{554, "5.7.1 message rejected as spam", "data", RejectPolicy, false},
		{550, "5.7.26 DMARC policy", "data", RejectPolicy, false},
		{450, "4.7.1 you are sending too many messages", "rcpt", RejectRateLimit, false},
		{451, "4.3.0 try later", "data", RejectTemporary, false},
		{552, "5.3.4 message too big", "data", RejectOther, false},
	} {
		r := classifyRejection(&textproto.Error{Code: c.code, Msg: c.msg}, c.stage)
		if r == nil || r.Problem != c.want || r.Code != c.code || r.ConnectionLevel() != c.wide {
			t.Errorf("%d %q at %s: %+v, want %s", c.code, c.msg, c.stage, r, c.want)
		}
	}
	if r := classifyRejection(&textproto.Error{Code: 250, Msg: "ok"}, "data"); r != nil {
		t.Errorf("an acceptance is not a refusal: %+v", r)
	}
}

func TestRefusalReasonIsSafe(t *testing.T) {
	secret := strings.Repeat("QUJD", 12)
	r := classifyRejection(&textproto.Error{Code: 550, Msg: "5.7.1 <ada@example.org> blocked, ref " + secret + "\r\n" + strings.Repeat("x ", 300)}, "data")
	if strings.Contains(r.Reason, "ada@") || strings.Contains(r.Reason, secret) || strings.ContainsAny(r.Reason, "\r\n") || len(r.Reason) > 200 {
		t.Fatalf("%q", r.Reason)
	}
	r = classifyRejection(&textproto.Error{Code: 535, Msg: "5.7.8 bad password hunter2 for user bob"}, "auth")
	if r.Reason != "sign-in refused 5.7.8" {
		t.Fatalf("sign-in reply kept: %q", r.Reason)
	}
}
