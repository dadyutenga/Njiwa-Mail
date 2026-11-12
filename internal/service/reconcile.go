package service

import "context"

// F02: reconciliation queue. A message is `unknown` when the worker could not
// tell whether the mail server accepted it; it is never retried on its own.
// The operator decides. The queue helps by reading the evidence we hold for the
// message (the queue id from the SMTP reply, mail server log events, bounce
// reports) and suggesting an outcome. A suggestion is only a suggestion: it is
// recorded when the operator confirms, with a note that names its basis, and
// no evidence ever changes a message's state by itself.

// Suggestion is a proposed resolution of an unknown message.
type Suggestion struct {
	// Outcome is "submitted", "failed", or "" when the right move is to wait or investigate.
	Outcome string `json:"outcome"`
	// Strength is "strong" (the mail server log or a bounce says so), "weak" or "none".
	Strength string `json:"strength"`
	Basis    string `json:"basis"`
	Note     string `json:"note"`
}

type evidenceRow struct {
	Event, Source string
}

// SuggestOutcome derives a suggestion from a message's queue id and its
// delivery evidence (oldest first). It is a pure function so the rules are
// testable and obvious.
func SuggestOutcome(queueID string, evidence []evidenceRow) Suggestion {
	var accepted, bounced, deferred bool
	var bounceSource string
	for _, ev := range evidence {
		switch ev.Event {
		case "remote_accepted":
			accepted = true
		case "bounced", "expired_in_queue":
			bounced, bounceSource = true, ev.Source
		case "deferred":
			deferred = true
		}
	}
	q := ""
	if queueID != "" {
		q = " (queue ID " + queueID + ")"
	}
	switch {
	case bounced && accepted:
		return Suggestion{Strength: "none", Basis: "The evidence conflicts: a server accepted it and a bounce or expiry was also seen. Read the mail server log before deciding."}
	case bounced:
		src := map[string]string{"dsn": "a bounce report", "mta_log": "the mail server log"}[bounceSource]
		if src == "" {
			src = "the mail server"
		}
		return Suggestion{Outcome: "failed", Strength: "strong",
			Basis: "The recipient's side refused it or the mail server gave up, according to " + src + ".",
			Note:  "failed per " + src + q}
	case accepted:
		return Suggestion{Outcome: "submitted", Strength: "strong",
			Basis: "The mail server log shows the recipient's mail server accepted it. This is not proof of inbox placement.",
			Note:  "submitted per mail server log: remote accepted" + q}
	case deferred:
		return Suggestion{Strength: "none", Basis: "The mail server has it and is still retrying. Wait; the log will show the final result."}
	case queueID != "":
		return Suggestion{Outcome: "submitted", Strength: "weak",
			Basis: "The mail server's reply named a queue ID, so it took the message, but there is no log evidence of what happened next.",
			Note:  "submitted: server reply named queue ID " + queueID}
	}
	return Suggestion{Strength: "none", Basis: "No evidence yet. Check that the mail server log collector is running, then search the mail server's log for this message."}
}

// ReconciliationQueue lists the unknown messages of a project environment with
// a suggested outcome for each, oldest first (the longest-waiting come first).
func (s *Service) ReconciliationQueue(ctx context.Context, sc *Scope, pg Page) ([]map[string]any, bool, error) {
	items, more, err := s.page(ctx, pg, `SELECT m.id::text AS id, m.template, m.recipient_masked AS recipient, m.created_at, m.updated_at,
  m.attempts, m.mta_queue_id, m.smtp_reply
FROM messages m WHERE m.environment_id=$1 AND m.state='unknown' ORDER BY m.updated_at, m.id`, sc.EnvID)
	if err != nil {
		return nil, false, err
	}
	for _, it := range items {
		id, _ := it["id"].(string)
		rows, err := s.Pool.Query(ctx, `SELECT event, source FROM delivery_events WHERE message_id=$1 ORDER BY observed_at, id`, id)
		if err != nil {
			return nil, false, unavailable("reconcile", err)
		}
		var ev []evidenceRow
		for rows.Next() {
			var r evidenceRow
			if err := rows.Scan(&r.Event, &r.Source); err != nil {
				rows.Close()
				return nil, false, unavailable("reconcile", err)
			}
			ev = append(ev, r)
		}
		rows.Close()
		q, _ := it["mta_queue_id"].(string)
		sg := SuggestOutcome(q, ev)
		it["suggestion"] = sg
		it["evidence_count"] = len(ev)
		if len(sg.Note) > 120 {
			sg.Note = sg.Note[:120]
			it["suggestion"] = sg
		}
	}
	return items, more, nil
}
