package dto

type RatingBucket struct {
	Score int `json:"score" doc:"bucket value on the source-native scale"`
	Count int `json:"count" doc:"votes cast at this value"`
}

type RatingStats struct {
	Average *float64 `json:"average,omitempty" doc:"mean on the source-native scale (score itself is the median for erogamescape)"`
	Stdev   *float64 `json:"stdev,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

// WorkCharacterVA deliberately carries NO `identity`, and neither do the other
// roster-shaped voice projections (VoiceName, PublicRosterVoice,
// PublicVoiceName). Their SQL is SELECT DISTINCT over catalog_credit with no
// role filter, so one rendered row stands for 1..N credit rows with 1..N
// different identities. A single value would be a lie and an array would be
// worse: one click would suppress N rows the reader never saw. Suppressing a VA
// is done on /works/{id}/credits, which is 1:1 with the table.
type WorkCharacterVA struct {
	CreditNameID int64  `json:"credit_name_id"`
	Name         string `json:"name"`
}

// No `identity` here either — see WorkCharacterVA.
type VoiceName struct {
	CreditNameID int64  `json:"credit_name_id"`
	Name         string `json:"name"`
	Lang         string `json:"lang"`
	Latin        string `json:"latin,omitempty"`
}
