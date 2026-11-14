package service

import (
	"crypto/subtle"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

// Spam scoring for form submissions. A score of 0 to 100 and the reasons
// behind it, mapped to an action ladder: allow, flag, quarantine, reject.
// Workspace filters can raise the action but never lower it.

const (
	spamFlagAt       = 30
	spamQuarantineAt = 60
	spamRejectAt     = 90
)

var actionRank = map[string]int{"allow": 0, "flag": 1, "quarantine": 2, "reject": 3}

func actionForScore(score int) string {
	switch {
	case score >= spamRejectAt:
		return "reject"
	case score >= spamQuarantineAt:
		return "quarantine"
	case score >= spamFlagAt:
		return "flag"
	}
	return "allow"
}

func maxAction(a, b string) string {
	if actionRank[b] > actionRank[a] {
		return b
	}
	return a
}

var spamPhrases = []string{"viagra", "cialis", "casino", "bitcoin", "crypto invest", "loan offer", "payday", "seo services",
	"backlinks", "click here", "make money", "work from home", "forex", "porn", "escort", "guaranteed ranking", "web traffic",
	"free followers", "dear sir/madam", "lottery winner"}

var shorteners = []string{"bit.ly", "tinyurl.com", "t.co/", "goo.gl", "is.gd", "ow.ly", "cutt.ly"}

type spamResult struct {
	Score   int
	Reasons []string
}

func (r *spamResult) add(points int, reason string) {
	r.Score += points
	r.Reasons = append(r.Reasons, reason)
}

// scoreSubmission looks at the visitor's words and address. disposable says
// whether an email domain is a throwaway provider.
func scoreSubmission(values []string, email string, disposable func(string) bool, tooFast bool) spamResult {
	var r spamResult
	text := strings.ToLower(strings.Join(values, "\n"))
	links := strings.Count(text, "http://") + strings.Count(text, "https://") + strings.Count(text, "www.")
	switch {
	case links >= 6:
		r.add(40, "many_links")
	case links >= 3:
		r.add(25, "several_links")
	case links >= 1:
		r.add(5, "has_link")
	}
	for _, s := range shorteners {
		if strings.Contains(text, s) {
			r.add(15, "link_shortener")
			break
		}
	}
	hits := 0
	for _, p := range spamPhrases {
		if strings.Contains(text, p) && hits < 3 {
			hits++
			r.add(20, "spam_phrase")
		}
	}
	letters, upper := 0, 0
	for _, c := range strings.Join(values, " ") {
		if c >= 'A' && c <= 'Z' {
			letters++
			upper++
		} else if c >= 'a' && c <= 'z' {
			letters++
		}
	}
	if letters >= 30 && upper*10 > letters*7 {
		r.add(10, "shouting")
	}
	run, prev := 0, rune(0)
	for _, c := range text {
		if c == prev && c != ' ' && c != '\n' {
			run++
			if run == 15 {
				r.add(10, "repeated_characters")
				break
			}
		} else {
			run, prev = 1, c
		}
	}
	if len(text) > 4000 {
		r.add(10, "very_long")
	}
	if at := strings.LastIndexByte(email, '@'); at > 0 && disposable != nil && disposable(email[at+1:]) {
		r.add(20, "disposable_email")
	}
	if tooFast {
		r.add(30, "submitted_too_fast")
	}
	if r.Score > 100 {
		r.Score = 100
	}
	return r
}

// ---------------------------------------------------------------- filters

type formFilter struct {
	ID, Kind, Pattern, Action string
}

var wordSplit = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// validFilter checks a pattern for its kind and returns it normalised.
func validFilter(kind, pattern, action string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || len(pattern) > 200 {
		return "", badRequest("enter a pattern (up to 200 characters)")
	}
	switch action {
	case "flag", "quarantine", "reject":
	default:
		return "", badRequest("the action is flag, quarantine or reject")
	}
	switch kind {
	case "keyword", "phrase":
		return strings.ToLower(pattern), nil
	case "regex":
		if _, err := regexp.Compile(pattern); err != nil {
			return "", badRequest("the regular expression is not valid: " + clean(err.Error(), 120))
		}
		return pattern, nil
	case "email":
		e, ok := NormalizeEmail(pattern)
		if !ok {
			return "", badRequest("enter a full email address")
		}
		return e, nil
	case "domain":
		d := strings.ToLower(strings.TrimPrefix(pattern, "@"))
		if !domainRe.MatchString(d) {
			return "", badRequest("enter a domain such as example.com")
		}
		return d, nil
	case "ip":
		if _, err := netip.ParseAddr(pattern); err == nil {
			return pattern, nil
		}
		if _, err := netip.ParsePrefix(pattern); err == nil {
			return pattern, nil
		}
		return "", badRequest("enter an IP address or a range such as 203.0.113.0/24")
	}
	return "", badRequest("the kind is keyword, phrase, regex, email, domain or ip")
}

// matchFilter reports whether one filter matches a submission. ip may be "".
func matchFilter(f formFilter, values []string, email, ip string) bool {
	text := strings.ToLower(strings.Join(values, "\n"))
	switch f.Kind {
	case "keyword":
		for _, w := range wordSplit.Split(text, -1) {
			if w == f.Pattern {
				return true
			}
		}
	case "phrase":
		return strings.Contains(text, f.Pattern)
	case "regex":
		re, err := regexp.Compile(f.Pattern)
		return err == nil && re.MatchString(strings.Join(values, "\n"))
	case "email":
		return email == f.Pattern
	case "domain":
		at := strings.LastIndexByte(email, '@')
		return at > 0 && (email[at+1:] == f.Pattern || strings.HasSuffix(email[at+1:], "."+f.Pattern))
	case "ip":
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return false
		}
		if p, err := netip.ParsePrefix(f.Pattern); err == nil {
			return p.Contains(addr)
		}
		if a, err := netip.ParseAddr(f.Pattern); err == nil {
			return a == addr
		}
	}
	return false
}

func cleanIP(ip string) string {
	if h, _, err := net.SplitHostPort(ip); err == nil {
		return h
	}
	return ip
}

func parseAddr(s string) (netip.Addr, error) { return netip.ParseAddr(s) }

func hmacEqual(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }
