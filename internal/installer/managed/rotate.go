package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/njiwa-tz/mail/internal/dnsauth"
)

// DKIM key rotation (backlog D10/E11), in stages so mail is never signed with
// a key receivers cannot find:
//
//	prepare  generate the new selector's keys; signing is unchanged
//	activate only when DNS publishes every new key: switch signing (a normal
//	         plan/approve/apply, with backup, receipt and rollback)
//	retire   after messages signed with the old key have been delivered,
//	         delete the old private keys (never while still signing with them)

// Rotation is the state between prepare and activate.
type Rotation struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Domains    []string          `json:"domains"`
	Records    map[string]string `json:"records"` // domain -> TXT value for <to>._domainkey.<domain>
	PreparedAt time.Time         `json:"prepared_at"`
}

func (p Params) rotationFile() string {
	p.defaults()
	return filepath.Join(p.Root, p.StateDir, "rotation.json")
}

// defaults fills the root and state directory when a caller (status, retire,
// cancel) did not go through BuildPlan.
func (p *Params) defaults() {
	if p.Root == "" {
		p.Root = "/"
	}
	if p.StateDir == "" {
		p.StateDir = "/var/lib/njiwa-mail/installer"
	}
}

// CurrentSelector is the selector the signer uses now, read from its key table.
func CurrentSelector(p Params) (string, error) {
	p.defaults()
	b, err := os.ReadFile(p.path(dkimTables + "/key.table"))
	if err != nil {
		return "", errors.New("no DKIM key table: run installer apply first")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		// "<sel>._domainkey.<domain> <domain>:<sel>:<path>"
		if f := strings.Fields(line); len(f) == 2 {
			if parts := strings.Split(f[1], ":"); len(parts) == 3 {
				return parts[1], nil
			}
		}
	}
	return "", errors.New("the DKIM key table lists no key")
}

// PrepareRotation generates keys for the new selector for every domain. Keys
// that already exist are kept, so prepare can be re-run safely.
func PrepareRotation(p Params, to string, now time.Time) (*Rotation, error) {
	if err := p.normalise(); err != nil {
		return nil, err
	}
	if !selRe(to) {
		return nil, errors.New("the new selector may contain only letters, digits and hyphens")
	}
	from, err := CurrentSelector(p)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("%s is already the signing selector; choose a new one (for example %s-2)", to, to)
	}
	rot := &Rotation{From: from, To: to, Domains: p.Domains, Records: map[string]string{}, PreparedAt: now.UTC()}
	q := p
	q.Selector = to
	for _, d := range p.Domains {
		txt, err := ensureDKIM(q, d)
		if err != nil {
			return nil, err
		}
		rot.Records[d] = txt
	}
	b, _ := json.MarshalIndent(rot, "", "  ")
	if err := writeFileAtomic(p.rotationFile(), b, 0o600); err != nil {
		return nil, err
	}
	return rot, nil
}

// LoadRotation reads the prepared rotation.
func LoadRotation(p Params) (*Rotation, error) {
	p.defaults()
	b, err := os.ReadFile(p.rotationFile())
	if err != nil {
		return nil, errors.New("no rotation is prepared: run installer dkim-rotate prepare first")
	}
	var r Rotation
	return &r, json.Unmarshal(b, &r)
}

// RotationDNSProblems lists, per domain, why the new key is not yet usable:
// missing, more than one record, or a different key than the one prepared.
// An empty result means activation is safe.
func RotationDNSProblems(ctx context.Context, r dnsauth.TXTResolver, rot *Rotation) []string {
	var out []string
	for _, d := range rot.Domains {
		want, err := dnsauth.ParseDKIMKey(rot.Records[d])
		if err != nil {
			out = append(out, d+": the prepared key cannot be read")
			continue
		}
		host := rot.To + "._domainkey." + d
		txt, _ := r.LookupTXT(ctx, host)
		switch {
		case len(txt) == 0:
			out = append(out, host+" is not published yet")
		case len(txt) > 1:
			out = append(out, host+" has more than one TXT record")
		default:
			got, err := dnsauth.ParseDKIMKey(txt[0])
			if err != nil || got.Fingerprint != want.Fingerprint {
				out = append(out, host+" does not publish the prepared key (or it was cut off when pasted)")
			}
		}
	}
	sort.Strings(out)
	return out
}

// FinishRotation clears the prepared state after a successful activation.
func FinishRotation(p Params) error {
	p.defaults()
	err := os.Remove(p.rotationFile())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RetireSelector deletes a selector's private keys. It refuses while the
// signer still uses that selector.
func RetireSelector(p Params, sel string) ([]string, error) {
	p.defaults()
	if !selRe(sel) {
		return nil, errors.New("invalid selector")
	}
	cur, err := CurrentSelector(p)
	if err != nil {
		return nil, err
	}
	if cur == sel {
		return nil, fmt.Errorf("%s is the signing selector; activate a new one before retiring it", sel)
	}
	matches, _ := filepath.Glob(p.path(filepath.Join(keyDir, "*", sel+".private")))
	var removed []string
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return removed, err
		}
		removed = append(removed, strings.TrimPrefix(m, strings.TrimSuffix(p.Root, "/")))
	}
	sort.Strings(removed)
	return removed, nil
}

// CancelRotation abandons a prepared rotation: the new selector's keys are
// deleted (never while signing with them) and the state is cleared.
func CancelRotation(p Params) (*Rotation, error) {
	p.defaults()
	rot, err := LoadRotation(p)
	if err != nil {
		return nil, err
	}
	if p.Root == "" {
		p.Root = "/"
	}
	if cur, err := CurrentSelector(p); err == nil && cur == rot.To {
		return nil, fmt.Errorf("%s is already the signing selector; the rotation was activated", rot.To)
	}
	if _, err := RetireSelector(p, rot.To); err != nil {
		return nil, err
	}
	return rot, FinishRotation(p)
}
