package dto

import "time"

// AdminVerdict is what the machine graders said about one version of an item.
// AIFlagged is a pointer all the way out to the wire: a console that renders a
// missing verdict as "clean" would undo the entire point of the gate.
type AdminVerdict struct {
	ID                 int64     `json:"id"`
	ContentFingerprint string    `json:"content_fingerprint"`
	Tier0Decision      string    `json:"tier0_decision"`
	Tier0Matched       []string  `json:"tier0_matched,omitempty"`
	AIFlagged          *bool     `json:"ai_flagged"`
	AIScore            *float32  `json:"ai_score"`
	AICategories       []string  `json:"ai_categories,omitempty"`
	AIChannel          string    `json:"ai_channel,omitempty"`
	Degraded           bool      `json:"degraded"`
	DegradedReason     string    `json:"degraded_reason,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	// Current is false once the item's text has moved on: the verdict judged
	// something that is no longer on the page.
	Current bool `json:"current"`
}

type AdminDecision struct {
	ID         int64     `json:"id"`
	ActorUID   int64     `json:"actor_uid"`
	FromStatus int16     `json:"from_status"`
	ToStatus   int16     `json:"to_status"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type AdminNewsItem struct {
	ID               int64      `json:"id"`
	SourceKey        string     `json:"source_key"`
	Lane             string     `json:"lane"`
	UpstreamCategory string     `json:"upstream_category,omitempty"`
	ExternalID       string     `json:"external_id"`
	Title            string     `json:"title"`
	Preview          string     `json:"preview"`
	SourceURL        string     `json:"source_url"`
	BannerURL        string     `json:"banner_url,omitempty"`
	PublishedAt      time.Time  `json:"published_at"`
	Status           int16      `json:"status"`
	DeadAt           *time.Time `json:"dead_at"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	SubmitterUID     *int64     `json:"submitter_uid"`
	// Verdict is the newest verdict for the item's CURRENT text, absent only when
	// that text has never been graded at all. It may itself be degraded, which is
	// not a judgement — see Degraded.
	Verdict *AdminVerdict `json:"verdict"`
	// Attempts counts verdicts against the current text; at the ceiling the item
	// is no longer re-queued and needs a human regardless of what the AI thinks.
	Attempts int `json:"attempts"`
}

type AdminNewsItemDetail struct {
	AdminNewsItem
	Body      string          `json:"body"`
	Verdicts  []AdminVerdict  `json:"verdicts"`
	Decisions []AdminDecision `json:"decisions"`
}

type AdminNewsQueueData struct {
	Items []AdminNewsItem `json:"items"`
	Total int64           `json:"total"`
}

type AdminNewsStatsData struct {
	// ByStatus and ByLane are keyed by the wire value so the console does not
	// have to know the enum to render a count.
	ByStatus map[string]int64 `json:"by_status"`
	ByLane   map[string]int64 `json:"by_lane"`
	// Ungraded is the number of pending items whose current text has no
	// non-degraded verdict — the queue behind the queue.
	Ungraded int64 `json:"ungraded"`
	Degraded int64 `json:"degraded"`
}

type AdminDecisionRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}
