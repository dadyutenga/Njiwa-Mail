package managed

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Runner executes a host command. Only the fixed list of commands below is
// ever run; nothing comes from user input.
type Runner interface {
	Run(name string, args ...string) (string, error)
}

type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type ReceiptFile struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Backup  string `json:"backup,omitempty"`
}

// Receipt is the durable evidence of one apply. It holds paths, digests and
// public data only: never private keys or script bodies.
type Receipt struct {
	ID                string            `json:"id"`
	CreatedAt         time.Time         `json:"created_at"`
	PlanDigest        string            `json:"plan_digest"`
	Params            Params            `json:"params"`
	Files             []ReceiptFile     `json:"files"`
	Steps             []Step            `json:"steps"`
	Result            string            `json:"result"` // ok | failed_rolled_back
	RollbackState     string            `json:"rollback_state"`
	BackupDir         string            `json:"backup_dir"`
	DKIMRecords       map[string]string `json:"dkim_records"` // domain -> TXT value at <selector>._domainkey.<domain>
	ClientFingerprint string            `json:"client_fingerprint"`
	PostfixVersion    string            `json:"postfix_version,omitempty"`
}

// ErrDigestMismatch means the host or the parameters changed after review.
var ErrDigestMismatch = errors.New("the plan changed since it was reviewed; run plan again and approve the new digest")

func (p Params) receiptDir() string { return filepath.Join(p.Root, p.StateDir, "receipts") }

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".njiwa-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	return writeFileAtomic(dst, b, st.Mode().Perm())
}

// chownTo gives a file to a system user when it exists (opendkim must read
// its keys); on hosts without that user it is left to root.
func chownTo(path, name string) {
	u, err := user.Lookup(name)
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	_ = os.Chown(path, uid, gid)
}

// Apply executes an approved plan. approved must equal the digest of a plan
// rebuilt now; any drift is refused. On a validation or reload failure every
// file is restored from the backup and the receipt says so.
func Apply(p Params, approved string, run Runner, now time.Time) (*Receipt, error) {
	pl, err := BuildPlan(p)
	if err != nil {
		return nil, err
	}
	p = pl.Params
	if approved == "" || approved != pl.Digest {
		return nil, ErrDigestMismatch
	}
	if pl.Conflict != "" && !p.Adopt {
		return nil, fmt.Errorf("refusing to continue: %s. Pass -adopt to replace it (the current files are backed up first)", pl.Conflict)
	}
	id := now.UTC().Format("20060102T150405Z")
	rc := &Receipt{ID: id, CreatedAt: now.UTC(), PlanDigest: pl.Digest, Params: p, RollbackState: "available",
		BackupDir: filepath.Join(p.StateDir, "backups", id), DKIMRecords: map[string]string{}}

	// Every path that may change, including generated secrets.
	targets := []string{}
	for _, f := range pl.Files {
		targets = append(targets, f.Path)
	}
	targets = append(targets, njDir+"/ca.crt", njDir+"/ca.key", njDir+"/server.crt", njDir+"/server.key",
		njDir+"/relay_clientcerts", njDir+"/relay_clientcerts.db", njDir+"/senders.db", njDir+"/transport.db",
		clientDir+"/client.crt", clientDir+"/client.key", clientDir+"/ca.crt")
	for _, d := range p.Domains {
		targets = append(targets, dkimKeyPath(d, p.Selector))
	}
	sort.Strings(targets)
	for _, t := range targets {
		rf := ReceiptFile{Path: t}
		if _, err := os.Stat(p.path(t)); err == nil {
			rf.Existed = true
			rf.Backup = filepath.Join(rc.BackupDir, t)
			if err := copyFile(p.path(t), p.path(rf.Backup)); err != nil {
				return nil, fmt.Errorf("backup of %s failed, nothing was changed: %w", t, err)
			}
		}
		rc.Files = append(rc.Files, rf)
	}
	step := func(name string, err error, detail string) bool {
		s := Step{Name: name, OK: err == nil, Detail: detail}
		if err != nil {
			s.Detail = strings.TrimSpace(detail + " " + err.Error())
		}
		rc.Steps = append(rc.Steps, s)
		return err == nil
	}
	fail := func() (*Receipt, error) {
		restore(p, rc, run)
		rc.Result, rc.RollbackState = "failed_rolled_back", "rolled_back"
		_ = saveReceipt(p, rc)
		last := rc.Steps[len(rc.Steps)-1]
		return rc, fmt.Errorf("%s failed (%s); every change was rolled back", last.Name, last.Detail)
	}

	if !step("generate certificates", ensurePKI(p, now), "") {
		return fail()
	}
	for _, d := range p.Domains {
		txt, err := ensureDKIM(p, d)
		if !step("DKIM key for "+d, err, "") {
			return fail()
		}
		rc.DKIMRecords[d] = txt
	}
	clientPEM, err := os.ReadFile(p.path(clientDir + "/client.crt"))
	if err == nil {
		rc.ClientFingerprint, err = Fingerprint(clientPEM)
	}
	if !step("client certificate fingerprint", err, "") {
		return fail()
	}
	files := append([]File{}, pl.Files...)
	files = append(files, File{Path: njDir + "/relay_clientcerts", Mode: 0644, Content: []byte(Marker + "\n" + rc.ClientFingerprint + " njiwa-worker\n")})
	for _, f := range files {
		if !step("write "+f.Path, writeFileAtomic(p.path(f.Path), f.Content, f.Mode), "") {
			return fail()
		}
	}
	for _, t := range []string{dkimTables + "/key.table", dkimTables + "/signing.table", dkimTables + "/trusted.hosts"} {
		chownTo(p.path(t), "opendkim")
	}
	for _, m := range []string{"senders", "transport", "relay_clientcerts"} {
		out, err := run.Run("postmap", "hash:"+p.path(njDir+"/"+m))
		if !step("postmap "+m, err, out) {
			return fail()
		}
	}
	out, err := run.Run("postfix", "check")
	if !step("postfix check", err, out) {
		return fail()
	}
	out, err = run.Run("opendkim", "-n", "-x", p.path(dkimConf))
	if !step("opendkim configuration check", err, out) {
		return fail()
	}
	if v, err := run.Run("postconf", "-h", "mail_version"); err == nil {
		rc.PostfixVersion = strings.TrimSpace(v)
	}
	if !step("reload services", reload(run), "") {
		return fail()
	}
	rc.Result = "ok"
	if err := saveReceipt(p, rc); err != nil {
		return rc, err
	}
	return rc, nil
}

// reload starts or reloads Postfix and restarts OpenDKIM. With systemd it
// uses the units; without (containers) it uses the programs directly.
func reload(run Runner) error {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		if out, err := run.Run("systemctl", "enable", "--now", "opendkim"); err != nil {
			return fmt.Errorf("opendkim: %s %w", out, err)
		}
		if out, err := run.Run("systemctl", "restart", "opendkim"); err != nil {
			return fmt.Errorf("opendkim: %s %w", out, err)
		}
		if out, err := run.Run("systemctl", "enable", "--now", "postfix"); err != nil {
			return fmt.Errorf("postfix: %s %w", out, err)
		}
		if out, err := run.Run("systemctl", "reload-or-restart", "postfix"); err != nil {
			return fmt.Errorf("postfix: %s %w", out, err)
		}
		return nil
	}
	if _, err := run.Run("postfix", "status"); err == nil {
		if out, err := run.Run("postfix", "reload"); err != nil {
			return fmt.Errorf("postfix reload: %s %w", out, err)
		}
	} else if out, err := run.Run("postfix", "start"); err != nil {
		return fmt.Errorf("postfix start: %s %w", out, err)
	}
	return nil
}

func ensurePKI(p Params, now time.Time) error {
	caCrt, caKeyPath := p.path(njDir+"/ca.crt"), p.path(njDir+"/ca.key")
	if _, err := os.Stat(caCrt); err != nil {
		ca, _, _, err := newCA(now)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(caCrt, ca.CertPEM, 0o644); err != nil {
			return err
		}
		if err := writeFileAtomic(caKeyPath, ca.KeyPEM, 0o600); err != nil {
			return err
		}
	}
	cb, err := os.ReadFile(caCrt)
	if err != nil {
		return err
	}
	kb, err := os.ReadFile(caKeyPath)
	if err != nil {
		return err
	}
	ca, caKey, err := parseCA(cb, kb)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.path(njDir + "/server.crt")); err != nil {
		host, _, _ := strings.Cut(p.SubmissionBind, ":")
		host = strings.Trim(host, "[]")
		srv, err := issue(ca, caKey, p.Hostname, []string{p.Hostname, "localhost"}, parseIPs(host, "127.0.0.1"), false, now)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(p.path(njDir+"/server.crt"), srv.CertPEM, 0o644); err != nil {
			return err
		}
		if err := writeFileAtomic(p.path(njDir+"/server.key"), srv.KeyPEM, 0o600); err != nil {
			return err
		}
	}
	if _, err := os.Stat(p.path(clientDir + "/client.crt")); err != nil {
		cl, err := issue(ca, caKey, "njiwa-worker", nil, nil, true, now)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(p.path(clientDir+"/client.crt"), cl.CertPEM, 0o644); err != nil {
			return err
		}
		if err := writeFileAtomic(p.path(clientDir+"/client.key"), cl.KeyPEM, 0o600); err != nil {
			return err
		}
	}
	return writeFileAtomic(p.path(clientDir+"/ca.crt"), cb, 0o644)
}

// ensureDKIM keeps an existing key (rotation is a separate, deliberate step)
// and returns the TXT value for the key actually used.
func ensureDKIM(p Params, domain string) (string, error) {
	path := p.path(dkimKeyPath(domain, p.Selector))
	if b, err := os.ReadFile(path); err == nil {
		if err := ensureKeyDirs(p, domain); err != nil {
			return "", err
		}
		chownTo(path, "opendkim")
		return dkimTXT(b)
	}
	priv, txt, err := newDKIMKey()
	if err != nil {
		return "", err
	}
	if err := ensureKeyDirs(p, domain); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, priv, 0o600); err != nil {
		return "", err
	}
	chownTo(path, "opendkim")
	return txt, nil
}

// ensureKeyDirs makes the key directories traversable by the signer and no
// one else: /etc/opendkim 0755, /etc/opendkim/keys and each domain directory
// 0750 owned by the opendkim user. It runs on every apply, so a host whose
// permissions drifted is repaired too.
func ensureKeyDirs(p Params, domain string) error {
	if err := os.MkdirAll(p.path(dkimDir), 0o755); err != nil {
		return err
	}
	if err := os.Chmod(p.path(dkimDir), 0o755); err != nil {
		return err
	}
	for _, d := range []string{keyDir, filepath.Join(keyDir, domain)} {
		if err := os.MkdirAll(p.path(d), 0o750); err != nil {
			return err
		}
		if err := os.Chmod(p.path(d), 0o750); err != nil {
			return err
		}
		chownTo(p.path(d), "opendkim")
	}
	return nil
}

func restore(p Params, rc *Receipt, run Runner) {
	for _, f := range rc.Files {
		if f.Existed {
			_ = copyFile(p.path(f.Backup), p.path(f.Path))
		} else {
			_ = os.Remove(p.path(f.Path))
		}
	}
	_ = reload(run)
}

func saveReceipt(p Params, rc *Receipt) error {
	b, _ := json.MarshalIndent(rc, "", "  ")
	return writeFileAtomic(filepath.Join(p.receiptDir(), rc.ID+".json"), b, 0o600)
}

// LoadReceipt reads one receipt; id "" means the latest.
func LoadReceipt(p Params, id string) (*Receipt, error) {
	if p.StateDir == "" {
		p.StateDir = "/var/lib/njiwa-mail/installer"
	}
	if p.Root == "" {
		p.Root = "/"
	}
	if id == "" {
		es, err := os.ReadDir(p.receiptDir())
		if err != nil || len(es) == 0 {
			return nil, errors.New("no installer receipt found")
		}
		names := []string{}
		for _, e := range es {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
		sort.Strings(names)
		id = names[len(names)-1]
	}
	b, err := os.ReadFile(filepath.Join(p.receiptDir(), filepath.Base(id)+".json"))
	if err != nil {
		return nil, err
	}
	var rc Receipt
	return &rc, json.Unmarshal(b, &rc)
}

// Rollback restores the files an apply changed, from its backup.
func Rollback(p Params, id string, run Runner) (*Receipt, error) {
	rc, err := LoadReceipt(p, id)
	if err != nil {
		return nil, err
	}
	if rc.RollbackState == "rolled_back" {
		return rc, errors.New("this apply was already rolled back")
	}
	rp := rc.Params
	rp.Root = p.Root
	restore(rp, rc, run)
	rc.RollbackState = "rolled_back"
	return rc, saveReceipt(rp, rc)
}

func parseIPs(vals ...string) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	for _, v := range vals {
		if ip := net.ParseIP(v); ip != nil && !seen[ip.String()] {
			seen[ip.String()] = true
			out = append(out, ip)
		}
	}
	return out
}
