// Package auditreport renders the audit log as a PDF report: a cover block with the
// filters, the entry count and the result of the integrity check, then every entry with
// its sequence number and hash, so a printed copy can be checked against the chain later.
package auditreport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/njiwa-tz/mail/internal/service"
)

// Report is everything one PDF shows.
type Report struct {
	GeneratedAt time.Time
	GeneratedBy string
	Filter      service.AuditFilter
	Entries     []service.AuditEntry
	Truncated   bool
	Max         int
	Integrity   service.AuditIntegrity
}

const (
	pageW, pageH = 841.89, 595.28 // A4 landscape, points
	margin       = 36.0
	bodyBottom   = pageH - 40
	lineH        = 10.5
	cellPad      = 4.0
)

type column struct {
	title string
	width float64
	mono  bool
}

var columns = []column{
	{"Seq", 44, true},
	{"Time (UTC)", 104, true},
	{"Actor", 122, false},
	{"Action", 116, false},
	{"Target", 118, false},
	{"Details", 196, false},
	{"Hash", 69.89, true},
}

type writer struct {
	pdf  *gopdf.GoPdf
	page int
	head string
	y    float64
}

// clean keeps text printable with the embedded fonts.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case unicode.IsControl(r) || r == unicode.ReplacementChar:
			return -1
		case r > 0x24F && !unicode.In(r, unicode.Greek, unicode.Cyrillic) && !unicode.IsPunct(r) && !unicode.IsSymbol(r):
			return '?'
		}
		return r
	}, s)
}

func (w *writer) font(family string, size float64) {
	_ = w.pdf.SetFont(family, "", size)
}

func (w *writer) color(r, g, b uint8) { w.pdf.SetTextColor(r, g, b) }

func (w *writer) text(x, y float64, s string) {
	w.pdf.SetXY(x, y)
	_ = w.pdf.Cell(nil, clean(s))
}

// lines wraps s to width; a word longer than the width is broken by character.
func (w *writer) lines(s string, width float64) []string {
	s = clean(s)
	if s == "" {
		return []string{""}
	}
	out, err := w.pdf.SplitTextWithWordWrap(s, width)
	if err != nil || len(out) == 0 {
		out, err = w.pdf.SplitText(s, width)
		if err != nil {
			return []string{strconv.QuoteToASCII(s)}
		}
	}
	return out
}

func (w *writer) footer() {
	w.font("regular", 7.5)
	w.color(110, 110, 120)
	w.text(margin, pageH-26, "Njiwa-Mail audit log report")
	w.text(margin+260, pageH-26, "Chain head "+short(w.head, 16))
	w.text(pageW-margin-60, pageH-26, fmt.Sprintf("Page %d", w.page))
}

func (w *writer) newPage() {
	if w.page > 0 {
		w.footer()
	}
	w.pdf.AddPage()
	w.page++
	w.y = margin
}

func (w *writer) tableHeader() {
	w.pdf.SetFillColor(240, 241, 244)
	w.pdf.RectFromUpperLeftWithStyle(margin, w.y, pageW-2*margin, 18, "F")
	w.font("bold", 8)
	w.color(60, 60, 70)
	x := margin
	for _, c := range columns {
		w.text(x+cellPad, w.y+5, c.title)
		x += c.width
	}
	w.y += 18
}

func short(h string, n int) string {
	if len(h) > n {
		return h[:n] + "…"
	}
	return h
}

func target(e service.AuditEntry) string {
	return strings.TrimSpace(e.TargetType + " " + e.TargetID)
}

func details(d map[string]any) string {
	if len(d) == 0 {
		return ""
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v, _ := json.Marshal(d[k])
		parts = append(parts, k+"="+string(v))
	}
	s := strings.Join(parts, ", ")
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}

func describeFilter(f service.AuditFilter) string {
	var p []string
	if f.Query != "" {
		p = append(p, fmt.Sprintf("search %q", f.Query))
	}
	if f.Action != "" {
		p = append(p, fmt.Sprintf("action starts with %q", f.Action))
	}
	if f.Actor != "" {
		p = append(p, fmt.Sprintf("actor %q", f.Actor))
	}
	if f.From != nil {
		p = append(p, "from "+f.From.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if f.To != nil {
		p = append(p, "before "+f.To.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if len(p) == 0 {
		return "none (every entry)"
	}
	return strings.Join(p, "; ")
}

// PDF renders the report.
func PDF(r Report) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}, Unit: gopdf.UnitPT})
	pdf.SetInfo(gopdf.PdfInfo{Title: "Njiwa-Mail audit log", Subject: "Audit log export", Creator: "Njiwa-Mail",
		Producer: "Njiwa-Mail", CreationDate: r.GeneratedAt})
	for name, data := range map[string][]byte{"regular": goregular.TTF, "bold": gobold.TTF, "mono": gomono.TTF} {
		if err := pdf.AddTTFFontData(name, data); err != nil {
			return nil, err
		}
	}
	w := &writer{pdf: pdf, head: r.Integrity.HeadHash}
	w.newPage()

	// Cover block.
	w.font("bold", 18)
	w.color(17, 17, 17)
	w.text(margin, w.y, "Audit log report")
	w.y += 26
	w.font("regular", 9.5)
	w.color(90, 90, 100)
	w.text(margin, w.y, "Every change made by a person or an API key, with the result of the tamper-evidence check.")
	w.y += 22

	status, sr, sg, sb := "VERIFIED", uint8(22), uint8(130), uint8(80)
	statusNote := fmt.Sprintf("All %d entries in the log match the hash chain.", r.Integrity.Entries)
	if !r.Integrity.OK {
		status, sr, sg, sb = "BROKEN", 190, 30, 45
		statusNote = fmt.Sprintf("Entry %d does not verify: %s.", r.Integrity.BrokenSeq, r.Integrity.Problem)
	}
	shown := fmt.Sprintf("%d", len(r.Entries))
	if r.Truncated {
		shown += fmt.Sprintf(" (the first %d that match; narrow the filters to export the rest)", r.Max)
	}
	seqRange := "—"
	if len(r.Entries) > 0 {
		seqRange = fmt.Sprintf("%d to %d", r.Entries[0].Seq, r.Entries[len(r.Entries)-1].Seq)
	}
	meta := [][2]string{
		{"Generated", r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC")},
		{"Generated by", r.GeneratedBy},
		{"Filters", describeFilter(r.Filter)},
		{"Entries in this report", shown},
		{"Sequence range", seqRange},
		{"Integrity", status + ". " + statusNote},
		{"Chain head hash", r.Integrity.HeadHash},
		{"Retention anchor", fmt.Sprintf("seq %d, hash %s", r.Integrity.AnchorSeq, short(r.Integrity.AnchorHash, 24))},
	}
	for _, m := range meta {
		w.font("bold", 9)
		w.color(60, 60, 70)
		w.text(margin, w.y, m[0])
		if m[0] == "Integrity" {
			w.color(sr, sg, sb)
		} else {
			w.color(17, 17, 17)
		}
		family := "regular"
		if strings.Contains(m[0], "hash") {
			family = "mono"
		}
		w.font(family, 9)
		for i, l := range w.lines(m[1], pageW-2*margin-140) {
			w.text(margin+140, w.y+float64(i)*12, l)
			if i > 0 {
				w.y += 12
			}
		}
		w.y += 15
	}
	w.font("regular", 8)
	w.color(110, 110, 120)
	w.text(margin, w.y+2, "Keep the chain head hash: if any entry up to it is later changed or removed, a new check will fail at that entry.")
	w.y += 24

	w.tableHeader()
	for i, e := range r.Entries {
		cells := []string{strconv.FormatInt(e.Seq, 10), e.At.UTC().Format("2006-01-02 15:04:05"), e.Actor, e.Action,
			target(e), details(e.Details), short(e.Hash, 12)}
		wrapped := make([][]string, len(cells))
		rows := 1
		for j, c := range cells {
			family := "regular"
			if columns[j].mono {
				family = "mono"
			}
			w.font(family, 7.5)
			wrapped[j] = w.lines(c, columns[j].width-2*cellPad)
			if len(wrapped[j]) > rows {
				rows = len(wrapped[j])
			}
		}
		h := float64(rows)*lineH + 6
		if w.y+h > bodyBottom {
			w.newPage()
			w.tableHeader()
		}
		if i%2 == 1 {
			w.pdf.SetFillColor(249, 249, 251)
			w.pdf.RectFromUpperLeftWithStyle(margin, w.y, pageW-2*margin, h, "F")
		}
		x := margin
		for j, lines := range wrapped {
			family := "regular"
			if columns[j].mono {
				family = "mono"
			}
			w.font(family, 7.5)
			w.color(30, 30, 35)
			if j == 0 || j == 6 {
				w.color(110, 110, 120)
			}
			for k, l := range lines {
				w.text(x+cellPad, w.y+3+float64(k)*lineH, l)
			}
			x += columns[j].width
		}
		w.y += h
		w.pdf.SetStrokeColor(232, 233, 237)
		w.pdf.SetLineWidth(0.4)
		w.pdf.Line(margin, w.y, pageW-margin, w.y)
	}
	if len(r.Entries) == 0 {
		w.font("regular", 9)
		w.color(110, 110, 120)
		w.text(margin, w.y+8, "No entries match these filters.")
	}
	w.footer()
	return pdf.GetBytesPdfReturnErr()
}
