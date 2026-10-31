package model

const (
	ReportStatusReceived int16 = 0
	ReportStatusLinked   int16 = 1
	ReportStatusFolded   int16 = 2
)

const (
	ReviewSourceReports          int16 = 0
	ReviewSourceAIText           int16 = 1
	ReviewSourceAIImage          int16 = 2
	ReviewSourceCommunityForward int16 = 3
	ReviewSourceMislabel         int16 = 4
	ReviewSourceManual           int16 = 5
	ReviewSourceAISample         int16 = 6
	ReviewSourceSkippedCheck     int16 = 7
)

const (
	ReviewStatusPending   int16 = 0
	ReviewStatusClaimed   int16 = 1
	ReviewStatusActioned  int16 = 2
	ReviewStatusDismissed int16 = 3
)

const (
	ActionNone        int16 = 0
	ActionHide        int16 = 1
	ActionRemove      int16 = 2
	ActionWarnUser    int16 = 3
	ActionRestrict    int16 = 4
	ActionEscalateIDP int16 = 5
)

const (
	CallbackStatusPending    int16 = 0
	CallbackStatusDelivered  int16 = 1
	CallbackStatusDeadLetter int16 = 2
)

// Scan status (trust_scan_result.status) — the AI shadow-scoring pipeline (step
// 03). pending is the fresh intake; the scoring worker drives it to scored (the
// gateway returned a verdict) or degraded (unconfigured / failed / the gateway's
// own fail-open).
//
// Only scored is terminal. degraded was ALSO terminal until 2026-08-07, on the
// reasoning that a queue which never re-claims can never back up — true, but it
// bought that guarantee by discarding content permanently. A transient upstream
// blip became an unjudged item forever, and because the drain was silent nobody
// learned it had happened. The bound now comes from maxScanAttempts, spread over
// scanRetryBackoff through next_scan_at: the queue still drains, but only after
// the work has actually been tried across a window an outage has to outlast.
// Unconfigured is the exception: it drains on the first attempt, since nothing
// changes until a restart with credentials.
const (
	ScanStatusPending  int16 = 0
	ScanStatusScored   int16 = 1
	ScanStatusDegraded int16 = 2
)

const (
	ScanDegradedGatewayUnconfigured int16 = 1
	ScanDegradedGatewayCallFailed   int16 = 2
	ScanDegradedGatewayDegraded     int16 = 3
)

const (
	ScanModeShadow int16 = 0
	ScanModeLive   int16 = 1
)

const (
	TermKindSuspect int16 = 0
	TermKindBanned  int16 = 1
)

const (
	TermPurposeAbuse      int16 = 0
	TermPurposeCompliance int16 = 1
)
