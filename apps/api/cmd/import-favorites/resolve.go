package main

import (
	"gorm.io/gorm"
)

// entityTypeWork is catalog_redirect's entity_type for a work. The importer
// reads that table directly rather than through the service, so the constant
// has to be restated here.
const entityTypeWork = 5

type workResolver struct {
	live     map[int64]struct{}
	redirect map[int64]int64
}

func newWorkResolver(cat *gorm.DB) (*workResolver, error) {
	w := &workResolver{
		live:     map[int64]struct{}{},
		redirect: map[int64]int64{},
	}
	var ids []int64
	if err := cat.Raw(`SELECT id FROM catalog_work WHERE status = 0`).Scan(&ids).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		w.live[id] = struct{}{}
	}
	var reds []struct {
		OldID     int64
		CurrentID int64
	}
	if err := cat.Raw(`SELECT old_id, current_id FROM catalog_redirect WHERE entity_type = ?`,
		entityTypeWork).Scan(&reds).Error; err != nil {
		return nil, err
	}
	for _, r := range reds {
		w.redirect[r.OldID] = r.CurrentID
	}
	return w, nil
}

func (w *workResolver) liveCount() int     { return len(w.live) }
func (w *workResolver) redirectCount() int { return len(w.redirect) }

// resolve follows a merged work to its survivor. Chains are collapsed by
// merge_rehang, so the loop is a guard against a chain that outlives one
// merge, not an expected path.
func (w *workResolver) resolve(id int64) (out int64, ok bool, redirected bool) {
	for hop := 0; hop < 8; hop++ {
		if _, live := w.live[id]; live {
			return id, true, redirected
		}
		next, has := w.redirect[id]
		if !has || next == id {
			return 0, false, redirected
		}
		id, redirected = next, true
	}
	return 0, false, redirected
}
