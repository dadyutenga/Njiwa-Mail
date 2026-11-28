package mtaagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/njiwa-tz/mail/internal/installer/managed"
)

type fakeInstaller struct {
	release chan struct{}
	fail    bool
}

func (f *fakeInstaller) Plan(r InstallRequest) (*InstallPlan, error) {
	return &InstallPlan{Digest: "abcdef0123456789", Summary: "install postfix for " + r.Hostname}, nil
}

func (f *fakeInstaller) Apply(r InstallRequest, log func(string)) (*managed.Receipt, *InstallMaterial, error) {
	log("$ apt-get install -y postfix")
	<-f.release
	if r.Digest != "abcdef0123456789" {
		return nil, nil, managed.ErrDigestMismatch
	}
	if f.fail {
		return &managed.Receipt{ID: "r2", Result: "failed_rolled_back"}, nil, errors.New("postfix check failed")
	}
	return &managed.Receipt{ID: "r1", Result: "ok"}, &InstallMaterial{ClientCert: []byte("cert")}, nil
}

func (f *fakeInstaller) Rollback(r InstallRequest, log func(string)) (*managed.Receipt, error) {
	log("rolled back " + r.ReceiptID)
	return &managed.Receipt{ID: r.ReceiptID, Result: "ok", RollbackState: "rolled_back"}, nil
}

func installClient(t *testing.T, a *Agent) *Client {
	t.Helper()
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	c := NewClient("unused", a.Token)
	c.http = &http.Client{Transport: rewrite{srv.URL}}
	return c
}

type rewrite struct{ to string }

func (r rewrite) RoundTrip(q *http.Request) (*http.Response, error) {
	q.URL.Scheme, q.URL.Host = "http", strings.TrimPrefix(r.to, "http://")
	return http.DefaultTransport.RoundTrip(q)
}

func finished(t *testing.T, c *Client) *InstallJob {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		j, err := c.InstallJob(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if j.State != "running" {
			return j
		}
	}
	t.Fatal("the job did not finish")
	return nil
}

func TestInstallIsOffUnlessAllowed(t *testing.T) {
	c := installClient(t, &Agent{Token: "tok", Runner: &fakeRunner{}})
	ctx := context.Background()
	_, planErr := c.InstallPlan(ctx, InstallRequest{})
	_, jobErr := c.InstallJob(ctx)
	for _, err := range []error{planErr, jobErr, c.InstallApply(ctx, InstallRequest{Digest: "x"}), c.InstallRollback(ctx, InstallRequest{})} {
		var ae *AgentError
		if !errors.As(err, &ae) || ae.Code != "host_install_disabled" {
			t.Errorf("%v", err)
		}
	}
}

func TestInstallPlanApplyFailureAndRollback(t *testing.T) {
	f := &fakeInstaller{release: make(chan struct{})}
	c := installClient(t, &Agent{Token: "tok", Runner: &fakeRunner{}, Installer: f})
	ctx := context.Background()
	pl, err := c.InstallPlan(ctx, InstallRequest{Hostname: "mail.example.com"})
	if err != nil || pl.Digest == "" || !strings.Contains(pl.Summary, "mail.example.com") {
		t.Fatalf("%+v %v", pl, err)
	}
	if err := c.InstallApply(ctx, InstallRequest{}); err == nil {
		t.Fatal("apply without an approved digest")
	}
	if err := c.InstallApply(ctx, InstallRequest{Digest: pl.Digest}); err != nil {
		t.Fatal(err)
	}
	// Progress is visible while it runs, and a second job is refused.
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		j, err := c.InstallJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == "running" && len(j.Lines) == 1 {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("no progress: %+v", j)
		}
	}
	var ae *AgentError
	if err := c.InstallRollback(ctx, InstallRequest{}); !errors.As(err, &ae) || ae.Code != "install_running" {
		t.Fatalf("second job: %v", err)
	}
	close(f.release)
	if j := finished(t, c); j.State != "done" || j.Receipt.ID != "r1" || j.Material == nil {
		t.Fatalf("%+v", j)
	}
	f.fail = true
	if err := c.InstallApply(ctx, InstallRequest{Digest: pl.Digest}); err != nil {
		t.Fatal(err)
	}
	if j := finished(t, c); j.State != "failed" || j.Material != nil || j.Receipt.Result != "failed_rolled_back" || !strings.Contains(j.Error, "postfix check") {
		t.Fatalf("%+v", j)
	}
	if err := c.InstallRollback(ctx, InstallRequest{ReceiptID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if j := finished(t, c); j.State != "done" || j.Kind != "rollback" || j.Receipt.RollbackState != "rolled_back" {
		t.Fatalf("%+v", j)
	}
}
