package main

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// entityTypeWork is catalog_external_ref / catalog_redirect's entity_type for
// a work. The importer reads those tables directly rather than through the
// service, so the constant has to be restated here.
const entityTypeWork = 5

type workResolver struct {
	live     map[int64]struct{}
	redirect map[int64]int64
	vndb     map[string]int64
}

func newWorkResolver(cat *gorm.DB) (*workResolver, error) {
	w := &workResolver{
		live:     map[int64]struct{}{},
		redirect: map[int64]int64{},
		vndb:     map[string]int64{},
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
	var anchors []struct {
		ExternalID string
		EntityID   int64
	}
	// link_kind 0 is the exact anchor. A probable anchor is a guess, and a
	// guess that lands someone's favorite on the wrong work is worse than a
	// favorite that does not migrate.
	if err := cat.Raw(`
		SELECT r.external_id, r.entity_id
		FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		WHERE s.key = 'vndb' AND r.entity_type = ? AND r.link_kind = 0 AND r.dead_at IS NULL`,
		entityTypeWork).Scan(&anchors).Error; err != nil {
		return nil, err
	}
	for _, a := range anchors {
		w.vndb[a.ExternalID] = a.EntityID
	}
	return w, nil
}

func (w *workResolver) liveCount() int     { return len(w.live) }
func (w *workResolver) redirectCount() int { return len(w.redirect) }
func (w *workResolver) anchorCount() int   { return len(w.vndb) }

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

type vndbOutcome int

const (
	vndbResolved vndbOutcome = iota
	vndbPending
	vndbNoAnchor
)

// resolveVNDB turns moyu's patch.vndb_id into a live work. Three shapes reach
// it: a real v-number, a "wiki-<catalog work id>" written when moyu adopted a
// work that had no vndb entry, and a "pending-<n>" placeholder standing in for
// a work whose number has not been assigned yet.
func (w *workResolver) resolveVNDB(raw string) (int64, bool, vndbOutcome) {
	id := strings.TrimSpace(raw)
	if strings.HasPrefix(id, "pending-") {
		return 0, false, vndbPending
	}
	if rest, cut := strings.CutPrefix(id, "wiki-"); cut {
		n, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return 0, false, vndbNoAnchor
		}
		work, ok, redirected := w.resolve(n)
		if !ok {
			return 0, redirected, vndbNoAnchor
		}
		return work, redirected, vndbResolved
	}
	anchor, has := w.vndb[id]
	if !has {
		return 0, false, vndbNoAnchor
	}
	work, ok, redirected := w.resolve(anchor)
	if !ok {
		return 0, redirected, vndbNoAnchor
	}
	return work, redirected, vndbResolved
}
