// Package takedown is notice-and-takedown for canon (plan 08): one call hides
// the target on every read face, holds it against agent writes and leaves a
// revision; restore undoes all three.
package takedown

import (
	"context"
	"errors"
	"fmt"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/ent/hold"
	enttakedown "api/internal/canon/ent/takedown"
)

const ScopeEntity = "entity"

var Scopes = map[string]bool{"asset": true, "media_set": true, "synopsis": true, ScopeEntity: true}

type Notice struct {
	NoticeID string
	Scope    string
	Entity   string
	EntityID int64
	Asset    string
	Reason   string
	By       string
	// ExcludeHost, when the rights holder asks that their site not be
	// crawled, becomes a host exclusion on the ingest side.
	ExcludeHost string
}

var ErrRestored = errors.New("takedown already restored")

func Take(ctx context.Context, client *ent.Client, n Notice) (*ent.Takedown, error) {
	if !Scopes[n.Scope] || n.NoticeID == "" || n.Reason == "" || n.By == "" {
		return nil, fmt.Errorf("a takedown needs a known scope, a notice id, a reason and who took it down")
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	td, err := tx.Takedown.Create().SetNoticeID(n.NoticeID).SetScope(n.Scope).SetEntity(n.Entity).SetEntityID(n.EntityID).
		SetAssetHash(n.Asset).SetReason(n.Reason).SetCreatedBy(n.By).SetExcludeHost(n.ExcludeHost).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Hold.Create().SetEntity(n.Entity).SetEntityID(n.EntityID).SetReason("takedown: " + n.Reason).
		SetCreatedBy(n.By).SetTakedownID(td.ID).Exec(ctx); err != nil {
		return nil, err
	}
	if err := tx.Revision.Create().SetEntity(n.Entity).SetEntityID(n.EntityID).SetRootEntity(n.Entity).SetRootID(n.EntityID).SetAction("takedown").
		SetActorKind("human").SetActorKey(n.By).SetNote(n.Reason).
		SetChanges(map[string]any{"notice_id": n.NoticeID, "scope": n.Scope, "asset": n.Asset, "takedown_id": td.ID}).Exec(ctx); err != nil {
		return nil, err
	}
	return td, tx.Commit()
}

func Restore(ctx context.Context, client *ent.Client, id int, by, note string) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	td, err := tx.Takedown.Get(ctx, id)
	if err != nil {
		return err
	}
	if td.RestoredAt != nil {
		return ErrRestored
	}
	now := time.Now()
	if err := tx.Takedown.UpdateOneID(id).SetRestoredAt(now).SetRestoredBy(by).SetRestoreNote(note).Exec(ctx); err != nil {
		return err
	}
	if err := tx.Hold.Update().Where(hold.TakedownID(id), hold.ReleasedAtIsNil()).SetReleasedAt(now).Exec(ctx); err != nil {
		return err
	}
	if err := tx.Revision.Create().SetEntity(td.Entity).SetEntityID(td.EntityID).SetRootEntity(td.Entity).SetRootID(td.EntityID).SetAction("restore").
		SetActorKind("human").SetActorKey(by).SetNote(note).SetChanges(map[string]any{"takedown_id": id}).Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// Gone reports an active whole-entity takedown, which read faces answer 410.
func Gone(ctx context.Context, client *ent.Client, entity string, id int64) (*ent.Takedown, error) {
	td, err := client.Takedown.Query().Where(enttakedown.Entity(entity), enttakedown.EntityID(id),
		enttakedown.Scope(ScopeEntity), enttakedown.RestoredAtIsNil()).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return td, err
}
