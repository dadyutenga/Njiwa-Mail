package managed

import (
	"bytes"
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
	"syscall"
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
	// InstalledPackages are the packages this apply installed (rollback can
	// remove exactly these with -remove-packages).
	InstalledPackages []string `json:"installed_packages,omitempty"`
}

// ErrBlocked means the host runs a mail service the installer must not touch.
var ErrBlocked = errors.New("the managed mail service cannot be installed on this host")

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
	if p.Now.IsZero() {
		p.Now = now
	}
	pl, err := BuildPlan(p)
	if err != nil {
		return nil, err
	}
	p = pl.Params
	if approved == "" || approved != pl.Digest {
		return nil, ErrDigestMismatch
	}
	if len(pl.Blockers) > 0 {
		var why []string
		for _, b := range pl.Blockers {
			why = append(why, b.Detail)
		}
		return nil, fmt.Errorf("%w: %s Nothing was changed. Use this host's mail service, or another one, as an existing mail server (njiwa-mail discover shows how)",
			ErrBlocked, strings.Join(why, " "))
	}
	if pl.Conflict != "" && !p.Adopt {
		return nil, fmt.Errorf("refusing to continue: %s. Pass -adopt to replace it (the current files are backed up first)", pl.Conflict)
	}
	if len(pl.MissingPackages) > 0 && pl.PackageManager != "apt" {
		return nil, fmt.Errorf("install %s with your package manager first, then run plan again", strings.Join(pl.MissingPackages, " "))
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
		clientDir+"/client.crt", clientDir+"/client.key", clientDir+"/ca.crt", clientDir+"/client.prev.crt")
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

	if len(pl.MissingPackages) > 0 {
		out, err := installPackages(run, pl.MissingPackages)
		if !step("install packages "+strings.Join(pl.MissingPackages, " "), err, out) {
			return fail()
		}
		rc.InstalledPackages = pl.MissingPackages
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
	relay := Marker + "\n" + rc.ClientFingerprint + " njiwa-worker\n"
	if prev := previousClientFingerprint(p, now); prev != "" && prev != rc.ClientFingerprint {
		relay += prev + " njiwa-worker-previous\n"
		rc.Steps = append(rc.Steps, Step{Name: "keep previous client certificate", OK: true,
			Detail: "accepted until the worker uses the new one (at most " + PreviousClientGrace.String() + ")"})
	}
	files = append(files, File{Path: njDir + "/relay_clientcerts", Mode: 0644, Content: []byte(relay)})
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
	if !p.NoFirewall {
		out, err = run.Run("nft", "-c", "-f", p.path(FirewallFile))
		if !step("firewall rules check", err, out) {
			return fail()
		}
	}
	if v, err := run.Run("postconf", "-h", "mail_version"); err == nil {
		rc.PostfixVersion = strings.TrimSpace(v)
	}
	if !step("reload services", reload(p, run), "") {
		return fail()
	}
	rc.Result = "ok"
	if err := saveReceipt(p, rc); err != nil {
		return rc, err
	}
	return rc, nil
}

// reload starts or reloads Postfix and restarts OpenDKIM, and loads (or, when
// its file is gone after a rollback, removes) the firewall table. With
// systemd it uses the units; without (containers) the programs directly.
func reload(p Params, run Runner) error {
	if _, err := os.Stat(p.path(FirewallFile)); err == nil {
		if out, err := run.Run("nft", "-f", p.path(FirewallFile)); err != nil {
			return fmt.Errorf("firewall: %s %w", out, err)
		}
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			_, _ = run.Run("systemctl", "daemon-reload")
			_, _ = run.Run("systemctl", "enable", "njiwa-mail-firewall")
		}
	} else {
		_, _ = run.Run("nft", "delete", "table", "inet", "njiwa_mail") // absent is fine
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		_, _ = run.Run("systemctl", "daemon-reload") // picks up the Postfix boot-ordering drop-in
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
	// Without systemd, OpenDKIM is restarted by hand so it reads changed key
	// and signing tables (it would otherwise keep signing with the old ones).
	if _, err := os.Stat(p.path(dkimConf)); err == nil {
		restartOpenDKIM(p, run)
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
		// A new CA: certificates issued by a previous one must be reissued.
		p.RenewCerts = true
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
	if certDue(p.path(njDir+"/server.crt"), now, p.RenewCerts) {
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
	if certDue(p.path(clientDir+"/client.crt"), now, p.RenewCerts) {
		// Keep the replaced certificate so Postfix can accept it for a grace
		// period (see previousClientFingerprint).
		if old, err := os.ReadFile(p.path(clientDir + "/client.crt")); err == nil {
			stamp := []byte(prevStamp + now.UTC().Format(time.RFC3339) + "\n")
			if err := writeFileAtomic(p.path(clientDir+"/client.prev.crt"), append(stamp, old...), 0o644); err != nil {
				return err
			}
		}
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

// prevStamp starts the first line of client.prev.crt, followed by the time the
// certificate was replaced (the file's mtime would not survive a restore).
const prevStamp = "# replaced at "

// previousClientFingerprint is the replaced client certificate's fingerprint
// while it is within the grace period after renewal and not expired.
func previousClientFingerprint(p Params, now time.Time) string {
	b, err := os.ReadFile(p.path(clientDir + "/client.prev.crt"))
	if err != nil || !bytes.HasPrefix(b, []byte(prevStamp)) {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimPrefix(string(b), prevStamp), "\n")
	replaced, err := time.Parse(time.RFC3339, line)
	if err != nil || now.Sub(replaced) > PreviousClientGrace {
		return ""
	}
	if exp, err := PEMExpiry(b); err != nil || !now.Before(exp) {
		return ""
	}
	fp, _ := Fingerprint(b)
	return fp
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
	_ = reload(p, run)
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
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("reading the installer receipts: %w", err)
		}
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
	if p.RemovePackages && len(rc.InstalledPackages) > 0 {
		args := append([]string{"purge", "-y"}, rc.InstalledPackages...)
		out, err := run.Run("apt-get", args...)
		rc.Steps = append(rc.Steps, Step{Name: "remove packages " + strings.Join(rc.InstalledPackages, " "), OK: err == nil, Detail: strings.TrimSpace(out)})
	}
	rc.RollbackState = "rolled_back"
	return rc, saveReceipt(rp, rc)
}

// installPackages installs the mail packages with apt. It refuses when apt
// would remove any package (for example another mail server), and Postfix is
// preseeded to "No configuration" so the package changes nothing before the
// installer writes its own configuration.
func installPackages(run Runner, pkgs []string) (string, error) {
	sim, err := run.Run("apt-get", append([]string{"-s", "install", "-y", "--no-install-recommends"}, pkgs...)...)
	if err != nil {
		return sim, fmt.Errorf("apt-get could not plan the installation")
	}
	var removes []string
	for _, l := range strings.Split(sim, "\n") {
		if strings.HasPrefix(l, "Remv ") {
			removes = append(removes, strings.Fields(l)[1])
		}
	}
	if len(removes) > 0 {
		return sim, fmt.Errorf("installing would remove %s; nothing was installed", strings.Join(removes, ", "))
	}
	if out, err := run.Run("sh", "-c", "echo 'postfix postfix/main_mailer_type select No configuration' | debconf-set-selections"); err != nil {
		return out, err
	}
	return run.Run("env", append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends"}, pkgs...)...)
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

// restartOpenDKIM stops the running signer (from its pid file) and starts it
// again with the current configuration. Used only on hosts without systemd.
func restartOpenDKIM(p Params, run Runner) {
	pidFile := p.path("/run/opendkim/opendkim.pid")
	if b, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 1 {
			if proc, err := os.FindProcess(pid); err == nil && proc.Signal(syscall.SIGTERM) == nil {
				for i := 0; i < 50; i++ { // up to 5 s for it to exit
					if proc.Signal(syscall.Signal(0)) != nil {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
		_ = os.Remove(pidFile)
	}
	_ = os.MkdirAll(p.path("/run/opendkim"), 0o755)
	chownTo(p.path("/run/opendkim"), "opendkim")
	_, _ = run.Run("opendkim", "-x", p.path(dkimConf))
}
