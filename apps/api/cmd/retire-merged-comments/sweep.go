package main

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const (
	entityTypeWork      int16 = 5
	threadKindComments  int16 = 1
	threadStatusDeleted int16 = 3
)

// anchorPlan says what an anchor id MEANS, which differs per anchor kind and
// decides both of this sweep's queries.
//
// claimAware is the difference that matters. A site-local kind carries the
// SITE's id, which for a claimed work is the claim's product_work_id -- a
// number from the product keyspace that may collide with an unrelated catalog
// id, so a live claim owning it means the anchor is alive. A catalog kind
// carries the catalog id itself, where the same exclusion would instead skip a
// genuinely dead anchor whose number some site happens to use as a gid.
type anchorPlan struct {
	entityType int16
	claimAware bool
	// catalogIDs marks an anchor id that is already a catalog id, so the
	// survivor's anchor on this site is the survivor's catalog id itself.
	catalogIDs bool
}

// Deliberately not exhaustive. anchor_kind 0 (board) and 2 (site_resource) name
// nothing in the catalog, and 4 (catalog_person) has no unambiguous entity type
// -- catalog splits a person (0) from the credit name (1) and community does
// not say which one it means. Guessing retires the wrong conversations, so an
// unmapped kind is an error rather than a default.
var anchorPlans = map[int16]anchorPlan{
	1: {entityType: entityTypeWork, claimAware: true},  // site_game
	3: {entityType: entityTypeWork, claimAware: false}, // catalog_work
}

// siteGameAnchorIsCatalogID names the sites whose site_game anchor carries the
// CATALOG work id rather than a gid of their own.
//
// This is knowledge about a site, not a run option: moyu's 铁律 3 says its page
// id IS the catalog work id (`cmd/align-patch-ids`, migrations 037/038 closed
// the legacy offset), and the forum joined it on 2026-09-23 (its G0 renumber,
// `align-galgame-ids`, which also set every kungal claim's product_work_id to
// the work id). The claim exclusion -- "this number is a product id that merely
// collides with a catalog id" -- is exactly wrong on both, and would leave a
// wall standing under a work that no longer exists. A flag would put that
// difference one typo away from a silent wrong sweep.
var siteGameAnchorIsCatalogID = map[string]bool{
	"moyu":   true,
	"kungal": true,
}

func planFor(site string, anchorKind int16) (anchorPlan, error) {
	plan, ok := anchorPlans[anchorKind]
	if !ok {
		return anchorPlan{}, fmt.Errorf("anchor kind %d names no catalog entity", anchorKind)
	}
	if anchorKind == 1 && siteGameAnchorIsCatalogID[site] {
		plan.claimAware = false
		plan.catalogIDs = true
	}
	return plan, nil
}

// stranded is one comments thread whose anchor names a catalog work that a
// merge retired: the page it hangs under is gone and nothing reaches the
// conversation any more.
type stranded struct {
	ThreadID       int64  `gorm:"column:thread_id"`
	Site           string `gorm:"column:site"`
	AnchorID       string `gorm:"column:anchor_id"`
	Posts          int64  `gorm:"column:posts"`
	Survivor       int64  `gorm:"column:survivor"`
	SurvivorAnchor string `gorm:"column:survivor_anchor"`
}

type sweepOpts struct {
	Site       string
	AnchorKind int16
	Limit      int
}

// liveAnchors reads the threads this sweep may act on: live comments threads on
// one site whose anchor is a bare number, that hold at least one post.
//
// "at least one post" is not a filter on importance, it is the line between
// this sweep and the lazy-comments wave. That wave retires the threads a
// get-or-create read face minted for anchors nobody ever wrote under
// (110,892 of the 114,047 rows in production on 2026-09-15). Emptiness is its
// whole criterion, so keeping `posts > 0` here means the two never contend for
// a row. Widening this to empty threads would put two writers on one table.
func liveAnchors(db *gorm.DB, o sweepOpts) ([]stranded, error) {
	var rows []stranded
	q := db.Raw(`
		SELECT t.id AS thread_id, t.site, t.anchor_id,
		       (SELECT count(*) FROM community_post p WHERE p.thread_id = t.id) AS posts
		FROM community_thread t
		WHERE t.site = ? AND t.anchor_kind = ? AND t.kind = ? AND t.status <> ?
		  AND t.anchor_id ~ '^[0-9]+$'
		  AND EXISTS (SELECT 1 FROM community_post p WHERE p.thread_id = t.id)
		ORDER BY t.id`,
		o.Site, o.AnchorKind, threadKindComments, threadStatusDeleted)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// strandedAmong asks the catalog which of those anchors name a work that is
// gone, and names the work that replaced it.
//
// The claim exclusion is the whole correctness of this sweep on a site whose
// anchor is its own game id: for a CLAIMED work that id is the claim's
// product_work_id -- a number from the product's keyspace that has nothing to
// do with the catalog id it happens to equal. Before its G0 renumber 10,289
// forum gids were also the catalog id of a different work, and the first
// census written without this clause reported kungal thread 1406 as stranded: its three posts are a live
// support thread about 光翼戦姫エクスティアコンチェルト1, whose gid 2656 belongs to
// work 2649, while catalog work 2656 is an unrelated merged-away work. Without
// this clause the sweep deletes live conversations and the report looks right.
func strandedAmong(db *gorm.DB, site string, anchorKind int16, rows []stranded) ([]stranded, error) {
	plan, err := planFor(site, anchorKind)
	if err != nil {
		return nil, err
	}
	out := make([]stranded, 0, len(rows))
	byAnchor := make(map[string]stranded, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		byAnchor[r.AnchorID] = r
		ids = append(ids, r.AnchorID)
	}
	for _, chunk := range chunkBy(ids, 500) {
		vals := make([]string, 0, len(chunk))
		args := []any{}
		for _, id := range chunk {
			vals = append(vals, "(?::bigint)")
			args = append(args, id)
		}
		args = append(args, plan.claimAware, site, plan.entityType, site)
		var found []struct {
			Anchor      string `gorm:"column:anchor"`
			Survivor    int64  `gorm:"column:survivor"`
			SurvivorGID *int64 `gorm:"column:survivor_gid"`
			ClaimedGID  bool   `gorm:"column:claimed_gid"`
		}
		err := db.Raw(`
			WITH a(id) AS (VALUES `+strings.Join(vals, ", ")+`)
			SELECT a.id::text AS anchor, r.current_id AS survivor,
			       s.product_work_id AS survivor_gid,
			       ? AND EXISTS (SELECT 1 FROM catalog_work c
			                WHERE c.product_work_id = a.id AND c.site = ?
			                  AND c.deleted_at IS NULL) AS claimed_gid
			FROM a
			JOIN catalog_redirect r ON r.entity_type = ? AND r.old_id = a.id
			LEFT JOIN catalog_work s ON s.id = r.current_id
			     AND s.deleted_at IS NULL AND s.site = ?`, args...).Scan(&found).Error
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			if f.ClaimedGID {
				continue
			}
			row := byAnchor[f.Anchor]
			row.Survivor = f.Survivor
			switch {
			case plan.catalogIDs:
				row.SurvivorAnchor = fmt.Sprintf("%d", f.Survivor)
			case f.SurvivorGID != nil:
				row.SurvivorAnchor = fmt.Sprintf("%d", *f.SurvivorGID)
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// retire soft-deletes the threads. status 3 is the only value the partial
// uniques exclude, so it can never break one, and the row keeps every post.
func retire(db *gorm.DB, rows []stranded) (int64, error) {
	var n int64
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ThreadID)
	}
	for _, chunk := range chunkBy(ids, 500) {
		res := db.Exec(`UPDATE community_thread SET status = ?, updated_at = now()
		                 WHERE id IN ? AND status <> ?`,
			threadStatusDeleted, chunk, threadStatusDeleted)
		if res.Error != nil {
			return n, res.Error
		}
		n += res.RowsAffected
	}
	return n, nil
}

func chunkBy[T any](in []T, size int) [][]T {
	var out [][]T
	for i := 0; i < len(in); i += size {
		end := min(i+size, len(in))
		out = append(out, in[i:end])
	}
	return out
}
