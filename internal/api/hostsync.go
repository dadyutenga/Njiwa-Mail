package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/mtaagent"
	"github.com/njiwa-tz/mail/internal/service"
)

// What the API does on its own with the host agent: record an install or a
// rollback that finished (even when nobody is watching the page), and give a
// newly verified domain its DKIM key. Both need an agent that allows
// installing; the second only adds keys to an installation the installer made.

// recordHostJob stores the result of a finished job. It reports whether the job
// is now on record. Recording is idempotent.
func (s *Server) recordHostJob(ctx context.Context, actor service.Actor, approvedBy string, j *mtaagent.InstallJob) (bool, error) {
	rc := j.Receipt
	if rc == nil || j.State != "done" {
		return false, nil
	}
	// The page and WatchHost may see the same finished job at the same moment.
	s.hostRecord.Lock()
	defer s.hostRecord.Unlock()
	switch {
	case j.Kind == "apply" && rc.Result == "ok" && j.Material != nil:
		return true, s.Svc.RecordInstallOnce(ctx, actor, service.ManagedInstall{ReceiptID: rc.ID, PlanDigest: rc.PlanDigest,
			ApprovedBy: approvedBy, BackupRef: rc.BackupDir, PostfixVersion: rc.PostfixVersion, ServerName: rc.Params.Hostname,
			Selector: rc.Params.Selector, DKIMRecords: rc.DKIMRecords, ClientCertPEM: j.Material.ClientCert,
			ClientKeyPEM: j.Material.ClientKey, CAPEM: j.Material.CA}, rc.Params.SubmissionBind)
	case j.Kind == "rollback":
		return true, s.Svc.RecordRollbackOnce(ctx, actor, rc.PlanDigest, j.ID)
	}
	return false, nil
}

// domainRetry is how long a failed automatic key creation waits.
const domainRetry = 30 * time.Minute

// SyncHost runs one round of the automatic host work.
func (s *Server) SyncHost(ctx context.Context) error {
	installer, ok := s.MTA.(hostInstaller)
	if !ok {
		return nil
	}
	job, err := installer.InstallJob(ctx)
	if err != nil {
		return nil // no agent, or one that may not change the host: nothing to do
	}
	system := service.Actor{Name: "host-agent"}
	approver := ""
	if job.Receipt != nil {
		approver = s.Svc.InstallApprover(ctx, job.Receipt.PlanDigest)
	}
	if approver == "" {
		approver = system.Name
	}
	if _, err := s.recordHostJob(ctx, system, approver, job); err != nil {
		return err
	}
	if job.State == "running" {
		return nil
	}
	agent, err := s.domainAgent()
	if err != nil {
		return nil
	}
	installed, err := s.Svc.ManagedInstalled(ctx)
	if err != nil || !installed {
		return err
	}
	missing, err := s.Svc.DomainsMissingKey(ctx)
	if err != nil || len(missing) == 0 {
		return err
	}
	key := strings.Join(missing, ",")
	s.hostMu.Lock()
	wait := s.hostFailed == key && time.Since(s.hostFailedAt) < domainRetry
	s.hostMu.Unlock()
	if wait {
		return nil
	}
	fail := func(err error) error {
		s.hostMu.Lock()
		s.hostFailed, s.hostFailedAt = key, time.Now()
		s.hostMu.Unlock()
		return err
	}
	plan, err := agent.PlanDomains(ctx, missing)
	if err != nil {
		return fail(err)
	}
	res, err := agent.ApplyDomains(ctx, missing, plan.Digest)
	if err != nil {
		return fail(err)
	}
	if err := s.Svc.RecordManagedDomainKeys(ctx, system, res.ReceiptID, res.Digest, res.Selector, res.DKIMRecords); err != nil {
		return fail(err)
	}
	slog.Info("created DKIM keys for newly verified domains", "domains", key)
	return nil
}

// WatchHost runs SyncHost until ctx ends, logging each new problem once.
func (s *Server) WatchHost(ctx context.Context, every time.Duration) {
	last := ""
	for {
		if err := s.SyncHost(ctx); err != nil && ctx.Err() == nil {
			if msg := err.Error(); msg != last {
				slog.Warn("automatic mail host work failed", "err", err)
				last = msg
			}
		} else {
			last = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
