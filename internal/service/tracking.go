package service

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/njiwa-tz/mail/internal/db"
)

// Open and click tracking is opt-in per campaign, for written marketing
// messages only. It keeps no IP address or user agent, and the dashboard
// labels opens as estimates: mail clients that load images on their own
// (Apple Mail Privacy Protection) count as opens, and clients that block
// images do not.

var hrefRe = regexp.MustCompile(`(?i)(<a\b[^>]*?\bhref\s*=\s*)(["'])(https?://[^"'\s<>]+)(["'])`)

// storeCampaignLinks keeps the campaign's links by position. A click can
// only ever redirect to one of these.
func storeCampaignLinks(ctx context.Context, tx *db.Tx, campaignID, body string) error {
	seen := map[string]bool{}
	idx := 0
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		u := html.UnescapeString(m[3])
		if seen[u] || len(u) > 2000 {
			continue
		}
		seen[u] = true
		if _, err := tx.Exec(ctx, `INSERT INTO campaign_links(campaign_id, idx, url) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, campaignID, idx, u); err != nil {
			return err
		}
		idx++
		if idx >= 200 {
			break
		}
	}
	return nil
}

func (s *Service) trackToken(kind string, recipientID int64, idx int) string {
	body := strconv.FormatInt(recipientID, 36) + "." + strconv.Itoa(idx)
	mac := s.Box.MAC("track-"+kind, []byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac[:12])
}

func (s *Service) parseTrackToken(kind, token string) (int64, int, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, false
	}
	id, err1 := strconv.ParseInt(parts[0], 36, 64)
	idx, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || id <= 0 || idx < 0 {
		return 0, 0, false
	}
	if !hmac.Equal([]byte(s.trackToken(kind, id, idx)), []byte(token)) {
		return 0, 0, false
	}
	return id, idx, true
}

// applyTracking rewrites a recipient's HTML: links to the click redirect
// (only links stored for the campaign), and an open pixel at the end.
func (s *Service) applyTracking(body string, recipientID int64, links map[string]int, opens bool) string {
	base := strings.TrimRight(s.Opts.PublicURL, "/")
	if links != nil {
		body = hrefRe.ReplaceAllStringFunc(body, func(a string) string {
			m := hrefRe.FindStringSubmatch(a)
			idx, ok := links[html.UnescapeString(m[3])]
			if !ok {
				return a
			}
			return m[1] + m[2] + base + "/v1/t/c/" + s.trackToken("click", recipientID, idx) + m[4]
		})
	}
	if opens {
		px := `<img src="` + base + "/v1/t/o/" + s.trackToken("open", recipientID, 0) + `" width="1" height="1" alt="" style="display:block;width:1px;height:1px;border:0">`
		if i := strings.LastIndex(strings.ToLower(body), "</body>"); i >= 0 {
			body = body[:i] + px + body[i:]
		} else {
			body += px
		}
	}
	return body
}

func (s *Service) campaignLinks(ctx context.Context, campaignID string) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT idx, url FROM campaign_links WHERE campaign_id=$1`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var i int
		var u string
		if rows.Scan(&i, &u) == nil {
			out[u] = i
		}
	}
	return out, rows.Err()
}

// RecordOpen counts the first open of a recipient. Any bad token is
// ignored: the pixel is always served.
func (s *Service) RecordOpen(ctx context.Context, token string) {
	id, _, ok := s.parseTrackToken("open", token)
	if !ok {
		return
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO tracking_events(campaign_id, recipient_id, kind)
SELECT r.campaign_id, r.id, 'open' FROM campaign_recipients r JOIN campaigns c ON c.id=r.campaign_id
WHERE r.id=$1 AND c.track_opens AND NOT EXISTS (SELECT 1 FROM tracking_events t WHERE t.recipient_id=r.id AND t.kind='open')`, id)
}

// RecordClick counts a recipient's first click on a link and returns where
// to send the reader: always a link stored for that campaign, never a URL
// from the request.
func (s *Service) RecordClick(ctx context.Context, token string) (string, error) {
	id, idx, ok := s.parseTrackToken("click", token)
	if !ok {
		return "", newErr(404, "not_found")
	}
	var campaignID, url string
	err := s.Pool.QueryRow(ctx, `SELECT r.campaign_id::text, l.url FROM campaign_recipients r JOIN campaign_links l ON l.campaign_id=r.campaign_id AND l.idx=$2
WHERE r.id=$1`, id, idx).Scan(&campaignID, &url)
	if errors.Is(err, db.ErrNoRows) {
		return "", newErr(404, "not_found")
	}
	if err != nil {
		return "", unavailable("click", err)
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO tracking_events(campaign_id, recipient_id, kind, link_idx)
SELECT $1, $2, 'click', $3 WHERE NOT EXISTS (SELECT 1 FROM tracking_events t WHERE t.recipient_id=$2 AND t.kind='click' AND t.link_idx=$3)`, campaignID, id, idx)
	return url, nil
}

// TrackingStats are a campaign's unique opens and clicks, and clicks per link.
func (s *Service) trackingStats(ctx context.Context, campaignID string) (map[string]any, error) {
	var opens, clickers int64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind='open'),
  count(DISTINCT recipient_id) FILTER (WHERE kind='click') FROM tracking_events WHERE campaign_id=$1`, campaignID).Scan(&opens, &clickers); err != nil {
		return nil, unavailable("tracking", err)
	}
	links, err := s.maps(ctx, `SELECT l.idx, l.url, count(t.id) AS clicks FROM campaign_links l
LEFT JOIN tracking_events t ON t.campaign_id=l.campaign_id AND t.kind='click' AND t.link_idx=l.idx
WHERE l.campaign_id=$1 GROUP BY l.idx, l.url ORDER BY l.idx`, campaignID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"unique_opens": opens, "unique_clicks": clickers, "links": links,
		"opens_note": "Opens are an estimate: some mail apps load images on their own, others block them."}, nil
}
