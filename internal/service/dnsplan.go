package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/njiwa-tz/mail/internal/dnsauth"
	"github.com/njiwa-tz/mail/internal/dnsprovider"
)

// The DNS plan engine is a pure function: what the domain requires (built from
// the active delivery mode by expectedRecords, never from a provider) plus what
// the zone already holds becomes a reviewable change set. It never produces a
// delete, never replaces an MX, never touches A/AAAA/CNAME records, never
// publishes a second SPF record and never overwrites a DKIM key or a DMARC
// policy. Anything it cannot do safely becomes a conflict or a manual item.

const (
	PlanCreate    = "create"
	PlanUnchanged = "unchanged"
	PlanModify    = "modify" // only an SPF merge
	PlanConflict  = "conflict"
	PlanManual    = "manual"
)

// Acknowledgement codes. Approving a plan that contains the matching change
// requires the user to confirm each code explicitly.
const (
	AckModifySPF = "modify_spf"
	AckCreateMX  = "create_mx"
)

type PlanItem struct {
	Key      string `json:"key"`    // ownership, spf, dkim, dmarc, mx, return_path, return_path_spf
	Action   string `json:"action"` // create, unchanged, modify, conflict, manual
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"` // the full proposed content (create/modify), or the required value (conflict/manual)
	Priority int    `json:"priority,omitempty"`
	TTL      int    `json:"ttl,omitempty"` // 0 = provider automatic; modify keeps the existing TTL
	// Existing is the record this item keeps (unchanged) or edits (modify).
	Existing *dnsprovider.Record `json:"existing,omitempty"`
	// Others are the other records at the same name that matter to this item.
	Others []dnsprovider.Record `json:"others,omitempty"`
	Risk   string               `json:"risk"` // low, medium, high
	Ack    string               `json:"ack,omitempty"`
	Reason string               `json:"reason"`
	// Placeholder: the value is guidance, not publishable.
	Placeholder bool `json:"placeholder,omitempty"`
}

type PlanWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // info, warning, blocker
	Message  string `json:"message"`
}

type DNSPlan struct {
	Domain       string        `json:"domain"`
	Zone         string        `json:"zone"`
	Items        []PlanItem    `json:"items"`
	Warnings     []PlanWarning `json:"warnings"`
	Infra        []string      `json:"existing_mail"`
	Blocked      bool          `json:"blocked"`
	RequiredAcks []string      `json:"required_acks"`
	Digest       string        `json:"digest"`
	Rollback     string        `json:"rollback"`
}

// Actionable reports the changes that would be written.
func (p DNSPlan) Actionable() int {
	n := 0
	for _, it := range p.Items {
		if it.Action == PlanCreate || it.Action == PlanModify {
			n++
		}
	}
	return n
}

type PlanInput struct {
	Domain       string
	Purpose      string
	ZoneID       string
	ZoneName     string
	ZoneNS       []string // the provider's name servers for the zone
	ObservedNS   []string // what the public DNS says the zone's NS records are
	NSLookupFail bool
	DNSSEC       string
	Required     []DomainRecord
	Existing     []dnsprovider.Record
	MailHost     string
}

var allTermRe = regexp.MustCompile(`^[+\-~?]?all$`)

func lower(s string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), ".")) }

func within(name, zone string) bool { return name == zone || strings.HasSuffix(name, "."+zone) }

// mergeSPF adds the mechanisms required by want to one existing SPF record
// without changing anything else. ok=false means it cannot be done safely.
func mergeSPF(existing, want string) (merged string, added []string, ok bool, why string) {
	terms := strings.Fields(existing)
	allAt := -1
	have := map[string]bool{}
	for i, t := range terms {
		lt := strings.ToLower(t)
		if i == 0 {
			continue
		}
		if strings.HasPrefix(lt, "redirect=") {
			return "", nil, false, "the existing SPF record uses redirect=, so its policy lives elsewhere and cannot be edited here"
		}
		if allTermRe.MatchString(lt) {
			allAt = i
			if lt == "all" || lt == "+all" {
				return "", nil, false, "the existing SPF record ends in +all, which lets anyone send as this domain; fix it first"
			}
			continue
		}
		have[lt] = true
	}
	for i, t := range strings.Fields(want) {
		lt := strings.ToLower(t)
		if i == 0 || allTermRe.MatchString(lt) || have[lt] {
			continue
		}
		added = append(added, t)
		have[lt] = true
	}
	if len(added) == 0 {
		return existing, nil, true, ""
	}
	if allAt < 0 {
		terms = append(terms, added...)
	} else {
		out := append([]string{}, terms[:allAt]...)
		out = append(out, added...)
		terms = append(out, terms[allAt:]...)
	}
	merged = strings.Join(terms, " ")
	if p := dnsauth.Problems(merged); len(p) > 0 {
		return "", nil, false, "the merged SPF record would not be valid: " + p[0]
	}
	return merged, added, true, ""
}

// spfPass reports whether the existing record already contains every
// mechanism the requirement lists.
func spfCovers(existing, want string) bool {
	_, added, ok, _ := mergeSPF(existing, want)
	return ok && len(added) == 0
}

func detectInfra(existing []dnsprovider.Record, name, mailHost string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	classify := func(host string) string {
		h := lower(host)
		switch {
		case strings.HasSuffix(h, ".google.com") || strings.HasSuffix(h, ".googlemail.com") || h == "google.com":
			return "Google Workspace"
		case strings.HasSuffix(h, ".mail.protection.outlook.com") || strings.HasSuffix(h, ".outlook.com"):
			return "Microsoft 365"
		case strings.Contains(h, ".zoho.") || strings.HasPrefix(h, "zoho."):
			return "Zoho Mail"
		case strings.HasSuffix(h, ".mx.cloudflare.net"):
			return "Cloudflare Email Routing"
		case mailHost != "" && h == lower(mailHost):
			return "this Njiwa-Mail / Mailcow server (" + h + ")"
		}
		return "another mail server (" + h + ")"
	}
	for _, r := range existing {
		if r.Name != name {
			continue
		}
		switch r.Type {
		case "MX":
			add(classify(r.Content))
		case "TXT":
			for _, t := range strings.Fields(strings.ToLower(r.Content)) {
				if v, ok := strings.CutPrefix(t, "include:"); ok {
					switch {
					case strings.Contains(v, "google.com"):
						add("Google Workspace")
					case strings.Contains(v, "protection.outlook.com"):
						add("Microsoft 365")
					case strings.Contains(v, "zoho."):
						add("Zoho Mail")
					case strings.Contains(v, "mx.cloudflare.net"):
						add("Cloudflare Email Routing")
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func recordsAt(rs []dnsprovider.Record, name, typ string) []dnsprovider.Record {
	var out []dnsprovider.Record
	for _, r := range rs {
		if r.Name == name && (typ == "" || r.Type == typ) {
			out = append(out, r)
		}
	}
	return out
}

func cnameAt(rs []dnsprovider.Record, name string) []dnsprovider.Record {
	return recordsAt(rs, name, "CNAME")
}

func parentZoneAncestors(name, zone string) []string {
	var out []string
	for n := name; n != "" && within(n, zone); {
		out = append(out, n)
		if n == zone {
			break
		}
		_, rest, _ := strings.Cut(n, ".")
		n = rest
	}
	return out
}

// BuildDNSPlan turns requirements and existing records into a change set.
func BuildDNSPlan(in PlanInput) DNSPlan {
	in.Domain, in.ZoneName, in.MailHost = lower(in.Domain), lower(in.ZoneName), lower(in.MailHost)
	for i := range in.Existing {
		in.Existing[i].Name = lower(in.Existing[i].Name)
	}
	p := DNSPlan{Domain: in.Domain, Zone: in.ZoneName, Items: []PlanItem{}, Warnings: []PlanWarning{}, RequiredAcks: []string{}}
	warn := func(code, sev, msg string) { p.Warnings = append(p.Warnings, PlanWarning{code, sev, msg}) }

	// 1. Is this really the zone's authoritative DNS, and does the domain live in it?
	if !within(in.Domain, in.ZoneName) {
		warn("domain_not_in_zone", "blocker", in.Domain+" does not belong to the zone "+in.ZoneName+". Choose the zone that contains it.")
	}
	switch {
	case in.NSLookupFail || len(in.ObservedNS) == 0:
		warn("ns_unconfirmed", "blocker", "The public name servers of "+in.ZoneName+" could not be looked up, so Njiwa-Mail cannot confirm that Cloudflare answers for it. Try again in a moment.")
	default:
		zone := map[string]bool{}
		for _, n := range in.ZoneNS {
			zone[lower(n)] = true
		}
		match := 0
		for _, n := range in.ObservedNS {
			if zone[lower(n)] {
				match++
			}
		}
		if match == 0 {
			warn("not_delegated", "blocker", in.ZoneName+" exists in this Cloudflare account, but the internet does not use Cloudflare for it (its name servers are "+strings.Join(in.ObservedNS, ", ")+"). Records added here would not take effect. Change the name servers at your registrar first, or set DNS up manually where the domain is really hosted.")
		} else if match < len(in.ObservedNS) {
			warn("ns_mixed", "warning", "Some name servers of "+in.ZoneName+" are not Cloudflare's. Answers may differ between resolvers until the delegation is fixed.")
		}
	}
	for _, r := range in.Existing {
		if r.Type == "NS" && r.Name != in.ZoneName && within(in.Domain, r.Name) {
			warn("delegated_subzone", "blocker", r.Name+" is delegated to other name servers. DNS for "+in.Domain+" is answered there, not here.")
		}
	}
	switch in.DNSSEC {
	case "active":
		warn("dnssec_active", "info", "DNSSEC is on for this zone. Cloudflare signs new records automatically; DNSSEC is not changed.")
	case "pending", "pending-disabled":
		warn("dnssec_pending", "warning", "DNSSEC is being enabled or disabled for this zone. Njiwa-Mail does not change it; wait until it settles before relying on new records.")
	}
	for _, r := range in.Existing {
		if r.Type == "CAA" {
			warn("caa_present", "info", "CAA records exist. They only control certificate issuance and are left untouched.")
			break
		}
	}

	// 2. Existing mail infrastructure.
	p.Infra = detectInfra(in.Existing, in.Domain, in.MailHost)
	if len(recordsAt(in.Existing, in.Domain, "MX")) > 0 || len(p.Infra) > 0 {
		warn("existing_mail", "warning", fmt.Sprintf("%s already has email set up (%s). Njiwa-Mail will not replace its MX or authentication records. Changing them could stop mail to or from this domain.",
			in.Domain, strings.Join(append([]string{}, orDefault(p.Infra, "an MX record")...), ", ")))
	}
	if in.Domain == in.ZoneName && (len(recordsAt(in.Existing, in.Domain, "MX")) > 0 || len(p.Infra) > 0) {
		warn("use_subdomain", "warning", "Your main domain already carries email. Prefer a dedicated sending subdomain such as notify."+in.Domain+" so existing mail is not disturbed.")
	}
	if len(recordsAt(in.Existing, in.Domain, "MX")) == 0 && len(recordsAt(in.Existing, in.Domain, "A"))+len(recordsAt(in.Existing, in.Domain, "AAAA")) > 0 {
		warn("implicit_mx", "info", in.Domain+" has a web address record and no MX: mail to it is currently delivered to that address. An MX record changes that.")
	}
	if in.MailHost != "" {
		for _, t := range []string{"A", "AAAA", "CNAME"} {
			for _, r := range recordsAt(in.Existing, in.MailHost, t) {
				if r.Proxied {
					warn("mail_host_proxied", "warning", in.MailHost+" is proxied by Cloudflare (orange cloud). SMTP cannot pass through the proxy: set it to DNS only. Njiwa-Mail does not change this record.")
				}
			}
		}
	}

	// 3. One item per required record.
	for _, req := range in.Required {
		it := PlanItem{Key: req.Key, Type: req.Type, Name: lower(req.Host), Content: req.Value, Priority: req.Priority, Risk: "low", Placeholder: req.Placeholder}
		switch {
		case !within(it.Name, in.ZoneName):
			it.Action, it.Risk = PlanManual, "low"
			it.Reason = it.Name + " is outside the zone " + in.ZoneName + ". Publish this record at the DNS host that serves that name."
		case req.Placeholder || strings.TrimSpace(req.Value) == "":
			it.Action = PlanManual
			it.Reason = "Njiwa-Mail does not know this value for your delivery setup yet, so it will not guess one. " + req.Hint
		default:
			planOne(&it, in, req)
		}
		p.Items = append(p.Items, it)
	}
	// Two requirements can ask for the same record (the MX for receiving and for
	// bounces often coincide): create it once.
	planned := map[string]string{}
	for i := range p.Items {
		it := &p.Items[i]
		if it.Action != PlanCreate {
			continue
		}
		k := it.Type + "|" + it.Name + "|" + it.Content
		if first, ok := planned[k]; ok {
			it.Action, it.Ack, it.Risk = PlanUnchanged, "", "low"
			it.Reason = "The same record as the " + first + " item: it is created once."
			continue
		}
		planned[k] = it.Key
	}

	acks := map[string]bool{}
	for _, it := range p.Items {
		if it.Ack != "" {
			acks[it.Ack] = true
		}
	}
	for a := range acks {
		p.RequiredAcks = append(p.RequiredAcks, a)
	}
	sort.Strings(p.RequiredAcks)
	for _, w := range p.Warnings {
		if w.Severity == "blocker" {
			p.Blocked = true
		}
	}
	p.Rollback = "Every record Njiwa-Mail creates is listed with its previous value. Rolling back removes only records Njiwa-Mail created, and puts an edited SPF record back, only while they are unchanged since. Anything someone else changed in the meantime is left alone and listed so you can fix it by hand. Disconnecting Cloudflare never removes records."
	p.Digest = planDigest(in, p.Items)
	return p
}

func orDefault(s []string, d string) []string {
	if len(s) == 0 {
		return []string{d}
	}
	return s
}

func planOne(it *PlanItem, in PlanInput, req DomainRecord) {
	name := it.Name
	if cn := cnameAt(in.Existing, name); len(cn) > 0 {
		it.Action, it.Risk, it.Others = PlanConflict, "high", cn
		it.Reason = name + " is an alias (CNAME) to " + cn[0].Content + ". A name with a CNAME cannot hold " + req.Type + " records, and Njiwa-Mail never changes aliases. Resolve this by hand."
		return
	}
	switch req.Key {
	case "ownership":
		for _, r := range recordsAt(in.Existing, name, "TXT") {
			if strings.TrimSpace(r.Content) == strings.TrimSpace(req.Value) {
				it.Action, it.Existing, it.Reason = PlanUnchanged, &r, "Already published."
				return
			}
		}
		it.Action, it.Reason = PlanCreate, "Adds a TXT record that proves the domain is yours. It does not affect mail."
	case "spf", "return_path_spf":
		planSPF(it, in, req)
	case "dkim":
		txt := recordsAt(in.Existing, name, "TXT")
		want, werr := dnsauth.ParseDKIMKey(req.Value)
		for _, r := range txt {
			if r.Content == req.Value {
				it.Action, it.Existing, it.Reason = PlanUnchanged, &r, "This exact key is already published."
				return
			}
			if got, err := dnsauth.ParseDKIMKey(r.Content); err == nil && werr == nil && want.Fingerprint != "" && got.Fingerprint == want.Fingerprint {
				it.Action, it.Existing, it.Reason = PlanUnchanged, &r, "This key is already published."
				return
			}
		}
		if len(txt) > 0 {
			it.Action, it.Risk, it.Others = PlanConflict, "high", txt
			it.Reason = "A different DKIM key is already published at " + name + ". Another system may sign mail with it, so it is never overwritten. Use a different selector in your delivery settings, or replace it yourself once you know it is unused."
			return
		}
		it.Action, it.Reason = PlanCreate, "Publishes the public key your mail server signs with, at its own selector. Other selectors are not touched."
	case "dmarc":
		for _, r := range recordsAt(in.Existing, name, "TXT") {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Content)), "v=dmarc1") {
				it.Action, it.Existing = PlanUnchanged, &r
				it.Reason = "A DMARC policy already exists (" + dmarcSummary(r.Content) + "). It is left exactly as it is: changing it would change how receivers treat all mail from this domain."
				return
			}
		}
		// A subdomain without its own record inherits the organisational policy.
		if name != in.ZoneName {
			dom := strings.TrimPrefix(name, "_dmarc.")
			ancestors := parentZoneAncestors(dom, in.ZoneName)
			if len(ancestors) > 0 {
				ancestors = ancestors[1:]
			}
			for _, anc := range ancestors {
				for _, r := range recordsAt(in.Existing, "_dmarc."+anc, "TXT") {
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Content)), "v=dmarc1") {
						it.Action, it.Existing = PlanUnchanged, &r
						it.Reason = "This domain inherits the DMARC policy of " + anc + " (" + dmarcSummary(r.Content) + "). A separate p=none record would weaken it, so none is added."
						return
					}
				}
			}
		}
		it.Action, it.Reason = PlanCreate, "Adds a monitoring-only DMARC policy (p=none). It does not reject or quarantine anything; tighten it later once reports look clean."
	case "mx", "return_path":
		mxs := recordsAt(in.Existing, name, "MX")
		for _, r := range mxs {
			if r.Content == lower(req.Value) {
				it.Action, it.Existing, it.Reason = PlanUnchanged, &r, "An MX to "+req.Value+" already exists."
				return
			}
		}
		if len(mxs) > 0 {
			it.Action, it.Risk, it.Others = PlanConflict, "high", mxs
			it.Reason = name + " already receives mail through " + mxs[0].Content + ". Njiwa-Mail never replaces an MX record: that would redirect or lose existing mail. Use a subdomain, or change it yourself deliberately."
			return
		}
		it.Action, it.Risk, it.Ack = PlanCreate, "medium", AckCreateMX
		it.Reason = "Adds an MX so mail for " + name + " is delivered to " + req.Value + ". Mail to this name is delivered there from now on."
		if it.Key == "return_path" {
			it.Reason = "Adds an MX so bounce messages for " + name + " return to " + req.Value + "."
		}
	default:
		it.Action, it.Reason = PlanManual, "Not supported for automatic setup."
	}
}

func planSPF(it *PlanItem, in PlanInput, req DomainRecord) {
	name := it.Name
	var spfRecs []dnsprovider.Record
	var contents []string
	for _, r := range recordsAt(in.Existing, name, "TXT") {
		contents = append(contents, r.Content)
	}
	for _, c := range dnsauth.FindSPF(contents) {
		for _, r := range recordsAt(in.Existing, name, "TXT") {
			if r.Content == c {
				spfRecs = append(spfRecs, r)
				break
			}
		}
	}
	switch len(spfRecs) {
	case 0:
		it.Action, it.Reason = PlanCreate, "Adds the SPF policy that authorises your sending server."
		return
	case 1:
		r := spfRecs[0]
		merged, added, ok, why := mergeSPF(r.Content, req.Value)
		switch {
		case !ok:
			it.Action, it.Risk, it.Existing = PlanConflict, "high", &r
			it.Reason = "Cannot merge safely: " + why + "."
		case len(added) == 0:
			it.Action, it.Existing, it.Reason = PlanUnchanged, &r, "The existing SPF record already authorises it."
		default:
			it.Action, it.Risk, it.Ack, it.Existing = PlanModify, "high", AckModifySPF, &r
			it.Content, it.TTL = merged, r.TTL
			it.Reason = "A domain may have only one SPF record, so " + strings.Join(added, " ") + " is added to the existing one. Nothing else in it changes and its TTL is kept. If other services send as this domain, they stay authorised."
		}
	default:
		it.Action, it.Risk, it.Others = PlanConflict, "high", spfRecs
		it.Reason = fmt.Sprintf("%d SPF records exist at %s. Receivers treat that as an error. Merge them into one by hand, then analyze again.", len(spfRecs), name)
	}
}

func dmarcSummary(rec string) string {
	if d, err := dnsauth.ParseDMARC(rec); err == nil {
		s := "p=" + d.Policy
		if d.SubPol != "" {
			s += "; sp=" + d.SubPol
		}
		return s
	}
	return "unreadable"
}

// planDigest binds approval to the exact change set AND to the state of every
// record at the names it touches, so a change made by someone else between
// approval and apply is detected.
func planDigest(in PlanInput, items []PlanItem) string {
	type fp struct {
		ID, Type, Name, Content, Modified string
		Priority, TTL                     int
	}
	names := map[string]bool{}
	for _, it := range items {
		names[it.Name] = true
	}
	var seen []fp
	for _, r := range in.Existing {
		if names[r.Name] {
			seen = append(seen, fp{r.ID, r.Type, r.Name, r.Content, r.Modified, r.Priority, r.TTL})
		}
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i].ID < seen[j].ID })
	type act struct {
		Key, Action, Type, Name, Content, Ack string
		Priority, TTL                         int
		Existing                              string
	}
	var acts []act
	for _, it := range items {
		a := act{it.Key, it.Action, it.Type, it.Name, it.Content, it.Ack, it.Priority, it.TTL, ""}
		if it.Existing != nil {
			a.Existing = it.Existing.ID
		}
		acts = append(acts, a)
	}
	b, _ := json.Marshal(map[string]any{"zone": in.ZoneID, "acts": acts, "seen": seen})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
