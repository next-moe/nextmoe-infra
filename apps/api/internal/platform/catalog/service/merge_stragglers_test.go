package service

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
)

func TestMergeRefsCoverTheSchema(t *testing.T) {
	var cols []string
	require.NoError(t, testDB.Raw(`
		SELECT c.table_name || '.' || c.column_name
		  FROM information_schema.columns c
		 WHERE c.table_schema = 'public'
		   AND (c.table_name LIKE 'catalog\_%' OR c.table_name LIKE 'edit\_%')
		   AND (c.column_name ~ '(^|_)(label|work|character|person|credit_name)_id$'
		        OR (c.column_name IN ('entity_id', 'a_id', 'b_id', 'source_entity_id', 'target_entity_id')
		            AND EXISTS (SELECT 1 FROM information_schema.columns e
		                         WHERE e.table_schema = 'public' AND e.table_name = c.table_name
		                           AND e.column_name = 'entity_type')))
		 ORDER BY 1`).Scan(&cols).Error)
	require.Greater(t, len(cols), 40, "the walk found too few columns to mean anything")

	known := map[string]bool{}
	for _, r := range mergeRefs {
		key := r.table + "." + r.column
		require.Falsef(t, known[key], "%s is listed twice in mergeRefs", key)
		known[key] = true
		_, excluded := mergeRefExclusions[key]
		require.Falsef(t, excluded, "%s is both moved and excluded", key)
	}
	inSchema := map[string]bool{}
	for _, c := range cols {
		inSchema[c] = true
		_, excluded := mergeRefExclusions[c]
		require.Truef(t, known[c] || excluded,
			"%s names a merge-able id but is neither in mergeRefs nor in mergeRefExclusions", c)
	}
	for key := range mergeRefExclusions {
		require.Truef(t, inSchema[key], "exclusion %s names no column in the schema", key)
	}
	for _, r := range mergeRefs {
		var n int64
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, r.table, r.column).Scan(&n).Error)
		require.Equalf(t, int64(1), n, "mergeRefs names %s.%s, which is not in the schema", r.table, r.column)
	}
}

func TestRehangNamesEveryRef(t *testing.T) {
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, testDB))
	for _, r := range mergeRefs {
		if r.step != stepRehang {
			continue
		}
		require.NotEmptyf(t, r.families, "%s.%s has no family", r.table, r.column)
		for _, fam := range r.families {
			stmts, err := rehangStmts(reg, fam, 101, 202)
			require.NoError(t, err)
			named := false
			for _, s := range stmts {
				if strings.Contains(s.sql, r.table+" ") && strings.Contains(s.sql, r.column) {
					named = true
					break
				}
			}
			require.Truef(t, named, "no rehang statement for family %d moves %s.%s", fam, r.table, r.column)
		}
	}
}

func mergeNow(t *testing.T, entityType int16, src, dst int64) {
	t.Helper()
	p, err := testMerge.ProposeMerge(t.Context(), entityType, src, dst, 7, "test merge")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
}

func TestMergeLabelRehangsReleaseLabels(t *testing.T) {
	cleanTables(t)
	target := createLabel(t, "Brand", model.LabelKindGameBrand)
	source := createLabel(t, "Brand Inc.", model.LabelKindGameBrand)
	w := createWork(t, "work")
	r1 := createRelease(t, w.ID, 2024, 1, 1)
	r2 := createRelease(t, w.ID, 2024, 2, 1)
	for _, e := range []model.CatalogReleaseLabel{
		{ReleaseID: r1.ID, LabelID: source, Kind: 0},
		{ReleaseID: r1.ID, LabelID: target, Kind: 0},
		{ReleaseID: r2.ID, LabelID: source, Kind: 1},
	} {
		require.NoError(t, testDB.Create(&e).Error)
	}
	before := workUpdatedAt(t, w.ID)

	mergeNow(t, model.EntityTypeLabel, source, target)

	var got []model.CatalogReleaseLabel
	require.NoError(t, testDB.Order("release_id, kind").Find(&got).Error)
	require.Len(t, got, 2)
	for _, g := range got {
		require.Equal(t, target, g.LabelID)
	}
	require.True(t, workUpdatedAt(t, w.ID).After(before), "a release's work renders its companies, so the merge touches it")
}

func TestMergeWorkRehangsCoverVotes(t *testing.T) {
	cleanTables(t)
	src, dst := createWork(t, "src"), createWork(t, "dst")
	cover := func(workID int64, hash string) int64 {
		c := &model.CatalogWorkCover{WorkID: workID, ImageHash: hash, SourceID: model.SourceBangumi}
		require.NoError(t, testDB.Create(c).Error)
		return c.ID
	}
	srcCover, dstCover := cover(src.ID, "aa"), cover(dst.ID, "bb")
	vote := func(workID, coverID, uid int64) {
		require.NoError(t, testDB.Create(&model.CatalogCoverVote{WorkID: workID, CoverID: coverID, ActorUID: uid, Site: "kungal"}).Error)
	}
	vote(src.ID, srcCover, 1)
	vote(src.ID, srcCover, 2)
	vote(dst.ID, dstCover, 2)

	mergeNow(t, model.EntityTypeWork, src.ID, dst.ID)

	var votes []model.CatalogCoverVote
	require.NoError(t, testDB.Order("actor_uid").Find(&votes).Error)
	require.Len(t, votes, 2, "one ballot per user per work")
	require.Equal(t, dst.ID, votes[0].WorkID)
	require.Equal(t, srcCover, votes[0].CoverID)
	require.Equal(t, dst.ID, votes[1].WorkID)
	require.Equal(t, dstCover, votes[1].CoverID, "the survivor's own ballot wins")
}

func TestSweepStragglersReplaysTheMerge(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	var vndb int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)
	require.NotZero(t, vndb)

	target := createLabel(t, "MAGES.", model.LabelKindGameBrand)
	retired := createLabel(t, "株式会社MAGES.", model.LabelKindGameBrand)
	ref := func(entityType int16, entityID int64, source int16, ext string, kind int16) {
		require.NoError(t, testDB.Create(&model.CatalogExternalRef{
			EntityType: entityType, EntityID: entityID, SourceID: source, ExternalID: ext,
			LinkKind: kind, MatchedBy: "test",
		}).Error)
	}
	ref(model.EntityTypeLabel, target, vndb, "p146", model.LinkKindExact)
	ref(model.EntityTypeLabel, target, model.SourceCurated, "c1", model.LinkKindExact)
	mergeNow(t, model.EntityTypeLabel, retired, target)

	charSrc, charDst := createCharacter(t, "src"), createCharacter(t, "dst")
	mergeNow(t, model.EntityTypeCharacter, charSrc.ID, charDst.ID)
	workSrc, workDst := createWork(t, "old"), createWork(t, "new")
	mergeNow(t, model.EntityTypeWork, workSrc.ID, workDst.ID)

	// Everything below is written after the merges, as the 2026-08 importers did.
	w := createWork(t, "released")
	rel := createRelease(t, w.ID, 2024, 3, 1)
	vndbSrc := vndb
	require.NoError(t, testDB.Create(&model.CatalogReleaseLabel{ReleaseID: rel.ID, LabelID: retired, Kind: 0, SourceID: &vndbSrc}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{WorkID: w.ID, LabelID: retired, Kind: model.WorkLabelKindBrand, SourceID: &vndbSrc}).Error)
	ref(model.EntityTypeLabel, retired, vndb, "p6307", model.LinkKindExact)
	ref(model.EntityTypeLabel, retired, model.SourceCurated, "c2", model.LinkKindExact)
	ref(model.EntityTypeLabel, retired, model.SourceErogameScape, "b9", model.LinkKindExact)
	require.NoError(t, testDB.Create(&model.CatalogUserEntityFollow{
		ActorUID: 5, EntityType: model.EntityTypeLabel, EntityID: retired, ClientID: "kungal",
	}).Error)
	intro := func(characterID int64, lang, text string) {
		require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
			CharacterID: characterID, Lang: lang, Intro: text, SourceID: model.SourceBangumi, Provenance: 0,
		}).Error)
	}
	intro(charSrc.ID, "ja", "stale ja")
	intro(charSrc.ID, "zh-Hans", "moves")
	intro(charDst.ID, "ja", "kept ja")
	require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
		WorkID: workSrc.ID, Lang: "zh-Hans", Intro: "late translation", SourceID: model.SourceBangumi, Provenance: 1,
	}).Error)

	orphanDst := createLabel(t, "gone", model.LabelKindGameBrand)
	orphanSrc := createLabel(t, "gone too", model.LabelKindGameBrand)
	require.NoError(t, testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id IN ?`, []int64{orphanSrc, orphanDst}).Error)
	require.NoError(t, testDB.Create(&model.CatalogRedirect{EntityType: model.EntityTypeLabel, OldID: orphanSrc, CurrentID: orphanDst}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{WorkID: w.ID, LabelID: orphanSrc, Kind: model.WorkLabelKindBrand}).Error)

	beforeW, beforeDst := workUpdatedAt(t, w.ID), workUpdatedAt(t, workDst.ID)
	rep, err := testMerge.SweepStragglers(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, rep.Pairs)
	require.Equal(t, 3, rep.Repaired)
	require.Equal(t, 1, rep.Orphaned)
	require.Empty(t, rep.Uncovered)
	require.Equal(t, int64(1), rep.Found["catalog_release_label.label_id"])
	require.Equal(t, int64(2), rep.Found["catalog_work_label.label_id"])
	require.Equal(t, int64(3), rep.Found["catalog_external_ref.entity_id"])
	require.Equal(t, int64(2), rep.Found["catalog_character_intro.character_id"])
	require.Equal(t, int64(1), rep.Found["catalog_work_intro.work_id"])
	require.Equal(t, int64(1), rep.Found["catalog_user_entity_follow.entity_id"])

	kinds := map[string]int16{}
	var refs []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ?", model.EntityTypeLabel, target).Find(&refs).Error)
	for _, r := range refs {
		kinds[r.ExternalID] = r.LinkKind
	}
	require.Equal(t, map[string]int16{
		"p146":  model.LinkKindExact,
		"p6307": model.LinkKindRelated,
		"c1":    model.LinkKindExact,
		"c2":    model.LinkKindExact,
		"b9":    model.LinkKindExact,
	}, kinds, "a straggler never demotes the survivor's own anchor")

	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_release_label WHERE label_id = ?`, target).Scan(&n).Error)
	require.Equal(t, int64(1), n)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_entity_follow WHERE entity_id = ?`, target).Scan(&n).Error)
	require.Equal(t, int64(1), n)
	var intros []model.CatalogCharacterIntro
	require.NoError(t, testDB.Where("character_id = ?", charDst.ID).Order("lang").Find(&intros).Error)
	require.Len(t, intros, 2)
	require.Equal(t, "kept ja", intros[0].Intro)
	require.Equal(t, "moves", intros[1].Intro)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_intro WHERE work_id = ?`, workDst.ID).Scan(&n).Error)
	require.Equal(t, int64(1), n)

	require.True(t, workUpdatedAt(t, w.ID).After(beforeW), "the work whose companies moved is touched")
	require.True(t, workUpdatedAt(t, workDst.ID).After(beforeDst), "the survivor of a work pair is touched")
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_label WHERE label_id = ?`, orphanSrc).Scan(&n).Error)
	require.Equal(t, int64(1), n, "a redirect to a dead target is reported, not guessed at")

	again, err := testMerge.SweepStragglers(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, again.Pairs, "only the orphan is left")
	require.Equal(t, 0, again.Repaired)
}
