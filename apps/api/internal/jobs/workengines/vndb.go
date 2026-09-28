package workengines

import (
	"context"
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

const writeBatch = 1000

type releaseRow struct {
	ReleaseID  int64  `gorm:"column:release_id"`
	WorkID     int64  `gorm:"column:work_id"`
	EngineID   *int64 `gorm:"column:engine_id"`
	VNDBEngine string `gorm:"column:vndb_engine"`
	Owner      string `gorm:"column:owner"`
	Kind       int16  `gorm:"column:kind"`
	Official   string `gorm:"column:official"`
}

func (r releaseRow) speaksForWork() bool {
	return r.Kind != model.ReleaseKindPatch && r.Official != "false"
}

type releaseWrite struct {
	releaseID int64
	oldID     *int64
	newID     *int64
}

// runVNDB also returns the works its releases leave with an engine that speaks
// for the work, so a dry run of both lanes can forecast the Bangumi lane
// against the state the apply would leave rather than the state it starts from.
func runVNDB(ctx context.Context, db *gorm.DB, opts Opts, v *vocab, st *Stats) (map[int64]struct{}, error) {
	var rows []releaseRow
	if err := db.WithContext(ctx).Raw(`
		SELECT rel.id AS release_id, rel.work_id, rel.engine_id, rl.engine AS vndb_engine,
		       coalesce(rel.field_provenance -> 'engine_id' -> 0 ->> 'source', '') AS owner,
		       rel.kind, coalesce(rel.extra ->> 'official', '') AS official
		FROM catalog_external_ref x
		JOIN catalog_release rel ON rel.id = x.entity_id AND rel.deleted_at IS NULL
		JOIN src_vndb.releases rl ON rl.id = x.external_id
		WHERE x.entity_type = ? AND x.source_id = ? AND x.link_kind = ?
		ORDER BY rel.id`,
		model.EntityTypeRelease, v.source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load vndb-anchored releases: %w", err)
	}
	st.Releases = len(rows)

	covered := map[int64]struct{}{}
	cover := func(r releaseRow, engine *int64) {
		if engine != nil && r.speaksForWork() {
			covered[r.WorkID] = struct{}{}
		}
	}
	var writes []releaseWrite
	for _, r := range rows {
		var want *int64
		if r.VNDBEngine != "" {
			id, ok, err := v.resolveVNDB(ctx, r.VNDBEngine)
			if err != nil {
				return nil, err
			}
			if !ok {
				st.UnknownVNDBEngine++
				cover(r, r.EngineID)
				continue
			}
			want = &id
		}
		if sameEngine(r.EngineID, want) {
			st.ReleasesSame++
			cover(r, want)
			continue
		}
		if provenance.IsHuman(r.Owner) {
			st.ReleasesHuman++
			cover(r, r.EngineID)
			continue
		}
		cover(r, want)
		switch {
		case r.EngineID == nil:
			st.ReleasesFilled++
		case want == nil:
			st.ReleasesCleared++
		default:
			st.ReleasesChanged++
		}
		writes = append(writes, releaseWrite{releaseID: r.ReleaseID, oldID: r.EngineID, newID: want})
	}
	if !opts.Apply {
		return covered, nil
	}
	for start := 0; start < len(writes); start += writeBatch {
		batch := writes[start:min(start+writeBatch, len(writes))]
		hosts, err := writeReleaseEngines(ctx, db, batch)
		if err != nil {
			return nil, err
		}
		st.ReleasesWritten += len(hosts)
		st.ReleasesLost += len(batch) - len(hosts)
		if err := repository.TouchWorks(ctx, db, hosts); err != nil {
			return nil, fmt.Errorf("touch works: %w", err)
		}
	}
	return covered, nil
}

func sameEngine(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func writeReleaseEngines(ctx context.Context, db *gorm.DB, batch []releaseWrite) ([]int64, error) {
	values := make([]string, 0, len(batch))
	args := make([]any, 0, len(batch)*3+1)
	for _, w := range batch {
		values = append(values, "(?::bigint, ?::bigint, ?::bigint)")
		args = append(args, w.releaseID, w.oldID, w.newID)
	}
	args = append(args, provenance.HumanSources())
	var hosts []int64
	if err := db.WithContext(ctx).Raw(`
		UPDATE catalog_release r SET engine_id = v.new_id
		FROM (VALUES `+strings.Join(values, ", ")+`) AS v(id, old_id, new_id)
		WHERE r.id = v.id AND r.deleted_at IS NULL
		  AND r.engine_id IS NOT DISTINCT FROM v.old_id
		  AND coalesce(r.field_provenance -> 'engine_id' -> 0 ->> 'source', '') NOT IN ?
		RETURNING r.work_id`, args...).Scan(&hosts).Error; err != nil {
		return nil, fmt.Errorf("write release engines: %w", err)
	}
	return hosts, nil
}
