package main

import (
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// pgInt64Array binds an id list as ONE postgres array parameter. A bare slice
// makes GORM explode it into per-element parameters, and the full MT pool
// (176k works × 4 placeholders) blew pgx's 65,535-parameter ceiling on the
// first real-size run — the tests' small pools never reached it.
type pgInt64Array []int64

func (a pgInt64Array) Value() (driver.Value, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, v := range a {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatInt(v, 10))
	}
	sb.WriteByte('}')
	return sb.String(), nil
}

// A title is the one field where the neighbours matter: 「ロマンス」and its
// fandisc must not end up as 「浪漫」and 「罗曼史」. So the MT lane batches by
// series membership and by the direct sequel/fandisc/remake/same-series
// edges, and every member of a batch is translated with its siblings' titles —
// including the zh titles the catalog ALREADY has for them — in the prompt.

type siblingEdge struct {
	A int64 `gorm:"column:a"`
	B int64 `gorm:"column:b"`
}

// loadSeriesEdges returns the sibling pairs among the given works: same
// catalog_series membership, plus the direct sequel_of / fandisc_of /
// remake_of / same_series relations. Relation type ids are resolved by KEY —
// the numeric ids are seed data, not a contract.
func loadSeriesEdges(ctx context.Context, db *gorm.DB, workIDs []int64) ([]siblingEdge, error) {
	if len(workIDs) == 0 {
		return nil, nil
	}
	ids := pgInt64Array(workIDs)
	var edges []siblingEdge
	err := db.WithContext(ctx).Raw(`
		SELECT m1.work_id AS a, m2.work_id AS b
		FROM catalog_series_member m1
		JOIN catalog_series_member m2 ON m2.series_id = m1.series_id AND m2.work_id > m1.work_id
		WHERE m1.work_id = ANY(?::bigint[]) AND m2.work_id = ANY(?::bigint[])
		UNION
		SELECT least(r.a_work_id, r.b_work_id) AS a, greatest(r.a_work_id, r.b_work_id) AS b
		FROM catalog_work_relation r
		JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
		WHERE rt.key IN ('sequel_of','fandisc_of','remake_of','same_series')
		  AND r.a_work_id = ANY(?::bigint[]) AND r.b_work_id = ANY(?::bigint[])`,
		ids, ids, ids, ids).Scan(&edges).Error
	if err != nil {
		return nil, fmt.Errorf("load series edges: %w", err)
	}
	return edges, nil
}

// groupWorks partitions the ids into connected components over the sibling
// edges. Deterministic: components are keyed by their smallest member and
// returned in that order.
func groupWorks(workIDs []int64, edges []siblingEdge) [][]int64 {
	parent := make(map[int64]int64, len(workIDs))
	var find func(int64) int64
	find = func(x int64) int64 {
		p, ok := parent[x]
		if !ok || p == x {
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	union := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if ra > rb {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}
	for _, id := range workIDs {
		parent[id] = id
	}
	member := make(map[int64]bool, len(workIDs))
	for _, id := range workIDs {
		member[id] = true
	}
	for _, e := range edges {
		if member[e.A] && member[e.B] {
			union(e.A, e.B)
		}
	}
	byRoot := map[int64][]int64{}
	for _, id := range workIDs {
		r := find(id)
		byRoot[r] = append(byRoot[r], id)
	}
	roots := make([]int64, 0, len(byRoot))
	for r := range byRoot {
		roots = append(roots, r)
	}
	slices.Sort(roots)
	out := make([][]int64, 0, len(roots))
	for _, r := range roots {
		g := byRoot[r]
		slices.Sort(g)
		out = append(out, g)
	}
	return out
}

type mtBatch struct {
	Members []mtCandidate
	// Known is the zh titles the catalog already holds for works related to
	// this batch — the anchor the batch has to stay consistent with.
	Known []string
}

const defaultBatchSize = 12

// buildBatches turns the candidate list into series-coherent batches, keeping
// the popularity order of the candidate list: a batch takes the rank of its
// best-ranked member.
func buildBatches(cands []mtCandidate, edges []siblingEdge, known map[int64][]string, maxSize int) []mtBatch {
	if maxSize <= 0 {
		maxSize = defaultBatchSize
	}
	byID := make(map[int64]mtCandidate, len(cands))
	rank := make(map[int64]int, len(cands))
	ids := make([]int64, 0, len(cands))
	for i, c := range cands {
		byID[c.WorkID] = c
		rank[c.WorkID] = i
		ids = append(ids, c.WorkID)
	}
	groups := groupWorks(ids, edges)
	slices.SortStableFunc(groups, func(a, b []int64) int {
		return groupRank(a, rank) - groupRank(b, rank)
	})

	out := make([]mtBatch, 0, len(groups))
	for _, g := range groups {
		for chunk := range slices.Chunk(g, maxSize) {
			b := mtBatch{Members: make([]mtCandidate, 0, len(chunk))}
			seen := map[string]bool{}
			for _, id := range chunk {
				b.Members = append(b.Members, byID[id])
			}
			for _, id := range g {
				for _, k := range known[id] {
					if k == "" || seen[k] {
						continue
					}
					seen[k] = true
					b.Known = append(b.Known, k)
				}
			}
			out = append(out, b)
		}
	}
	return out
}

func groupRank(g []int64, rank map[int64]int) int {
	best := 1 << 30
	for _, id := range g {
		if r, ok := rank[id]; ok && r < best {
			best = r
		}
	}
	return best
}

// loadKnownZhTitles collects the Chinese titles already on the works related to
// the candidates — including the works that are NOT candidates, which is the
// point: a series whose first two entries were translated years ago is exactly
// the context the third one needs.
//
// The map is keyed by the CANDIDATE each title is context for, because that is
// the only key buildBatches ever looks up. The first version keyed it by the
// related work holding the title, so a translated non-candidate sibling — the
// headline case above — never reached any batch; the export test caught it
// before the lane's first prod run.
func loadKnownZhTitles(ctx context.Context, db *gorm.DB, workIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		CandID  int64   `gorm:"column:cand_id"`
		JaT     string  `gorm:"column:ja_title"`
		ZhT     string  `gorm:"column:zh_title"`
		Prov    int16   `gorm:"column:provenance"`
		SrcHash *string `gorm:"column:src_hash"`
	}
	ids := pgInt64Array(workIDs)
	err := db.WithContext(ctx).Raw(`
		WITH related AS (
			SELECT m1.work_id AS cand, m2.work_id AS id
			FROM catalog_series_member m1
			JOIN catalog_series_member m2 ON m2.series_id = m1.series_id
			WHERE m1.work_id = ANY(?::bigint[])
			UNION
			SELECT r.a_work_id AS cand, r.b_work_id AS id
			FROM catalog_work_relation r
			JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
			WHERE rt.key IN ('sequel_of','fandisc_of','remake_of','same_series') AND r.a_work_id = ANY(?::bigint[])
			UNION
			SELECT r.b_work_id AS cand, r.a_work_id AS id
			FROM catalog_work_relation r
			JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
			WHERE rt.key IN ('sequel_of','fandisc_of','remake_of','same_series') AND r.b_work_id = ANY(?::bigint[])
		)
		SELECT rel.cand AS cand_id,
			COALESCE((SELECT t.title FROM catalog_work_title t
			           WHERE t.work_id = rel.id AND (t.lang = 'ja' OR t.lang LIKE 'ja-%')
			             AND t.kind <= 1 ORDER BY t.kind, t.id LIMIT 1), '') AS ja_title,
			t.title AS zh_title, t.provenance, t.src_hash
		FROM related rel
		JOIN catalog_work_title t ON t.work_id = rel.id AND t.lang LIKE 'zh%' AND t.kind <= 1
		JOIN catalog_work w ON w.id = rel.id AND w.deleted_at IS NULL
		ORDER BY rel.cand, t.provenance, t.kind, t.id`,
		ids, ids, ids).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load known zh titles: %w", err)
	}
	for _, r := range rows {
		// A machine title whose src_hash no longer matches its ja title is about
		// to be retranslated; letting it into the context would anchor the fresh
		// translation on the very output being replaced.
		if r.Prov == model.WorkTitleProvenanceMachine &&
			(r.SrcHash == nil || *r.SrcHash != hashSource(cleanTitle(r.JaT))) {
			continue
		}
		line := r.ZhT
		if r.JaT != "" {
			line = r.JaT + " → " + r.ZhT
		}
		out[r.CandID] = append(out[r.CandID], line)
	}
	return out, nil
}
