package service

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func absorbNow(t *testing.T, src, dst int64) int64 {
	t.Helper()
	p, err := testMerge.ProposeAbsorb(t.Context(), src, dst, 7, "edition of its base work")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
	return p.ID
}

type absorbFixture struct {
	survivor, loser, other int64
	release, folder        int64
	vote                   int64
}

func seedAbsorbLoser(t *testing.T, f *absorbFixture) {
	t.Helper()
	l := f.loser
	seedLoser(t, l, false)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{WorkID: l, Name: "百合", SourceID: model.SourceBangumi, Count: 3}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{WorkID: l, Name: "人工", SourceID: model.SourceCurated, Count: 1}).Error)
	addWorkRating(t, l, model.SourceBangumi, 7.5, 100)
	require.NoError(t, testDB.Create(&model.CatalogWorkPlatform{WorkID: l, Platform: "psv", SourceID: model.SourceBangumi}).Error)
	series := &model.CatalogSeries{DisplayName: "series", SourceID: model.SourceBangumi, ExternalID: "s1"}
	require.NoError(t, testDB.Create(series).Error)
	require.NoError(t, testDB.Create(&model.CatalogSeriesMember{SeriesID: series.ID, WorkID: l}).Error)
	ch := createCharacter(t, "ヒロイン")
	createWorkCharacter(t, l, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	person := createPerson(t, "原画")
	cn := createCreditName(t, &person.ID, "原画家")
	bgm := model.SourceBangumi
	require.NoError(t, testDB.Create(&model.CatalogCredit{
		WorkID: l, CreditNameID: cn.ID, RoleID: seededRoleID(t), SourceID: &bgm}).Error)
	createWorkRelation(t, l, f.other)
	rel := createRelease(t, l, 2012, 3, 30)
	f.release = rel.ID
	addExternalRef(t, model.EntityTypeRelease, rel.ID, srcVNDB, "r777", model.LinkKindExact)

	require.NoError(t, testDB.Create(&model.CatalogUserWorkRating{
		ActorUID: 21, WorkID: l, Overall: 8, Recommend: model.RecommendYes, ClientID: "c"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogUserWorkState{ActorUID: 21, WorkID: l, State: 2}).Error)
	require.NoError(t, testDB.Create(&model.CatalogUserPlaytime{ActorUID: 21, WorkID: l, ClientID: "c", Minutes: 90}).Error)
	folder := &model.CatalogUserFolder{OwnerUID: 21, Name: "fav"}
	require.NoError(t, testDB.Create(folder).Error)
	f.folder = folder.ID
	require.NoError(t, testDB.Create(&model.CatalogUserFolderItem{FolderID: folder.ID, WorkID: l, OwnerUID: 21}).Error)
	var shared model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ? AND image_hash = ?", l, lShared).First(&shared).Error)
	v := &model.CatalogCoverVote{WorkID: l, CoverID: shared.ID, ActorUID: 21, Site: "kungal"}
	require.NoError(t, testDB.Create(v).Error)
	f.vote = v.ID
	require.NoError(t, testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key, created_at)
		VALUES (?, ?, 'catalog.work.tag_ids.suppressed', 'tag:999', now())`, editspec.TypeWork, l).Error)
	claimWork(t, l, "kungal", 5151)
}

func newAbsorbFixture(t *testing.T) absorbFixture {
	t.Helper()
	cleanTables(t)
	var f absorbFixture
	f.loser = createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, loserJaTitle).ID
	f.survivor = createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, survivorDisplay).ID
	f.other = createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "related game").ID
	seedSurvivor(t, f.survivor)
	return f
}

func TestAbsorbKeepsTheBaseWorkAndTakesOnlyUserData(t *testing.T) {
	f := newAbsorbFixture(t)
	svc := newSurvivorFirstSvc()
	seedAbsorbLoser(t, &f)
	before := faceOf(t, svc, f.survivor)

	pid := absorbNow(t, f.loser, f.survivor)

	want := before.clone()
	want.Intros["de"] = "loser curated de|curated|false"
	voted := svc.imageURL(lShared)
	want.Portrait, want.ListCover, want.ListPortrait = "cover:"+voted, voted, "cover:"+voted
	assert.Equal(t, want, faceOf(t, svc, f.survivor), "an edition adds no imported data; its curated intro fills a "+
		"language, and its reader's vote lands on the survivor's identical cover, which the vote now elects")

	var refs []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ? AND matched_by = ?",
		model.EntityTypeWork, f.survivor, absorbMatchedBy).Order("source_id").Find(&refs).Error)
	require.Len(t, refs, 3)
	for _, r := range refs {
		assert.Equal(t, model.LinkKindRelated, r.LinkKind, "%d:%s", r.SourceID, r.ExternalID)
	}

	count := func(table, where string, args ...any) int64 {
		var n int64
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM `+table+` WHERE `+where, args...).Scan(&n).Error)
		return n
	}
	for table, n := range map[string]int64{
		"catalog_work_intro": 4, "catalog_work_cover": 2, "catalog_work_screenshot": 1,
		"catalog_work_tag": 1, "catalog_work_platform": 0,
		"catalog_series_member": 0, "catalog_work_character": 0, "catalog_credit": 0,
	} {
		assert.Equal(t, n, count(table, "work_id = ?", f.survivor), "%s on the base work", table)
		assert.Zero(t, count(table, "work_id = ?", f.loser), "%s left on the edition", table)
	}
	assert.Zero(t, count("catalog_work_rating", "work_id IN (?, ?) AND source_id = ?", f.survivor, f.loser, model.SourceBangumi),
		"the edition's source rating is archived, not moved")
	assert.Zero(t, count("catalog_work_relation", "a_work_id = ? OR b_work_id = ?", f.survivor, f.survivor),
		"the edition's imported relation is not the base work's")
	assert.EqualValues(t, 1, count("catalog_work_tag", "work_id = ? AND source_id = ?", f.survivor, model.SourceCurated),
		"a tag a person set moves")

	assert.EqualValues(t, 1, count("catalog_release", "id = ? AND work_id = ?", f.release, f.survivor), "releases move")
	assert.EqualValues(t, 1, count("catalog_external_ref", "entity_type = ? AND entity_id = ? AND link_kind = ?",
		model.EntityTypeRelease, f.release, model.LinkKindExact), "a release ref moves with its release")
	for _, table := range []string{"catalog_user_work_rating", "catalog_user_work_state", "catalog_user_playtime", "catalog_user_folder_item"} {
		assert.EqualValues(t, 1, count(table, "work_id = ?", f.survivor), "%s moves", table)
	}
	assert.EqualValues(t, 1, count("catalog_cover_vote", "id = ? AND work_id = ? AND cover_id IN (SELECT id FROM catalog_work_cover WHERE work_id = ?)",
		f.vote, f.survivor, f.survivor), "a vote on art the base work also has follows it there")
	assert.EqualValues(t, 1, count("edit_suppressed_row", "entity_type = ? AND entity_id = ?", editspec.TypeWork, f.survivor))
	var site string
	require.NoError(t, testDB.Raw(`SELECT coalesce(site, '') FROM catalog_work WHERE id = ?`, f.survivor).Scan(&site).Error)
	assert.Equal(t, "kungal", site, "the claim moves to the base work")

	var reason string
	require.NoError(t, testDB.Raw(`SELECT reason FROM catalog_redirect WHERE entity_type = ? AND old_id = ?`,
		model.EntityTypeWork, f.loser).Scan(&reason).Error)
	assert.True(t, strings.HasPrefix(reason, absorbRedirectNote), "redirect reason %q", reason)

	archived := map[string]int64{}
	var rows []struct {
		Table string `gorm:"column:table_name"`
		N     int64  `gorm:"column:n"`
	}
	require.NoError(t, testDB.Raw(`SELECT table_name, count(*) AS n FROM catalog_merge_archive
		WHERE proposal_id = ? GROUP BY 1`, pid).Scan(&rows).Error)
	for _, r := range rows {
		archived[r.Table] = r.N
	}
	assert.Equal(t, map[string]int64{
		"catalog_work_title": 5, "catalog_work_intro": 4, "catalog_work_cover": 3,
		"catalog_work_screenshot": 1, "catalog_work_tag": 1, "catalog_work_rating": 1,
		"catalog_work_platform": 1, "catalog_series_member": 1, "catalog_work_character": 1,
		"catalog_credit": 1, "catalog_work_relation": 1, "catalog_external_ref": 3,
		"edit_suppressed_row": 1,
	}, archived)
}

func TestAbsorbRefusesAnEditionHoldingAVNDBAnchor(t *testing.T) {
	f := newAbsorbFixture(t)
	seedLoser(t, f.loser, true)

	_, err := testMerge.ProposeAbsorb(t.Context(), f.loser, f.survivor, 7, "not an edition")
	require.ErrorIs(t, err, ErrAbsorbVNDBAnchor)

	p, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, f.loser, f.survivor, 7, "hand-edited to absorb")
	require.NoError(t, err)
	require.NoError(t, testDB.Model(p).Update("field_resolution", absorbResolution()).Error)
	approveAndForceExecutable(t, p.ID)
	require.ErrorIs(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil), ErrAbsorbVNDBAnchor)

	var live int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work WHERE id = ? AND deleted_at IS NULL`, f.loser).Scan(&live).Error)
	assert.EqualValues(t, 1, live, "a refused absorb changes nothing")
}

func TestStragglerSweepAbsorbsLateRowsOfAnAbsorbedEdition(t *testing.T) {
	f := newAbsorbFixture(t)
	svc := newSurvivorFirstSvc()
	before := faceOf(t, svc, f.survivor)
	absorbNow(t, f.loser, f.survivor)

	seedLoser(t, f.loser, false)
	rep, err := testMerge.SweepStragglers(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, rep.Repaired)

	want := before.clone()
	want.Intros["de"] = "loser curated de|curated|false"
	assert.Equal(t, want, faceOf(t, svc, f.survivor))
	var related int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND matched_by = ? AND link_kind = ?`,
		model.EntityTypeWork, f.survivor, absorbMatchedBy, model.LinkKindRelated).Scan(&related).Error)
	assert.EqualValues(t, 3, related, "late refs of an absorbed edition land as related")
}

func TestAbsorbClassifiesEveryWorkRef(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range mergeRefs {
		applies := r.families == nil
		for _, fam := range r.families {
			applies = applies || fam == model.EntityTypeWork
		}
		if !applies {
			continue
		}
		seen[r.table] = true
		_, archived := absorbArchived[r.table]
		_, kept := absorbKept[r.table]
		assert.Truef(t, archived != kept, "%s.%s must be in exactly one of absorbArchived / absorbKept", r.table, r.column)
	}
	for table := range absorbArchived {
		assert.Truef(t, seen[table], "absorbArchived names %s, which no work-family merge ref holds", table)
	}
	for table := range absorbKept {
		assert.Truef(t, seen[table], "absorbKept names %s, which no work-family merge ref holds", table)
	}
}
