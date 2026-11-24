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
	Evidence []Evidence      `json:"evidence" doc:"At least one item, except for a ref property, which is structural."`
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
	// IDs maps every ref of a written changeset to its canon id.
	IDs map[string]int64 `json:"ids,omitempty"`
}

// The pool API ingest serves on its private network and fanin calls; the
// direction is always fanin → pool (plan 05 §15).
const (
	PathOutbox   = "/pool/v0/outbox"
	PathSnapshot = "/pool/v0/snapshots/{hash}"
	PathRecord   = "/pool/v0/records/{id}"
	PathInbox    = "/pool/v0/inbox"
	PathBlock    = "/pool/v0/blocklist"
	PathRefs     = "/pool/v0/refs"
)

type OutboxEntry struct {
	ID        int64     `json:"id"`
	CreatedAt string    `json:"created_at" format:"date-time"`
	Row       OutboxRow `json:"row"`
}

type OutboxPage struct {
	Items     []OutboxEntry `json:"items"`
	NextAfter int64         `json:"next_after"`
}

type Snapshot struct {
	Hash         string   `json:"hash" doc:"sha256 of the lines joined by newline, hex."`
	RequestedURL string   `json:"requested_url"`
	FinalURL     string   `json:"final_url"`
	FetchedAt    string   `json:"fetched_at" format:"date-time"`
	Loaded       bool     `json:"loaded"`
	Cut          bool     `json:"cut"`
	Lines        []string `json:"lines"`
}

type Record struct {
	ID        string          `json:"id"`
	Source    string          `json:"source" doc:"Registered source key, e.g. ndl or jpro."`
	Key       string          `json:"key" doc:"The source's own key, e.g. an ISBN or a JP-e number."`
	Hash      string          `json:"hash"`
	FetchedAt string          `json:"fetched_at" format:"date-time"`
	Fields    json.RawMessage `json:"fields"`
}

type InboxBatch struct {
	Items []InboxRow `json:"items"`
}

// BlockItem is a takedown's effect on collection (plan 08 §4.3, §4.5): ingest
// must not bring the entity, asset or host back. Active false lifts it.
type BlockItem struct {
	Kind       string `json:"kind" enum:"entity,asset,host"`
	Entity     string `json:"entity,omitempty"`
	EntityID   int64  `json:"entity_id,omitempty"`
	Value      string `json:"value,omitempty" doc:"Asset hash or host name."`
	Reason     string `json:"reason"`
	TakedownID int    `json:"takedown_id"`
	Active     bool   `json:"active"`
}

type BlockBatch struct {
	Items []BlockItem `json:"items"`
}

// RefBatch is every canon id of one kind ingest may cite by name: fanin sends
// the whole list whenever it changes, and the pool replaces its own.
type RefBatch struct {
	Kind  string    `json:"kind" enum:"org"`
	Items []RefItem `json:"items"`
}

type RefItem struct {
	Name string `json:"name"`
	ID   int64  `json:"id"`
}
