package chartraits

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const linkBatchSize = 1000

type Opts struct {
	Apply bool
	DSN   string
}

type Stats struct {
	VocabTotal     int
	VocabWritten   int
	VocabUnchanged int

	EdgesTotal   int
	EdgesAdded   int
	EdgesDeleted int

	LinksSeen      int
	LinksWritten   int
	LinksUnchanged int

	SexualFamilyUpdated int

	Errors int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	st := &Stats{}
	if err := runVocab(ctx, db, opts, st); err != nil {
		return nil, err
	}
	idByTID, err := loadIDMap(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := runEdges(ctx, db, opts, idByTID, st); err != nil {
		return nil, err
	}
	if err := runSexualFamily(ctx, db, opts, st); err != nil {
		return nil, err
	}
	if err := runLinks(ctx, db, opts, st); err != nil {
		return nil, err
	}
	slog.Info("chartraits done", "apply", opts.Apply,
		"vocab_total", st.VocabTotal, "vocab_written", st.VocabWritten, "vocab_unchanged", st.VocabUnchanged,
		"edges_total", st.EdgesTotal, "edges_added", st.EdgesAdded, "edges_deleted", st.EdgesDeleted,
		"links_seen", st.LinksSeen, "links_written", st.LinksWritten, "links_unchanged", st.LinksUnchanged,
		"sexual_family_updated", st.SexualFamilyUpdated,
		"errors", st.Errors)
	return st, nil
}

func runVocab(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	var rows []struct {
		ID           string `gorm:"column:id"`
		GID          string `gorm:"column:gid"`
		GOrder       int16  `gorm:"column:gorder"`
		DefaultSpoil int16  `gorm:"column:defaultspoil"`
		Sexual       bool   `gorm:"column:sexual"`
		Searchable   bool   `gorm:"column:searchable"`
		Applicable   bool   `gorm:"column:applicable"`
		Name         string `gorm:"column:name"`
		Alias        string `gorm:"column:alias"`
		Description  string `gorm:"column:description"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, gid, gorder, defaultspoil, sexual, searchable, applicable, name, alias, description
		FROM src_vndb.traits ORDER BY id`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load src traits: %w", err)
	}
	st.VocabTotal = len(rows)

	if !opts.Apply {
		var unchanged int64
		if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM src_vndb.traits s
			JOIN catalog_character_trait t ON t.vndb_tid = s.id
			WHERE (` + vocabTuple("t.") + `) IS NOT DISTINCT FROM (` + vocabEffective("t.", "s.", srcColumn) + `)`).
			Scan(&unchanged).Error; err != nil {
			return fmt.Errorf("dry vocab plan: %w", err)
		}
		st.VocabUnchanged = int(unchanged)
		st.VocabWritten = st.VocabTotal - int(unchanged)
		return nil
	}

	for _, r := range rows {
		res := db.WithContext(ctx).Exec(`
			INSERT INTO catalog_character_trait
				(vndb_tid, name, group_tid, gorder, default_spoil, sexual, searchable, applicable, alias, description)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (vndb_tid) DO UPDATE SET `+vocabAssignments()+`, updated_at = now()
			WHERE (`+vocabTuple("catalog_character_trait.")+`)
			    IS DISTINCT FROM (`+vocabEffective("catalog_character_trait.", "EXCLUDED.", sameColumn)+`)`,
			r.ID, r.Name, r.GID, r.GOrder, r.DefaultSpoil, r.Sexual, r.Searchable, r.Applicable, r.Alias, r.Description)
		if res.Error != nil {
			st.Errors++
			slog.Warn("vocab upsert", "tid", r.ID, "err", res.Error)
			continue
		}
		if res.RowsAffected == 1 {
			st.VocabWritten++
		} else {
			st.VocabUnchanged++
		}
	}
	return nil
}

var vocabColumns = []string{"name", "group_tid", "gorder", "default_spoil", "sexual", "searchable", "applicable", "alias", "description"}

var vocabSrcColumns = map[string]string{"group_tid": "gid", "default_spoil": "defaultspoil"}

func srcColumn(col string) string {
	if c, ok := vocabSrcColumns[col]; ok {
		return c
	}
	return col
}

func sameColumn(col string) string { return col }

func vocabTuple(prefix string) string {
	cols := make([]string, 0, len(vocabColumns))
	for _, c := range vocabColumns {
		cols = append(cols, prefix+c)
	}
	return strings.Join(cols, ", ")
}

// effectiveColumn is what an import would leave in one column: a column a
// person edited (catalog.trait.*) keeps its value, every other takes VNDB's.
func effectiveColumn(current, incoming, col string, incomingColumn func(string) string) string {
	return fmt.Sprintf("CASE WHEN COALESCE(%s, false) THEN %s%s ELSE %s%s END",
		editspec.HumanFieldProvenanceSQL(current+"field_provenance", col), current, col, incoming, incomingColumn(col))
}

func vocabEffective(current, incoming string, incomingColumn func(string) string) string {
	cols := make([]string, 0, len(vocabColumns))
	for _, c := range vocabColumns {
		cols = append(cols, effectiveColumn(current, incoming, c, incomingColumn))
	}
	return strings.Join(cols, ", ")
}

func vocabAssignments() string {
	out := make([]string, 0, len(vocabColumns))
	for _, c := range vocabColumns {
		out = append(out, c+" = "+effectiveColumn("catalog_character_trait.", "EXCLUDED.", c, sameColumn))
	}
	return strings.Join(out, ", ")
}

func loadIDMap(ctx context.Context, db *gorm.DB) (map[string]int64, error) {
	var rows []struct {
		ID      int64  `gorm:"column:id"`
		VndbTID string `gorm:"column:vndb_tid"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, vndb_tid FROM catalog_character_trait`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load trait id map: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.VndbTID] = r.ID
	}
	return out, nil
}

func runEdges(ctx context.Context, db *gorm.DB, opts Opts, idByTID map[string]int64, st *Stats) error {
	var rows []struct {
		ID     string `gorm:"column:id"`
		Parent string `gorm:"column:parent"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, parent FROM src_vndb.traits_parents`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load src trait parents: %w", err)
	}

	type edge struct{ trait, parent int64 }
	want := make(map[edge]struct{}, len(rows))
	for _, r := range rows {
		t, okT := idByTID[r.ID]
		p, okP := idByTID[r.Parent]
		if !okT || !okP {
			continue
		}
		want[edge{t, p}] = struct{}{}
	}
	var suppressed []struct {
		TraitID  int64 `gorm:"column:trait_id"`
		ParentID int64 `gorm:"column:parent_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT entity_id AS trait_id, split_part(identity_key, ':', 2)::bigint AS parent_id
		FROM edit_suppressed_row WHERE entity_type = ? AND field_key = ?`,
		editspec.TypeTrait, editspec.FieldTraitParents).Scan(&suppressed).Error; err != nil {
		return fmt.Errorf("load suppressed trait parents: %w", err)
	}
	for _, s := range suppressed {
		delete(want, edge{s.TraitID, s.ParentID})
	}
	st.EdgesTotal = len(want)

	var existing []struct {
		TraitID  int64 `gorm:"column:trait_id"`
		ParentID int64 `gorm:"column:parent_id"`
		Origin   int16 `gorm:"column:origin"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT trait_id, parent_id, origin FROM catalog_character_trait_parent`).Scan(&existing).Error; err != nil {
		return fmt.Errorf("load existing edges: %w", err)
	}
	have := make(map[edge]int16, len(existing))
	for _, e := range existing {
		have[edge{e.TraitID, e.ParentID}] = e.Origin
	}

	var toAdd []edge
	for e := range want {
		if _, ok := have[e]; !ok {
			toAdd = append(toAdd, e)
		}
	}
	var toDel []edge
	for e, origin := range have {
		if _, ok := want[e]; !ok && origin == model.TraitParentOriginVNDB {
			toDel = append(toDel, e)
		}
	}
	st.EdgesAdded = len(toAdd)
	st.EdgesDeleted = len(toDel)
	if !opts.Apply {
		return nil
	}
	for _, e := range toAdd {
		if err := db.WithContext(ctx).Exec(`INSERT INTO catalog_character_trait_parent (trait_id, parent_id)
			VALUES (?, ?) ON CONFLICT DO NOTHING`, e.trait, e.parent).Error; err != nil {
			st.Errors++
			slog.Warn("edge insert", "trait", e.trait, "parent", e.parent, "err", err)
		}
	}
	for _, e := range toDel {
		if err := db.WithContext(ctx).Exec(`DELETE FROM catalog_character_trait_parent
			WHERE trait_id = ? AND parent_id = ? AND origin = ?`, e.trait, e.parent, model.TraitParentOriginVNDB).Error; err != nil {
			st.Errors++
			slog.Warn("edge delete", "trait", e.trait, "parent", e.parent, "err", err)
		}
	}
	return nil
}

func runSexualFamily(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	if !opts.Apply {
		var n int64
		if err := db.WithContext(ctx).Raw(model.TraitSexualFamilyPendingSQL).Scan(&n).Error; err != nil {
			return fmt.Errorf("plan sexual_family: %w", err)
		}
		st.SexualFamilyUpdated = int(n)
		return nil
	}
	res := db.WithContext(ctx).Exec(model.TraitSexualFamilySQL)
	if res.Error != nil {
		return fmt.Errorf("update sexual_family: %w", res.Error)
	}
	st.SexualFamilyUpdated = int(res.RowsAffected)
	return nil
}

func runLinks(ctx context.Context, db *gorm.DB, opts Opts, st *Stats) error {
	rows, err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (r.entity_id, ct.tid) r.entity_id AS character_id,
		       COALESCE(t.id, 0) AS trait_id, ct.spoil AS spoiler_level, ct.lie AS lie
		FROM src_vndb.chars_traits ct
		JOIN src_vndb.traits s ON s.id = ct.tid
		JOIN catalog_external_ref r ON r.entity_type = 4 AND r.source_id = 2
			AND r.link_kind = 0 AND r.external_id = ct.id
		LEFT JOIN catalog_character_trait t ON t.vndb_tid = ct.tid
		WHERE NOT ` + editspec.CharacterTraitSuppressedSQL("r.entity_id", "t.id") + `
		ORDER BY r.entity_id, ct.tid, r.external_id`).Rows()
	if err != nil {
		return fmt.Errorf("stream links: %w", err)
	}
	defer rows.Close()

	type link struct {
		characterID  int64
		traitID      int64
		spoilerLevel int16
		lie          bool
	}
	batch := make([]link, 0, linkBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		st.LinksSeen += len(batch)
		if !opts.Apply {
			batch = batch[:0]
			return nil
		}
		kept := batch[:0]
		for _, l := range batch {
			if l.traitID == 0 {
				st.Errors++
				continue
			}
			kept = append(kept, l)
		}
		batch = kept
		if len(batch) == 0 {
			return nil
		}
		var sb strings.Builder
		args := make([]any, 0, len(batch)*4)
		sb.WriteString(`INSERT INTO catalog_character_trait_link (character_id, trait_id, spoiler_level, lie) VALUES `)
		for i, l := range batch {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?, ?, ?, ?)")
			args = append(args, l.characterID, l.traitID, l.spoilerLevel, l.lie)
		}
		sb.WriteString(` ON CONFLICT (character_id, trait_id) DO UPDATE SET
			spoiler_level = EXCLUDED.spoiler_level, lie = EXCLUDED.lie, updated_at = now()
			WHERE ` + editspec.NotCuratedLaneSQL("catalog_character_trait_link.source_id") + `
			  AND (catalog_character_trait_link.spoiler_level, catalog_character_trait_link.lie)
			    IS DISTINCT FROM (EXCLUDED.spoiler_level, EXCLUDED.lie)`)
		res := db.WithContext(ctx).Exec(sb.String(), args...)
		if res.Error != nil {
			st.Errors++
			slog.Warn("link batch upsert", "size", len(batch), "err", res.Error)
			batch = batch[:0]
			return nil
		}
		st.LinksWritten += int(res.RowsAffected)
		st.LinksUnchanged += len(batch) - int(res.RowsAffected)
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var l link
		if err := rows.Scan(&l.characterID, &l.traitID, &l.spoilerLevel, &l.lie); err != nil {
			return fmt.Errorf("scan link row: %w", err)
		}
		batch = append(batch, l)
		if len(batch) >= linkBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("link stream: %w", err)
	}
	return flush()
}
