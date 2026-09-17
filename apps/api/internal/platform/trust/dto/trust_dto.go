package dto

import "time"

type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

type ReportRequest struct {
	SubjectKind string  `json:"subject_kind" doc:"registered subject kind for this site (e.g. forum_topic)"`
	SubjectID   string  `json:"subject_id" doc:"the subject's stable id in the product"`
	ReasonKey   string  `json:"reason_key" doc:"a global or per-site reason key (abuse/spam/…)"`
	Note        *string `json:"note,omitempty"`
	Snapshot    *string `json:"snapshot,omitempty" doc:"reporter-carried content snapshot at report time"`
	SubjectURL  *string `json:"subject_url,omitempty" doc:"deep link to the reported content in its product context (http/https, ≤512 chars); carried by the submitter because sub-entity subjects have no page derivable from subject_id alone"`
	ReporterID  int64   `json:"reporter_id" doc:"the reporting user's global id (P0 requires login)"`
}

type ReportResponse struct {
	ReportID     int64  `json:"report_id"`
	ReviewItemID *int64 `json:"review_item_id,omitempty" doc:"set when the report opened/linked/folded a review item"`
}

type ScanRequest struct {
	Site         string `json:"site,omitempty" doc:"optional tenant site (allowlist-gated relay path); omitted = derived from the client binding"`
	SubjectKind  string `json:"subject_kind" doc:"registered subject kind for this site (e.g. community_post)"`
	SubjectID    string `json:"subject_id" doc:"the subject's stable id in the product"`
	Text         string `json:"text" doc:"the UGC text to scan (capped at ~8000 runes; excess is truncated and recorded)"`
	AuthorID     *int64 `json:"author_id,omitempty" doc:"optional content author's global id (attribution/repeat-offender signal); not the tenant"`
	SubjectReach *int64 `json:"subject_reach,omitempty" doc:"optional audience the content has reached so far (views or the product's nearest equivalent); ranks the review queue — omitted or 0 contributes no boost"`
}

type ScanResponse struct {
	ScanID    int64 `json:"scan_id"`
	Truncated bool  `json:"truncated" doc:"true = the text exceeded the cap and was truncated before storage"`
}

type CheckRequest struct {
	Site     string `json:"site,omitempty" doc:"optional tenant site (allowlist-gated relay path); omitted = derived from the client binding"`
	Text     string `json:"text" doc:"the UGC text to check against the Tier0 word list"`
	AuthorID *int64 `json:"author_id,omitempty" doc:"optional content author's global id (accepted, not used in v0)"`
}

type CheckResponse struct {
	Decision string   `json:"decision" enum:"allow,deny,hold" doc:"allow=no match; hold=only suspect terms matched (route to review); deny=a banned term matched (block)"`
	Matched  []string `json:"matched" doc:"the matched normalized terms ([] when none)"`
}

type SubjectKindsResponse struct {
	Kinds []SubjectKindView `json:"kinds"`
}

type ReportReasonsResponse struct {
	Reasons []ReasonView `json:"reasons"`
}

type SubjectKindView struct {
	ID              int64     `json:"id"`
	Site            string    `json:"site"`
	Key             string    `json:"key"`
	CallbackURL     *string   `json:"callback_url,omitempty"`
	HasSecret       bool      `json:"has_secret"`
	IsDeprecated    bool      `json:"is_deprecated"`
	NotifyOnDismiss bool      `json:"notify_on_dismiss"`
	CreatedAt       time.Time `json:"created_at"`
}

type ReasonView struct {
	ID           int64   `json:"id"`
	Key          string  `json:"key"`
	NameCN       string  `json:"name_cn"`
	Site         *string `json:"site,omitempty"`
	Severity     int16   `json:"severity"`
	IsDeprecated bool    `json:"is_deprecated"`
}

type ReviewItemView struct {
	ID              int64      `json:"id"`
	Site            string     `json:"site"`
	SubjectKind     string     `json:"subject_kind"`
	SubjectID       string     `json:"subject_id"`
	Source          int16      `json:"source" doc:"0=reports 1=ai_text 2=ai_image 3=community_forward 4=mislabel 5=manual 6=ai_sample (a clean verdict drawn at random for calibration — never enforced)"`
	Severity        *int16     `json:"severity,omitempty"`
	ClassifierScore *float32   `json:"classifier_score,omitempty"`
	ReportWeightSum *float32   `json:"report_weight_sum,omitempty"`
	SubjectReach    *int64     `json:"subject_reach,omitempty" doc:"audience the content had reached when the item opened (snapshot); absent = the product does not report it"`
	Priority        float32    `json:"priority"`
	ContextNote     *string    `json:"context_note,omitempty" doc:"non-report evidence excerpt (community forward / ai_text)"`
	Status          int16      `json:"status" doc:"0=pending 1=claimed 2=actioned 3=dismissed"`
	ClaimedBy       *int64     `json:"claimed_by,omitempty"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	DecidedBy       *int64     `json:"decided_by,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type ReportView struct {
	ID              int64     `json:"id"`
	Site            string    `json:"site"`
	SubjectKind     string    `json:"subject_kind"`
	SubjectID       string    `json:"subject_id"`
	ReporterID      int64     `json:"reporter_id"`
	ReasonID        int64     `json:"reason_id"`
	Note            *string   `json:"note,omitempty"`
	SubjectSnapshot *string   `json:"subject_snapshot,omitempty"`
	SubjectURL      *string   `json:"subject_url,omitempty" doc:"submitter-carried deep link to the content in its product context"`
	Weight          float32   `json:"weight"`
	ReviewItemID    *int64    `json:"review_item_id,omitempty"`
	Status          int16     `json:"status" doc:"0=received 1=linked 2=folded"`
	CreatedAt       time.Time `json:"created_at"`
}

type ReviewItemDetail struct {
	Item    ReviewItemView `json:"item"`
	Reports []ReportView   `json:"reports"`
}

type DispositionView struct {
	ID               int64      `json:"id"`
	ReviewItemID     int64      `json:"review_item_id"`
	Action           int16      `json:"action" doc:"0=none 1=hide 2=remove 3=warn_user 4=restrict 5=escalate_idp"`
	ActedBy          int64      `json:"acted_by"`
	ReasonCode       string     `json:"reason_code"`
	Statement        *string    `json:"statement,omitempty"`
	CallbackStatus   *int16     `json:"callback_status,omitempty" doc:"0=pending 1=delivered 2=dead_letter; null=no callback"`
	CallbackAttempts int32      `json:"callback_attempts"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type DecideRequest struct {
	Decision   string  `json:"decision" enum:"dismissed,actioned"`
	Action     *int16  `json:"action,omitempty" doc:"required for actioned: 0=none 1=hide 2=remove 3=warn_user 4=restrict 5=escalate_idp"`
	ReasonCode string  `json:"reason_code,omitempty" doc:"required for actioned"`
	Statement  *string `json:"statement,omitempty" doc:"Art.17 reason statement (optional in P0)"`
}

type CreateSubjectKindRequest struct {
	Site            string  `json:"site" doc:"tenant site the kind belongs to"`
	Key             string  `json:"key"`
	CallbackURL     *string `json:"callback_url,omitempty"`
	CallbackSecret  *string `json:"callback_secret,omitempty"`
	NotifyOnDismiss *bool   `json:"notify_on_dismiss,omitempty" doc:"emit an action:0 callback on a dismissed decision (release holds); default false"`
}

type PatchSubjectKindRequest struct {
	CallbackURL     *string `json:"callback_url,omitempty"`
	CallbackSecret  *string `json:"callback_secret,omitempty"`
	IsDeprecated    *bool   `json:"is_deprecated,omitempty"`
	NotifyOnDismiss *bool   `json:"notify_on_dismiss,omitempty"`
}

type EnsureSubjectKindItem struct {
	Key             string  `json:"key" doc:"stable subject-kind key (e.g. forum_topic)"`
	CallbackURL     *string `json:"callback_url,omitempty" doc:"enforcement callback endpoint; omit to leave unchanged / unset"`
	CallbackSecret  *string `json:"callback_secret,omitempty" doc:"per-kind HMAC signing secret; omit to leave unchanged"`
	NotifyOnDismiss *bool   `json:"notify_on_dismiss,omitempty" doc:"emit an action:0 callback on a dismissed decision; omit to leave unchanged (false on create)"`
}

type EnsureSubjectKindsRequest struct {
	Kinds []EnsureSubjectKindItem `json:"kinds" doc:"declared subject kinds (≤50); converged per-kind, deprecated kinds are never revived"`
}

type BatchSubjectKindsRequest struct {
	Site  string                  `json:"site" doc:"tenant site the kinds belong to"`
	Kinds []EnsureSubjectKindItem `json:"kinds" doc:"declared subject kinds (≤50); converged per-kind, deprecated kinds are never revived"`
}

type EnsureSubjectKindResultView struct {
	Key    string `json:"key"`
	Result string `json:"result" enum:"created,updated,unchanged,deprecated_skipped" doc:"per-kind convergence outcome"`
}

type EnsureSubjectKindsResponse struct {
	Results []EnsureSubjectKindResultView `json:"results"`
}

type ForwardRequest struct {
	Site         string   `json:"site" doc:"tenant site the subject belongs to (allowlist-gated, not from the client binding)"`
	SubjectKind  string   `json:"subject_kind" doc:"registered subject kind (e.g. community_post)"`
	SubjectID    string   `json:"subject_id"`
	Severity     *int16   `json:"severity,omitempty"`
	WeightSum    *float32 `json:"weight_sum,omitempty" doc:"accumulated signal weight (idempotent update takes the max)"`
	ContextNote  *string  `json:"context_note,omitempty" doc:"reviewer-facing evidence excerpt"`
	SubjectReach *int64   `json:"subject_reach,omitempty" doc:"optional audience the content has reached (views or nearest equivalent); ranks the review queue and re-ranks an already-open item upward as the number grows"`
	ForwarderRef *string  `json:"forwarder_ref,omitempty" doc:"caller-side trace ref (informational)"`
}

type ForwardResponse struct {
	ReviewItemID int64 `json:"review_item_id"`
	Created      bool  `json:"created" doc:"false = an open item already existed and its signals were updated"`
}

type ForwardResolveRequest struct {
	ReviewItemID int64   `json:"review_item_id"`
	Outcome      string  `json:"outcome" enum:"approved,rejected" doc:"approved→dismissed, rejected→actioned"`
	ActorRef     *string `json:"actor_ref,omitempty" doc:"site-side actor ref (recorded in the audit policy_ref)"`
}

type ForwardResolveResponse struct {
	Closed bool `json:"closed" doc:"false = the item was already terminal (tolerated race)"`
}

type TermView struct {
	ID           int64     `json:"id"`
	Site         *string   `json:"site,omitempty" doc:"null = global (applies to every site)"`
	TermNorm     string    `json:"term_norm"`
	Kind         int16     `json:"kind" doc:"0=suspect (hold) 1=banned (deny)"`
	Purpose      int16     `json:"purpose" doc:"0=abuse 1=compliance (exempt from precision-based retirement)"`
	Note         *string   `json:"note,omitempty"`
	IsDeprecated bool      `json:"is_deprecated"`
	CreatedAt    time.Time `json:"created_at"`
}

type TermsResponse struct {
	Terms []TermView `json:"terms"`
	Total int64      `json:"total"`
}

type CreateTermRequest struct {
	Site    *string `json:"site,omitempty" doc:"tenant site; null = global (applies to every site)"`
	Term    string  `json:"term" doc:"the raw term (normalized server-side before storage)"`
	Kind    int16   `json:"kind" doc:"0=suspect (hold — enqueue, don't block) 1=banned (deny — the sync check rejects)"`
	Purpose int16   `json:"purpose,omitempty" doc:"0=abuse (default; precision-prunable against the AI classifier) 1=compliance (legal/regulatory — exempt from precision pruning, since the abuse classifier does not judge that question)"`
	Note    *string `json:"note,omitempty" doc:"optional operator memo"`
}

type CreateReasonRequest struct {
	Key      string  `json:"key"`
	NameCN   string  `json:"name_cn"`
	Site     *string `json:"site,omitempty"`
	Severity int16   `json:"severity"`
}

type PatchReasonRequest struct {
	NameCN       *string `json:"name_cn,omitempty"`
	Severity     *int16  `json:"severity,omitempty"`
	IsDeprecated *bool   `json:"is_deprecated,omitempty"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type SitePolicyView struct {
	Site               string    `json:"site"`
	ScanMode           *int16    `json:"scan_mode" doc:"0=shadow 1=live; null = inherit the platform default"`
	SampleRate         *float64  `json:"sample_rate" doc:"share of CLEAN verdicts drawn for human calibration; null = inherit"`
	FlagThreshold      *float32  `json:"flag_threshold" doc:"score at or above which this site treats content as flagged; null = defer to the AI gateway's own verdict"`
	AggregateThreshold *float32  `json:"aggregate_threshold" doc:"report weight that opens a review item; null = inherit"`
	AutoHideEnabled    *bool     `json:"auto_hide_enabled" doc:"may a live-mode flag queue a hide, or only open an item for a human; null = inherit"`
	Note               *string   `json:"note,omitempty" doc:"why this site is set the way it is"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PlatformDefaultsView struct {
	ScanMode           int16   `json:"scan_mode"`
	SampleRate         float64 `json:"sample_rate"`
	AggregateThreshold float32 `json:"aggregate_threshold"`
	AutoHideEnabled    bool    `json:"auto_hide_enabled"`
}

type SitePoliciesResponse struct {
	Policies []SitePolicyView     `json:"policies"`
	Defaults PlatformDefaultsView `json:"defaults"`
}

type UpsertSitePolicyRequest struct {
	ScanMode           *int16   `json:"scan_mode,omitempty" doc:"0=shadow 1=live; omit/null = inherit the platform default"`
	SampleRate         *float64 `json:"sample_rate,omitempty" doc:"omit/null = inherit"`
	FlagThreshold      *float32 `json:"flag_threshold,omitempty" doc:"omit/null = defer to the AI gateway's verdict"`
	AggregateThreshold *float32 `json:"aggregate_threshold,omitempty" doc:"omit/null = inherit"`
	AutoHideEnabled    *bool    `json:"auto_hide_enabled,omitempty" doc:"omit/null = inherit"`
	Note               *string  `json:"note,omitempty"`
}
