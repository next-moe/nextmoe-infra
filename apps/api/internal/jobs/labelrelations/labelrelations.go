package labelrelations

import (
	"context"
	"fmt"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const vndbSourceKey = "vndb"

const matchedBy = "rule:vndb-producer-relation"

const insertBatch = 1000

type Opts struct {
	Apply bool
	DSN   string
}

type Stats struct {
	EdgesTotal             int
	BothAnchored           int
	Written                int
	SkippedUnanchored      int
	SkippedUnknownRelation int
	SkippedSelf            int
	SkippedSuppressed      int
	Deleted                int64
}

func Run(ctx context.Context, opts Opts) (Stats, error) {
	var st Stats
	if opts.DSN == "" {
		return st, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess the target database")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return st, fmt.Errorf("open catalog pool: %w", err)
	}
	return build(ctx, db, opts.Apply)
}

func build(ctx context.Context, db *gorm.DB, apply bool) (Stats, error) {
	var st Stats
	sourceID, err := resolveSourceID(ctx, db, vndbSourceKey)
	if err != nil {
		return st, err
	}
	anchors, err := loadLabelAnchors(ctx, db, sourceID)
	if err != nil {
		return st, err
	}
	edges, err := loadProducerRelations(ctx, db)
	if err != nil {
		return st, err
	}
	st.EdgesTotal = len(edges)

	suppressed, err := editspec.LoadSuppressedLabelRelations(ctx, db)
	if err != nil {
		return st, err
	}
	rows := dropSuppressed(project(edges, anchors, sourceID, &st), suppressed, &st)
	if !apply {
		return st, nil
	}
	if err := rebuild(ctx, db, sourceID, rows, &st); err != nil {
		return st, err
	}
	return st, nil
}

type producerRelation struct {
	ID       string `gorm:"column:id"`
	PID      string `gorm:"column:pid"`
	Relation string `gorm:"column:relation"`
}

func resolveSourceID(ctx context.Context, db *gorm.DB, key string) (int16, error) {
	var id int16
	if err := db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("resolve source %q: %w", key, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("source %q not seeded — run migrate-catalog first", key)
	}
	return id, nil
}

func loadLabelAnchors(ctx context.Context, db *gorm.DB, sourceID int16) (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.external_id, r.entity_id
		FROM catalog_external_ref r
		JOIN catalog_label l ON l.id = r.entity_id AND l.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeLabel, sourceID, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load label anchors: %w", err)
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.EntityID
	}
	return m, nil
}

func loadProducerRelations(ctx context.Context, db *gorm.DB) ([]producerRelation, error) {
	var rows []producerRelation
	if err := db.WithContext(ctx).Raw(
		`SELECT id, pid, relation FROM src_vndb.producers_relations ORDER BY id, pid, relation`,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load producers_relations: %w", err)
	}
	return rows, nil
}

func project(edges []producerRelation, anchors map[string]int64, sourceID int16, st *Stats) []model.CatalogLabelRelation {
	now := nowUTC()
	out := make([]model.CatalogLabelRelation, 0, len(edges))
	seen := make(map[[3]int64]struct{}, len(edges))
	for _, e := range edges {
		relation, ok := vndbRelation[e.Relation]
		if !ok {
			st.SkippedUnknownRelation++
			continue
		}
		labelID, okA := anchors[e.ID]
		otherID, okB := anchors[e.PID]
		if !okA || !okB {
			st.SkippedUnanchored++
			continue
		}
		st.BothAnchored++
		if labelID == otherID {
			st.SkippedSelf++
			continue
		}
		key := [3]int64{labelID, otherID, int64(relation)}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model.CatalogLabelRelation{
			LabelID: labelID, OtherLabelID: otherID, Relation: relation,
			SourceID: sourceID, MatchedBy: matchedBy, CreatedAt: now,
		})
	}
	st.Written = len(out)
	return out
}

func rebuild(ctx context.Context, db *gorm.DB, sourceID int16, rows []model.CatalogLabelRelation, st *Stats) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`DELETE FROM catalog_label_relation WHERE source_id = ?`, sourceID)
		if res.Error != nil {
			return fmt.Errorf("delete source graph: %w", res.Error)
		}
		st.Deleted = res.RowsAffected
		if len(rows) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(rows, insertBatch).Error; err != nil {
			return fmt.Errorf("insert graph: %w", err)
		}
		return nil
	})
}

// The rebuild deletes and re-inserts this source's whole graph, so an edge a
// company suppressed would come straight back without this filter.
func dropSuppressed(rows []model.CatalogLabelRelation, suppressed editspec.SuppressedLabelRelations, st *Stats) []model.CatalogLabelRelation {
	kept := rows[:0]
	for _, r := range rows {
		if suppressed.Has(r.LabelID, r.OtherLabelID, r.Relation) {
			st.SkippedSuppressed++
			continue
		}
		kept = append(kept, r)
	}
	st.Written = len(kept)
	return kept
}

var nowUTC = func() time.Time { return time.Now().UTC() }
