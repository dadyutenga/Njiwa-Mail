package managed

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Platform is what differs between the distributions the managed installer
// supports: Debian/Ubuntu (apt, Postfix's postfix@.service, Berkeley DB maps)
// and the RHEL family (AlmaLinux, Rocky Linux, RHEL, Fedora: dnf, EPEL for
// OpenDKIM on Enterprise Linux, postfix.service, LMDB maps on Fedora and EL
// 10, Postfix 3.5 on EL 9, SELinux, firewalld). Detection only reads files and
// runs read-only queries (postconf); it never changes the host.
type Platform struct {
	Family         string `json:"family,omitempty"` // debian | rhel ("" = unknown, treated as debian)
	ID             string `json:"id,omitempty"`
	Version        string `json:"version,omitempty"`
	PostfixVersion string `json:"postfix_version,omitempty"` // "" when Postfix is not installed yet
	MapType        string `json:"map_type,omitempty"`        // hash | lmdb ("" = the family's default)
	SELinux        bool   `json:"selinux,omitempty"`
	Firewalld      bool   `json:"firewalld,omitempty"`
	EPELEnabled    bool   `json:"epel_enabled,omitempty"`
	// CRBEnabled: the CodeReady Builder repository (crb) is enabled. EPEL's
	// OpenDKIM on Enterprise Linux 9 needs libraries from it.
	CRBEnabled bool `json:"crb_enabled,omitempty"`
}

// Runner-like read-only query used during detection; nil skips queries.
type queryFunc func(name string, args ...string) (string, error)

// DetectPlatform describes the host under root. query may be nil (tests, or
// before Postfix is installed); then Postfix facts come from the family.
func DetectPlatform(root string, query queryFunc) Platform {
	p := Params{Root: root}
	var pl Platform
	if b, err := os.ReadFile(p.path("/etc/os-release")); err == nil {
		kv := map[string]string{}
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
				kv[k] = strings.Trim(v, `"'`)
			}
		}
		pl.ID, pl.Version = kv["ID"], kv["VERSION_ID"]
		like := " " + kv["ID_LIKE"] + " "
		switch {
		case pl.ID == "debian" || pl.ID == "ubuntu" || strings.Contains(like, " debian ") || strings.Contains(like, " ubuntu "):
			pl.Family = "debian"
		case pl.ID == "fedora" || pl.ID == "rhel" || pl.ID == "almalinux" || pl.ID == "rocky" || pl.ID == "centos" ||
			strings.Contains(like, " rhel ") || strings.Contains(like, " fedora "):
			pl.Family = "rhel"
		}
	}
	if _, err := os.Stat(p.path("/sys/fs/selinux/enforce")); err == nil {
		pl.SELinux = true
	}
	if _, err := os.Stat(p.path("/run/firewalld/firewalld.pid")); err == nil {
		pl.Firewalld = true
	}
	for _, f := range []string{"/etc/yum.repos.d/epel.repo", "/etc/yum.repos.d/epel.sources"} {
		if _, err := os.Stat(p.path(f)); err == nil {
			pl.EPELEnabled = true
		}
	}
	pl.CRBEnabled = repoEnabled(p, "crb", "codeready-builder-for-rhel-")
	if query != nil && root == "/" {
		if v, err := query("postconf", "-d", "-h", "mail_version"); err == nil {
			pl.PostfixVersion = strings.TrimSpace(v)
		}
		if m, err := query("postconf", "-m"); err == nil {
			types := " " + strings.Join(strings.Fields(m), " ") + " "
			switch {
			case strings.Contains(types, " hash "):
				pl.MapType = "hash"
			case strings.Contains(types, " lmdb "):
				pl.MapType = "lmdb"
			}
		}
	}
	return pl
}

// repoEnabled reports whether a dnf repository whose id is name, or starts
// with prefix, is enabled in /etc/yum.repos.d.
func repoEnabled(p Params, name, prefix string) bool {
	files, _ := filepath.Glob(p.path("/etc/yum.repos.d/*.repo"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		in := false
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
				id := strings.TrimSpace(l[1 : len(l)-1])
				in = id == name || (prefix != "" && strings.HasPrefix(id, prefix) && !strings.Contains(id, "debug") && !strings.Contains(id, "source"))
				continue
			}
			if k, v, ok := strings.Cut(l, "="); in && ok && strings.TrimSpace(k) == "enabled" && strings.TrimSpace(v) == "1" {
				return true
			}
		}
	}
	return false
}

func (pl Platform) major() int {
	n, _ := strconv.Atoi(strings.SplitN(pl.Version, ".", 2)[0])
	return n
}

// enterpriseLinux is AlmaLinux, Rocky Linux, RHEL or CentOS (not Fedora).
func (pl Platform) enterpriseLinux() bool { return pl.Family == "rhel" && pl.ID != "fedora" }

// mapType is the Postfix lookup table type: Berkeley DB (hash) where it
// exists, LMDB where it was removed (Fedora, Enterprise Linux 10).
func (pl Platform) mapType() string {
	if pl.MapType != "" {
		return pl.MapType
	}
	if pl.Family == "rhel" && (pl.ID == "fedora" || pl.major() >= 10) {
		return "lmdb"
	}
	return "hash"
}

// mapExt is the file postmap writes next to the source table.
func (pl Platform) mapExt() string {
	if pl.mapType() == "lmdb" {
		return ".lmdb"
	}
	return ".db"
}

// compatLevel is Postfix's compatibility_level: "3.6" from Postfix 3.6 on;
// Postfix 3.5 (Enterprise Linux 9) only knows the integer levels.
func (pl Platform) compatLevel() string {
	v := pl.PostfixVersion
	if v == "" {
		if pl.enterpriseLinux() && pl.major() == 9 {
			return "2"
		}
		return "3.6"
	}
	parts := strings.SplitN(v, ".", 3)
	maj, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if maj > 3 || (maj == 3 && minor >= 6) {
		return "3.6"
	}
	return "2"
}

// packageManager is apt or dnf ("" when the installer cannot install
// packages on this distribution).
func (pl Platform) packageManager() string {
	switch pl.Family {
	case "debian":
		return "apt"
	case "rhel":
		return "dnf"
	}
	return ""
}

// needsEPEL: OpenDKIM comes from EPEL on Enterprise Linux.
func (pl Platform) needsEPEL() bool { return pl.enterpriseLinux() && !pl.EPELEnabled }

// needsCRB: on Enterprise Linux 9, EPEL's OpenDKIM needs libmemcached and
// libmilter from the CodeReady Builder repository (in AppStream on EL 10).
func (pl Platform) needsCRB() bool {
	return pl.enterpriseLinux() && pl.major() == 9 && !pl.CRBEnabled
}

// postfixDropIn is the systemd drop-in that orders Postfix after Docker:
// Debian runs Postfix as postfix@-.service, the RHEL family as postfix.service.
func (pl Platform) postfixDropIn() string {
	if pl.Family == "rhel" {
		return "/etc/systemd/system/postfix.service.d/njiwa-mail.conf"
	}
	return postfixBootDropIn
}
