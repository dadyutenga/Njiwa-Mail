package managed

import (
	"errors"
	"os"
	"time"
)

// DomainManager expands an already-owned installation. HTTP callers cannot
// choose paths, endpoints, selectors, firewall policy, packages or adoption.
// Existing domains remain until an explicit host maintenance operation removes them.
type DomainManager struct {
	Root   string
	Runner Runner
}

type DomainResult struct {
	ReceiptID   string            `json:"receipt_id"`
	Digest      string            `json:"digest"`
	Selector    string            `json:"selector"`
	DKIMRecords map[string]string `json:"dkim_records"`
}

func (m *DomainManager) params(domains []string) (Params, *Receipt, error) {
	rc, err := LoadReceipt(Params{Root: m.Root}, "")
	if err != nil {
		return Params{}, nil, err
	}
	if rc.Result != "ok" || rc.RollbackState == "rolled_back" {
		return Params{}, nil, errors.New("managed installation is not owned and healthy")
	}
	p := rc.Params
	p.Root = m.Root
	if p.Root == "" {
		p.Root = "/"
	}
	p.Adopt, p.RenewCerts, p.RemovePackages = false, false, false
	p.Now = time.Time{}
	p.Domains = append(append([]string{}, p.Domains...), domains...)
	for _, f := range []string{pfDir + "/main.cf", pfDir + "/master.cf", dkimConf} {
		b, err := os.ReadFile(p.path(f))
		if err != nil || len(b) < len(Marker) || string(b[:len(Marker)]) != Marker {
			return Params{}, nil, errors.New("managed configuration ownership could not be established")
		}
	}
	return p, rc, nil
}

func (m *DomainManager) Plan(domains []string) (*Plan, error) {
	p, _, err := m.params(domains)
	if err != nil {
		return nil, err
	}
	pl, err := BuildPlan(p)
	if err != nil {
		return nil, err
	}
	if pl.Conflict != "" || len(pl.Blockers) > 0 || len(pl.MissingPackages) > 0 || len(pl.EnableRepos) > 0 {
		return nil, errors.New("host maintenance is required; domain onboarding cannot adopt services or install packages")
	}
	return pl, nil
}

func (m *DomainManager) Apply(domains []string, approved string) (*DomainResult, error) {
	if approved == "" {
		return nil, ErrDigestMismatch
	}
	_, rc, err := m.params(domains)
	if err != nil {
		return nil, err
	}
	// Lost HTTP responses are recoverable using the durable host receipt.
	if rc.PlanDigest != approved {
		pl, err := m.Plan(domains)
		if err != nil {
			return nil, err
		}
		if pl.Digest != approved {
			return nil, ErrDigestMismatch
		}
		rc, err = Apply(pl.Params, approved, m.Runner, time.Now())
		if err != nil {
			return nil, err
		}
	}
	return &DomainResult{ReceiptID: rc.ID, Digest: rc.PlanDigest, Selector: rc.Params.Selector, DKIMRecords: rc.DKIMRecords}, nil
}
