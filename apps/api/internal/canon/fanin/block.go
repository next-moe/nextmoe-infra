package fanin

import (
	"context"
	"time"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
	enttakedown "api/internal/canon/ent/takedown"
)

// pushBlocklist sends takedowns and restores the pool has not heard of yet.
// Without it the next collection round would put the taken-down entry back.
func (f *Fanin) pushBlocklist(ctx context.Context) error {
	fresh, err := f.Client.Takedown.Query().Where(enttakedown.PushedAtIsNil()).All(ctx)
	if err != nil {
		return err
	}
	lifted, err := f.Client.Takedown.Query().Where(enttakedown.RestoredAtNotNil(), enttakedown.RestorePushedAtIsNil(),
		enttakedown.PushedAtNotNil()).All(ctx)
	if err != nil {
		return err
	}
	var items []contract.BlockItem
	for _, td := range fresh {
		items = append(items, blockItems(td, td.RestoredAt == nil)...)
	}
	for _, td := range lifted {
		items = append(items, blockItems(td, false)...)
	}
	if len(items) == 0 {
		return nil
	}
	if err := f.Pool.Block(ctx, items); err != nil {
		return err
	}
	now := time.Now()
	for _, td := range fresh {
		u := f.Client.Takedown.UpdateOneID(td.ID).SetPushedAt(now)
		if td.RestoredAt != nil {
			u = u.SetRestorePushedAt(now)
		}
		if err := u.Exec(ctx); err != nil {
			return err
		}
	}
	for _, td := range lifted {
		if err := f.Client.Takedown.UpdateOneID(td.ID).SetRestorePushedAt(now).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func blockItems(td *ent.Takedown, active bool) []contract.BlockItem {
	base := contract.BlockItem{Reason: td.Reason, TakedownID: td.ID, Active: active}
	var out []contract.BlockItem
	switch td.Scope {
	case "entity":
		it := base
		it.Kind, it.Entity, it.EntityID = "entity", td.Entity, td.EntityID
		out = append(out, it)
	case "asset":
		if td.AssetHash != "" {
			it := base
			it.Kind, it.Value = "asset", td.AssetHash
			out = append(out, it)
		}
	}
	if td.ExcludeHost != "" {
		it := base
		it.Kind, it.Value = "host", td.ExcludeHost
		out = append(out, it)
	}
	return out
}
