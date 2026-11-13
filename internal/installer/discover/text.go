package discover

import (
	"fmt"
	"sort"
	"strings"
)

// interestingPorts are shown in the summary: mail, web, and Njiwa's own.
var interestingPorts = map[int]string{25: "SMTP", 465: "SMTPS", 587: "submission", 993: "IMAPS", 995: "POP3S", 143: "IMAP", 110: "POP3",
	80: "HTTP", 443: "HTTPS", 8080: "Njiwa API", 3000: "Njiwa dashboard", 2525: "Njiwa inbound", 5432: "PostgreSQL", 10587: "Njiwa submission", 8891: "DKIM signer"}

// Text renders a report for a terminal.
func Text(r *Report) string {
	var b strings.Builder
	h := r.Host
	fmt.Fprintf(&b, "Host\n  %s, systemd: %s, %d CPU, %d MiB memory (%d available), %d MiB free under /var\n",
		orUnknown(h.OS), yesNo(h.Systemd), h.CPUs, h.MemTotalMB, h.MemAvailMB, h.DiskFreeMB)
	fmt.Fprintf(&b, "  public IPs: %s; private IPs: %s; Docker: %s\n", list(h.PublicIPs), list(h.PrivateIPs), yesNo(h.DockerPresent))

	b.WriteString("\nPorts in use (mail, web, Njiwa)\n")
	shown := 0
	for _, p := range r.Ports {
		name, ok := interestingPorts[p.Port]
		if !ok {
			continue
		}
		shown++
		fmt.Fprintf(&b, "  %-6d %-16s %-15s %s\n", p.Port, name, p.Address, owner(p))
	}
	if shown == 0 {
		b.WriteString("  none\n")
	}

	b.WriteString("\nMail services\n")
	if r.Mailcow != nil {
		fmt.Fprintf(&b, "  Mailcow: %s (hostname %s, running: %s)\n", orUnknown(r.Mailcow.Path), orUnknown(r.Mailcow.Hostname), yesNo(r.Mailcow.Running))
	}
	if len(r.MailServices) == 0 && r.Mailcow == nil {
		b.WriteString("  none found\n")
	}
	for _, s := range r.MailServices {
		state := "installed"
		if s.Running {
			state = "running"
		}
		switch {
		case s.Ours:
			state += ", configured by Njiwa-Mail"
		case s.InUse:
			state += ", in use: " + strings.Join(s.Evidence, "; ")
		default:
			state += ", not configured"
		}
		fmt.Fprintf(&b, "  %s: %s\n", s.Name, state)
	}
	if len(r.ComposeStacks) > 0 {
		fmt.Fprintf(&b, "\nDocker Compose stacks: %s\n", strings.Join(r.ComposeStacks, ", "))
	}
	if len(r.DockerNetworks) > 0 {
		var ns []string
		for _, n := range r.DockerNetworks {
			ns = append(ns, n.Subnet+" ("+n.Interface+")")
		}
		sort.Strings(ns)
		fmt.Fprintf(&b, "Docker networks: %s\n", strings.Join(ns, ", "))
	}
	if len(r.ReverseProxies) > 0 {
		fmt.Fprintf(&b, "Reverse proxies: %s\n", strings.Join(r.ReverseProxies, ", "))
	}
	for _, c := range r.Certificates {
		fmt.Fprintf(&b, "Certificate (%s): %s, expires %s\n", c.Owner, strings.Join(c.Names, ", "), c.NotAfter.Format("2006-01-02"))
	}
	for _, l := range r.Limits {
		fmt.Fprintf(&b, "Note: %s\n", l)
	}

	rec := r.Recommendation
	if rec == nil {
		return b.String()
	}
	label := map[string]string{ModeManaged: "Send from my VPS (Njiwa-managed mail server)", ModeExisting: "Use my existing mail server (SMTP)"}[rec.Mode]
	fmt.Fprintf(&b, "\nRecommended delivery mode: %s\n", label)
	for _, x := range rec.Reasons {
		fmt.Fprintf(&b, "  - %s\n", x)
	}
	if len(rec.ManagedBlockers) > 0 {
		b.WriteString("\nWhy managed mode cannot be installed here (nothing will be changed):\n")
		for _, i := range rec.ManagedBlockers {
			fmt.Fprintf(&b, "  [%s] %s\n      %s\n", i.Code, i.Detail, i.Fix)
		}
	}
	if len(rec.Warnings) > 0 {
		b.WriteString("\nBefore installing Njiwa-Mail:\n")
		for _, i := range rec.Warnings {
			fmt.Fprintf(&b, "  [%s] %s\n      %s\n", i.Code, i.Detail, i.Fix)
		}
	}
	if len(rec.Candidates) > 0 {
		b.WriteString("\nMail servers Njiwa-Mail can connect to:\n")
		for _, c := range rec.Candidates {
			fmt.Fprintf(&b, "  %s: %s port %d (%s). %s\n", c.Label, c.Host, c.Port, c.TLSMode, c.Note)
		}
	}
	b.WriteString("\nNext: docs/INSTALL.md. Record this result for the setup wizard with: njiwa-mail discover -record\n")
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func list(v []string) string {
	if len(v) == 0 {
		return "none"
	}
	return strings.Join(v, ", ")
}
