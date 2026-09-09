package repr

import "api/internal/platform/apiv2/problem"

type UserPlaytime struct {
	_       struct{} `json:"-" additionalProperties:"true"`
	Object  string   `json:"object" enum:"playtime" doc:"Type discriminant. Always playtime."`
	WorkID  string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id this row is about."`
	Minutes int      `json:"minutes" minimum:"0" doc:"Absolute cumulative minutes."`
}

type ClaimRecord struct {
	_             struct{}       `json:"-" additionalProperties:"true"`
	Object        string         `json:"object" enum:"claim" doc:"Type discriminant. Always claim."`
	ID            string         `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id this claim is on."`
	State         string         `json:"state" enum:"none,live,draft,pending,declined,hidden" doc:"Claim lifecycle state. none means the work is not currently claimed by any site."`
	DisplayName   string         `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Site          string         `json:"site" maxLength:"64" doc:"Claiming site key, empty when unclaimed. Open vocabulary; must not be used as a discriminant."`
	ProductWorkID *string        `json:"product_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The claiming site's own work id. null when unclaimed."`
	LastEvent     *ClaimEventRef `json:"last_event,omitempty" doc:"The newest claim event on this work, whoever acted. Present on the me faces and single-claim reads; absent on the moderation queue rows."`
	FirstActedAt  *string        `json:"first_acted_at,omitempty" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC of the bearer's first action on this work. Present on the me faces."`
	ActedCount    *int           `json:"acted_count,omitempty" minimum:"0" doc:"How many times the bearer acted on this work, within the site= scope when given. Present on the me faces."`
}

type ClaimEventRef struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Object    string   `json:"object" enum:"claim_event" doc:"Type discriminant. Always claim_event."`
	ID        string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Claim event id. Same id space as /v2/catalog/claim-events."`
	FromState *string  `json:"from_state" enum:"none,live,draft,pending,declined,hidden" doc:"State before the event. null on the event that created the claim."`
	ToState   string   `json:"to_state" enum:"none,live,draft,pending,declined,hidden" doc:"State after the event."`
	Reason    *string  `json:"reason" maxLength:"2000" doc:"Actor's note, usually a decline reason. null when none was given. Must not be used as a discriminant."`
	ActorUID  string   `json:"actor_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The claiming site's own user id of the actor — a moderator on a decision event, not necessarily the claim owner."`
	CreatedAt string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type ClaimEvent struct {
	_             struct{} `json:"-" additionalProperties:"true"`
	Object        string   `json:"object" enum:"claim_event" doc:"Type discriminant. Always claim_event."`
	ID            string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Claim event id. Ascending; the watermark a mirror stores."`
	WorkID        string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id the event is on."`
	FromState     *string  `json:"from_state" enum:"none,live,draft,pending,declined,hidden" doc:"State before the event. null on the event that created the claim."`
	ToState       string   `json:"to_state" enum:"none,live,draft,pending,declined,hidden" doc:"State after the event."`
	Reason        *string  `json:"reason" maxLength:"2000" doc:"Actor's note, usually a decline reason. null when none was given. Must not be used as a discriminant."`
	ActorUID      string   `json:"actor_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The claiming site's own user id of the actor. Not a catalog id."`
	Site          string   `json:"site" maxLength:"64" doc:"Tenant the event was recorded under. Open vocabulary; must not be used as a discriminant."`
	ProductWorkID *string  `json:"product_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The claim's CURRENT product-side id — a snapshot, not the value at event time. null when the work is unclaimed or gone."`
	CreatedAt     string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type PlaytimeBatchItem struct {
	_       struct{}         `json:"-" additionalProperties:"true"`
	Status  int              `json:"status" minimum:"0" maximum:"599" doc:"HTTP status for this item."`
	Object  *string          `json:"object,omitempty" enum:"playtime" doc:"Present on 200 items. Always playtime."`
	WorkID  *string          `json:"work_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog work id. Present on 200 items."`
	Minutes *int             `json:"minutes,omitempty" minimum:"0" doc:"Present on 200 items."`
	Problem *problem.Problem `json:"problem,omitempty" doc:"Present on failed items. A full problem object."`
}

type ProposalRecord struct {
	_               struct{}        `json:"-" additionalProperties:"true"`
	Object          string          `json:"object" enum:"proposal" doc:"Type discriminant. Always proposal."`
	ID              string          `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Proposal id."`
	State           string          `json:"state" enum:"open,merged,declined,withdrawn" doc:"Proposal lifecycle state."`
	TargetObject    string          `json:"target_object" enum:"work,company,character,release,tag,engine,series" doc:"Family of the entity this proposal targets."`
	EntityType      string          `json:"entity_type" maxLength:"64" doc:"Editing-engine type, e.g. catalog.work. Must not be used as a discriminant."`
	EntityID        string          `json:"entity_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Target catalog id."`
	Note            string          `json:"note" maxLength:"2000" doc:"Proposer's own summary. Must not be used as a discriminant."`
	ProposerUID     string          `json:"proposer_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The claiming site's own user id of the proposer. Not a catalog id."`
	Site            string          `json:"site" maxLength:"64" doc:"Tenant this proposal was filed under. Open vocabulary; must not be used as a discriminant."`
	BaseRevisionSeq int             `json:"base_revision_seq" minimum:"0" doc:"Revision seq this proposal was written against."`
	DecidedByUID    *string         `json:"decided_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"The claiming site's own user id of the decider. null while open."`
	DecidedAt       *string         `json:"decided_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null while open."`
	CreatedAt       string          `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	UpdatedAt       string          `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	Amendments      *[]Amendment    `json:"amendments,omitempty" doc:"Present when include=amendments. Empty array if none."`
	Patch           *map[string]any `json:"patch,omitempty" doc:"Present when include=patch, which only the me and moderation faces publish. Field key to proposed value."`
	EffectivePatch  *map[string]any `json:"effective_patch,omitempty" doc:"Present when include=patch. The patch after every amendment is folded in; this is what a merge would write."`
}

// Split per route for the reason D35 split the decision inputs: one record
// serving both routes can only carry the union of two state vocabularies, and a
// shared enum that advertises the other route's values is the defect D35
// records, not a smaller schema.
//
// unban has no fixed target — it restores whatever state the claim was hidden
// from — so a client that mapped decision to outcome told the moderator
// "published" for a claim that went back to pending. The service has carried
// both ends of the transition all along and the face dropped them.
type ClaimDecisionRecord struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Object    string   `json:"object" enum:"decision" doc:"Type discriminant. Always decision."`
	ID        string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Claim event id issued by this decision."`
	Decision  string   `json:"decision" enum:"approve,decline,ban,unban" doc:"approve publishes a pending claim, decline sends it back, ban hides it from any state, unban restores the state it was hidden from."`
	Note      string   `json:"note" maxLength:"2000" doc:"Must not be used as a discriminant."`
	FromState *string  `json:"from_state" enum:"none,live,draft,pending,declined,hidden" doc:"Claim state before this decision. null when the claim had none."`
	ToState   string   `json:"to_state" enum:"none,live,draft,pending,declined,hidden" doc:"Claim state after this decision. The authoritative outcome; do not derive it from decision, because unban has no fixed target."`
}

type ProposalDecisionRecord struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Object    string   `json:"object" enum:"decision" doc:"Type discriminant. Always decision."`
	ID        string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Proposal id this decision closed."`
	Decision  string   `json:"decision" enum:"merge,decline" doc:"merge writes the effective patch and records a revision. decline closes the proposal."`
	Note      string   `json:"note" maxLength:"2000" doc:"Must not be used as a discriminant."`
	FromState *string  `json:"from_state" enum:"open,merged,declined,withdrawn" doc:"Proposal state before this decision. null when it could not be read."`
	ToState   string   `json:"to_state" enum:"open,merged,declined,withdrawn" doc:"Proposal state after this decision, read back rather than derived from decision."`
}

type SnapshotRecord struct {
	_           struct{}       `json:"-" additionalProperties:"true"`
	Object      string         `json:"object" enum:"snapshot" doc:"Type discriminant. Always snapshot."`
	EntityType  string         `json:"entity_type" maxLength:"64" doc:"Editing-engine type. Must not be used as a discriminant."`
	EntityID    string         `json:"entity_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Target catalog id."`
	FieldValues map[string]any `json:"field_values" doc:"Registered field keys to current values. Empty object if none."`
}

type NewsSubmission struct {
	_           struct{}   `json:"-" additionalProperties:"true"`
	Object      string     `json:"object" enum:"news_submission" doc:"Type discriminant. Always news_submission."`
	ID          string     `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"News item id. Same id space as /v2/news."`
	Source      NewsSource `json:"source" doc:"The source row that grants this submission."`
	Lane        string     `json:"lane" enum:"news,column" doc:"Which of the source's two sections this item belongs to."`
	Status      string     `json:"status" enum:"pending,published,rejected,withdrawn" doc:"Moderation lifecycle state. POST always lands on pending."`
	Title       string     `json:"title" maxLength:"512" doc:"Must not be used as a discriminant."`
	Summary     string     `json:"summary" maxLength:"200" doc:"Lede, at most 200 runes. Must not be used as a discriminant."`
	SourceURL   string     `json:"source_url" format:"uri" maxLength:"1024" doc:"Canonical link to the original item."`
	Banner      *Image     `json:"banner" doc:"Lead image. null when there is none. B10: never a bare hash."`
	PublishedAt string     `json:"published_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	WorkIDs     []string   `json:"work_ids" doc:"Catalog work ids linked by hand. Empty array, never null."`
}

type CoverVote struct {
	_       struct{} `json:"-" additionalProperties:"true"`
	Object  string   `json:"object" enum:"cover_vote" doc:"Type discriminant. Always cover_vote."`
	CoverID string   `json:"cover_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog cover row id."`
	WorkID  string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Work the ballot is on."`
	Vote    string   `json:"vote" enum:"up" doc:"Only up is stored. down is not a catalog ballot."`
}

type UserFolder struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"folder" doc:"Type discriminant. Always folder."`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Folder id."`
	OwnerUID    string   `json:"owner_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The account that owns this folder. This is the central sign-in user id shared by every NextMoe site, not one site's own user id."`
	Name        string   `json:"name" maxLength:"100" doc:"Display name. Must not be used as a discriminant."`
	Description string   `json:"description" maxLength:"500" doc:"Owner's own note. Empty string when none. Must not be used as a discriminant."`
	Visibility  string   `json:"visibility" enum:"private,public" doc:"public folders are readable by anyone through /v2/folders; private ones are visible only to their owner, through /v2/me/folders."`
	IsDefault   bool     `json:"is_default" doc:"At most one folder per user carries this. Setting it on a folder clears it on the previous holder."`
	ItemCount   int      `json:"item_count" minimum:"0" doc:"Number of works in this folder."`
	CreatedAt   string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	UpdatedAt   string   `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type FolderHolding struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Object    string   `json:"object" enum:"folder_holding" doc:"Type discriminant. Always folder_holding."`
	WorkID    string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id that was asked about."`
	FolderIDs []string `json:"folder_ids" doc:"The caller's own folders holding this work, id-ascending. Never empty: a work no folder holds is left out of the list instead."`
}

type FolderHolder struct {
	_        struct{} `json:"-" additionalProperties:"true"`
	Object   string   `json:"object" enum:"folder_holder" doc:"Type discriminant. Always folder_holder."`
	OwnerUID string   `json:"owner_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"An account holding the work in at least one of its folders. This is the central sign-in user id shared by every NextMoe site, not one site's own user id."`
}

type FolderPurgeReceipt struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Object         string   `json:"object" enum:"folder_purge" doc:"Type discriminant. Always folder_purge."`
	OwnerUID       string   `json:"owner_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The account whose folders were removed."`
	FoldersDeleted int64    `json:"folders_deleted" minimum:"0" doc:"Folders removed. 0 when the account held none, which is not an error."`
	ItemsDeleted   int64    `json:"items_deleted" minimum:"0" doc:"Memberships removed with them."`
}

type UserFolderItem struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Object    string   `json:"object" enum:"folder_item" doc:"Type discriminant. Always folder_item."`
	FolderID  string   `json:"folder_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Folder this membership belongs to."`
	WorkID    string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id."`
	CreatedAt string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC when the work was added."`
	UpdatedAt string   `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. The incremental-sync watermark; re-adding an existing membership does not touch it."`
}

type FolderItemBatchItem struct {
	_       struct{}         `json:"-" additionalProperties:"true"`
	Status  int              `json:"status" minimum:"0" maximum:"599" doc:"HTTP status for this item."`
	Object  *string          `json:"object,omitempty" enum:"folder_item" doc:"Present on 200 items. Always folder_item."`
	WorkID  *string          `json:"work_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog work id. Present on 200 items."`
	Problem *problem.Problem `json:"problem,omitempty" doc:"Present on failed items. A full problem object."`
}

type UserWorkState struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Object     string   `json:"object" enum:"work_state" doc:"Type discriminant. Always work_state."`
	WorkID     string   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work id this state is on."`
	State      string   `json:"state" enum:"wish,doing,done,on_hold,dropped" doc:"Closed vocabulary. wish wants to play, doing is playing, done finished at least one route, on_hold paused, dropped abandoned. Maps one-to-one onto Bangumi collection types."`
	Completion *string  `json:"completion" enum:"one_route,main,all" doc:"How much is finished: one route, the main route, or every route. null when unstated. Never accompanies wish."`
	CreatedAt  string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	UpdatedAt  string   `json:"updated_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. The incremental-sync watermark."`
}

type WorkStateBatchItem struct {
	_          struct{}         `json:"-" additionalProperties:"true"`
	Status     int              `json:"status" minimum:"0" maximum:"599" doc:"HTTP status for this item."`
	Object     *string          `json:"object,omitempty" enum:"work_state" doc:"Present on 200 items. Always work_state."`
	WorkID     *string          `json:"work_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog work id. Present on 200 items."`
	State      *string          `json:"state,omitempty" enum:"wish,doing,done,on_hold,dropped" doc:"Present on 200 items."`
	Completion *string          `json:"completion,omitempty" enum:"one_route,main,all" doc:"Present on 200 items when set."`
	Problem    *problem.Problem `json:"problem,omitempty" doc:"Present on failed items. A full problem object."`
}
