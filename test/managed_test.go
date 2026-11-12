package test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installation"
	"github.com/njiwa-tz/mail/internal/service"
)

func pemPair(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	e := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	e(err)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	e(err)
	kd, err := x509.MarshalPKCS8PrivateKey(k)
	e(err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
}

// E02-E09 service side: a recorded install makes managed mode installed, the
// worker's mTLS settings load from the sealed profile, the Domains page shows
// the signer's real DKIM key, and activation still needs a fresh check.
func TestManagedInstallRecordingAndActivation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	dkim := dkimRecord(t)
	in := service.ManagedInstall{ReceiptID: "r1", PlanDigest: "abc123", ApprovedBy: "cli", PostfixVersion: "3.8.6",
		SubmissionHost: "127.0.0.1", SubmissionPort: 10587, ServerName: "mailout.example.com", Selector: "dkim",
		DKIMRecords: map[string]string{"notify.example.com": dkim}, ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca}
	bad := in
	bad.ClientKeyPEM = []byte("nope")
	if err := e.svc.RecordManagedInstall(ctx, service.CLIActor, bad); errCodeOf(err) != "invalid_request" {
		t.Fatalf("mismatched key accepted: %v", err)
	}
	e.must(e.svc.RecordManagedInstall(ctx, service.CLIActor, in))
	if n := e.count(`SELECT count(*) FROM transport_profiles WHERE mode='managed_mta' AND encrypted_secret::text LIKE '%PRIVATE KEY%'`); n != 0 {
		t.Fatal("client key stored in clear")
	}
	if e.count(`SELECT count(*) FROM installer_receipts WHERE plan_digest='abc123' AND result_code='ok' AND verified_at IS NOT NULL`) != 1 ||
		e.count(`SELECT count(*) FROM audit_log WHERE action='installer.apply'`) != 1 {
		t.Fatal("receipt or audit missing")
	}
	steps := map[string]installation.Step{}
	for _, s := range e.readiness().Steps {
		steps[s.ID] = s
	}
	if steps["install"].Status != installation.Ready || steps["verify_transport"].Status == installation.Ready {
		t.Fatalf("after install: %+v", steps)
	}
	// The connection test for the managed profile uses the mTLS settings.
	var id string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM transport_profiles WHERE mode='managed_mta'`).Scan(&id))
	cfg, _, rev, err := e.svc.ProfileSMTP(ctx, id)
	if err != nil || cfg.ClientCert == nil || cfg.RootCAs == nil || cfg.ServerName != "mailout.example.com" || cfg.Username != "" || cfg.Port != 10587 {
		t.Fatalf("managed SMTP settings: %+v %v", cfg, err)
	}
	// Activation needs a fresh passing connection check for this revision.
	if _, err := e.svc.ActivateTransport(ctx, service.CLIActor, id, false); errCodeOf(err) != "profile_not_verified" {
		t.Fatalf("activated without a check: %v", err)
	}
	e.must(e.svc.RecordInstallationCheck(ctx, id, rev, "connection", true, "", nil, false))
	if _, err := e.svc.ActivateTransport(ctx, service.CLIActor, id, false); err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.LoadActiveSMTP(ctx)
	if err != nil || !a.OK || a.Cfg.ClientCert == nil || a.ProfileID != id {
		t.Fatalf("worker settings: %+v %v", a, err)
	}
	// Domains page: the expected DKIM record is the signer's real key, not a template.
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "m"))
	d := e.admOK(admin, "POST", wsPath("m", "/domains"), map[string]any{"name": "notify.example.com", "purpose": "sending"})
	rec := recordByKey(e.admOK(admin, "GET", wsPath("m", "/domains/"+str(d.JSON, "id")), nil), "dkim")
	if rec["placeholder"] == true || str(rec, "value") != dkim {
		t.Fatalf("managed DKIM record: %v", rec)
	}
	// A different published key fails the check.
	dns := newDNS()
	dns.txt["dkim._domainkey.notify.example.com"] = []string{dkimRecord(t) + "x"}
	e.srv.Resolver = dns
	if r := recordByKey(e.admOK(admin, "POST", wsPath("m", "/domains/"+str(d.JSON, "id")+"/check"), nil), "dkim"); r["pass"] == true || !strings.Contains(str(r, "hint"), "not valid") {
		t.Fatalf("wrong key passed: %v", r)
	}
	// Rollback: managed is no longer installed.
	e.must(e.svc.RecordManagedRollback(ctx, service.CLIActor, "abc123"))
	for _, s := range e.readiness().Steps {
		if s.ID == "install" && s.Status == installation.Ready {
			t.Fatal("still installed after rollback")
		}
	}
}

// After a key rotation the managed profile records the new selector; the
// Domains page must expect the key there, not at DKIM_SELECTOR.
func TestManagedSelectorFollowsRotation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	owner := e.user("owner@njiwa.co.tz", "owner")
	e.admOK(owner, "PUT", "/admin/v1/installation/wizard", map[string]any{"selected_mode": "managed_mta"})
	cert, key := pemPair(t, "njiwa-worker")
	ca, _ := pemPair(t, "ca")
	dkim := dkimRecord(t)
	e.must(e.svc.RecordManagedInstall(ctx, service.CLIActor, service.ManagedInstall{PlanDigest: "d1", SubmissionHost: "127.0.0.1", SubmissionPort: 10587,
		ServerName: "mailout.example.com", Selector: "rot2", DKIMRecords: map[string]string{"notify.example.com": dkim},
		ClientCertPEM: cert, ClientKeyPEM: key, CAPEM: ca}))
	var id string
	e.must(e.pool.QueryRow(ctx, `SELECT id::text FROM transport_profiles WHERE mode='managed_mta'`).Scan(&id))
	_, _, rev, err := e.svc.ProfileSMTP(ctx, id)
	e.must(err)
	e.must(e.svc.RecordInstallationCheck(ctx, id, rev, "connection", true, "", nil, false))
	_, err = e.svc.ActivateTransport(ctx, service.CLIActor, id, false)
	e.must(err)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.must(e.svc.CreateProject(ctx, "rot"))
	d := e.admOK(admin, "POST", wsPath("rot", "/domains"), map[string]any{"name": "notify.example.com", "purpose": "sending"})
	det := e.admOK(admin, "GET", wsPath("rot", "/domains/"+str(d.JSON, "id")), nil)
	rec := recordByKey(det, "dkim")
	if str(rec, "host") != "rot2._domainkey.notify.example.com" || str(rec, "value") != dkim || det.JSON["dkim_selector"] != "rot2" {
		t.Fatalf("selector after rotation: %v %v", rec, det.JSON["dkim_selector"])
	}
}
