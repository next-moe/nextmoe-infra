package service

import "time"

const foldWindow = 30 * 24 * time.Hour

var callbackBackoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	25 * time.Minute,
	2 * time.Hour,
	10 * time.Hour,
}

const callbackMaxAttempts = 5

// Three tries a minute apart fit inside one Workers AI capacity burst: from
// 2026-10-27 the 429 (code 4006) episodes used them up and left 220 forum items,
// ad posts among them, never scored. The schedule spans ~45h so an outage has
// to outlast it before content goes unjudged.
var scanRetryBackoff = [...]time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

const maxScanAttempts = int16(len(scanRetryBackoff) + 1)

const (
	maxScanTextRunes = 8000

	scanBatchSize = 20
	scanInterval  = 60 * time.Second

	maxScanSampleRate = 0.05

	scanSamplePriority float32 = 0.05
)
