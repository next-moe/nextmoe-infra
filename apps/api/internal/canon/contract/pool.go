package contract

import "encoding/json"

// The pool rows ingest writes (outbox) and fanin answers (inbox), plan 05 §15.

type OutboxRow struct {
	ContractVersion string      `json:"contract_version"`
	Kind            string      `json:"kind" enum:"candidate,changeset,lead_result"`
	Program         string      `json:"program" doc:"Registered program key (plan 05 §9)."`
	LookupID        string      `json:"lookup_id,omitempty" doc:"The lookup session that produced the row."`
	Candidate       *Candidate  `json:"candidate,omitempty"`
	Changeset       *Changeset  `json:"changeset,omitempty"`
	LeadResult      *LeadResult `json:"lead_result,omitempty"`
}

type Target struct {
	Entity string `json:"entity" doc:"Entity name from this contract."`
	ID     int64  `json:"id,omitempty" doc:"An existing canon id."`
	Ref    string `json:"ref,omitempty" doc:"A ref of an entity created in the same changeset."`
}

type Evidence struct {
	SnapshotID string `json:"snapshot_id,omitempty" doc:"Content hash of the snapshot."`
	LineFrom   int    `json:"line_from,omitempty" minimum:"1"`
	LineTo     int    `json:"line_to,omitempty" minimum:"1"`
	RecordID   string `json:"record_id,omitempty" doc:"A registry record, instead of a snapshot."`
	Path       string `json:"path,omitempty" doc:"Field path inside the record."`
}

type Candidate struct {
	Target   Target          `json:"target"`
	Property string          `json:"property"`
	Value    json.RawMessage `json:"value"`
	Tier     string          `json:"tier"`
	Evidence []Evidence      `json:"evidence" minItems:"1"`
}

type NewEntity struct {
	Ref        string               `json:"ref"`
	Entity     string               `json:"entity"`
	Properties map[string]Candidate `json:"properties" doc:"Keyed by property; each value carries its own evidence."`
}

type Changeset struct {
	LeadID   string      `json:"lead_id"`
	Entities []NewEntity `json:"entities" minItems:"1"`
}

type LeadResult struct {
	LeadID  string `json:"lead_id"`
	Outcome string `json:"outcome" enum:"verified,not_eligible,unsure,blocked"`
	Reason  string `json:"reason,omitempty"`
}

type InboxRow struct {
	OutboxID int64  `json:"outbox_id"`
	Result   string `json:"result" enum:"written,todo,refused"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
	EntityID int64  `json:"entity_id,omitempty"`
}
