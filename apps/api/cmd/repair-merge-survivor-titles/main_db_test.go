package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
			os.Exit(1)
		}
		for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run} {
			if err := step(db); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: catalog test setup: %v\n", err)
				os.Exit(1)
			}
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func setup(t *testing.T) int16 {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{
		"catalog_merge_proposal", "catalog_redirect", "catalog_external_ref", "edit_suppressed_row",
		"catalog_revision", "catalog_release", "catalog_work_title", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
	var medium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	require.NotZero(t, medium)
	return medium
}

type title struct {
	lang, text string
	kind       int16
}

func mkWork(t *testing.T, medium int16, olang, display string, titles ...title) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: medium, OLang: olang, DisplayName: display,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, testDB.Create(w).Error)
	for _, ti := range titles {
		require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
			WorkID: w.ID, Lang: ti.lang, Title: ti.text, Kind: ti.kind,
		}).Error)
	}
	return w.ID
}

func snapshotJSON(t *testing.T, id int64) []byte {
	t.Helper()
	var w model.CatalogWork
	require.NoError(t, testDB.Unscoped().First(&w, id).Error)
	var titles []model.CatalogWorkTitle
	require.NoError(t, testDB.Where("work_id = ?", id).Order("id").Find(&titles).Error)
	raw, err := json.Marshal(struct {
		Work   model.CatalogWork        `json:"work"`
		Titles []model.CatalogWorkTitle `json:"titles"`
	}{w, titles})
	require.NoError(t, err)
	return raw
}

func revision(t *testing.T, id int64, action int16, snapshot []byte) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_revision (entity_type, entity_id, revision, action, snapshot, is_minor, note)
		SELECT ?, ?, COALESCE(MAX(revision), 0) + 1, ?, ?, false, 'test'
		FROM catalog_revision WHERE entity_type = ? AND entity_id = ?`,
		model.EntityTypeWork, id, action, string(snapshot), model.EntityTypeWork, id).Error)
}

// legacyMerge replays what ExecuteMerge did to titles before merges went
// survivor-first, so these tests keep describing the data being repaired
// whatever the merge itself does today.
func legacyMerge(t *testing.T, loser, survivor int64, withSnapshot bool) {
	t.Helper()
	source := snapshotJSON(t, loser)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work_title t SET work_id = ? WHERE t.work_id = ?
		AND NOT EXISTS (SELECT 1 FROM catalog_work_title u
		                 WHERE u.work_id = ? AND u.lang = t.lang AND u.title = t.title AND u.kind = t.kind)`,
		survivor, loser, survivor).Error)
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_title WHERE work_id = ?`, loser).Error)
	require.NoError(t, repository.FlattenRedirectsTo(testDB, model.EntityTypeWork, loser, survivor))
	require.NoError(t, repository.InsertRedirect(testDB, model.EntityTypeWork, loser, survivor, nil, "test"))
	if withSnapshot {
		require.NoError(t, testDB.Create(&model.CatalogMergeProposal{
			EntityType: model.EntityTypeWork, SourceEntityID: loser, TargetEntityID: survivor,
			Status: model.ProposalStatusExecuted, FieldResolution: []byte(`{}`), ProposedBy: 1,
		}).Error)
		revision(t, loser, model.RevisionActionMergedSource, source)
		revision(t, survivor, model.RevisionActionMergedTarget, snapshotJSON(t, survivor))
	}
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET status = ?, deleted_at = now() WHERE id = ?`,
		model.WorkStatusMerged, loser).Error)
}

type titleState struct {
	Lang  string
	Title string
	Kind  int16
}

func titlesOf(t *testing.T, workID int64) []titleState {
	t.Helper()
	var out []titleState
	require.NoError(t, testDB.Raw(`SELECT lang, title, kind FROM catalog_work_title WHERE work_id = ? ORDER BY id`,
		workID).Scan(&out).Error)
	return out
}

func shownOf(t *testing.T, workID int64, lang string) string {
	t.Helper()
	ws, err := loadWorks(t.Context(), testDB, []int64{workID}, false)
	require.NoError(t, err)
	require.Len(t, ws, 1)
	shown, _ := shownTitle(ws[0].Titles, nil, lang)
	return shown.Title
}

func repair(t *testing.T, apply bool) (stats, []change, []leftover) {
	t.Helper()
	var receipts, leftovers bytes.Buffer
	st, err := run(t.Context(), testDB, opts{Apply: apply, Batch: 2, Receipts: &receipts, Leftovers: &leftovers})
	require.NoError(t, err)
	return st, decodeLines[change](t, &receipts), decodeLines[leftover](t, &leftovers)
}

func decodeLines[T any](t *testing.T, r io.Reader) []T {
	t.Helper()
	var out []T
	dec := json.NewDecoder(r)
	for dec.More() {
		var v T
		require.NoError(t, dec.Decode(&v))
		out = append(out, v)
	}
	return out
}

func dbFingerprint(t *testing.T) string {
	t.Helper()
	var rows []string
	require.NoError(t, testDB.Raw(`
		SELECT format('t %s %s %s %s %s', id, work_id, lang, title, kind) FROM catalog_work_title
		UNION ALL SELECT format('w %s %s', id, updated_at) FROM catalog_work
		ORDER BY 1`).Scan(&rows).Error)
	return strings.Join(rows, "\n")
}

func TestLineageReadsTheSnapshotsTheMergeWrites(t *testing.T) {
	medium := setup(t)
	loser := mkWork(t, medium, "ja", "敗者", title{"ja", "敗者", official})
	survivor := mkWork(t, medium, "ja", "勝者", title{"ja", "勝者", official})
	var loserTitle, survivorTitle int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work_title WHERE work_id = ?`, loser).Scan(&loserTitle).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work_title WHERE work_id = ?`, survivor).Scan(&survivorTitle).Error)

	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve,
		repository.NewProposalRepository(testDB), repository.NewRevisionRepository(testDB))
	actor := int64(1)
	p, err := merge.ProposeMerge(t.Context(), model.EntityTypeWork, loser, survivor, actor, "test")
	require.NoError(t, err)
	require.NoError(t, merge.ApproveMerge(t.Context(), p.ID, actor))
	require.NoError(t, testDB.Model(&model.CatalogMergeProposal{}).Where("id = ?", p.ID).
		Update("execute_after", gorm.Expr("now() - interval '1 minute'")).Error)
	require.NoError(t, merge.ExecuteMerge(t.Context(), p.ID, &actor))

	lineages, st, err := loadLineages(t.Context(), testDB)
	require.NoError(t, err)
	assert.Equal(t, lineageStats{Losers: 1, LosersWithSnapshot: 1}, st)
	require.Contains(t, lineages, survivor)
	lin := lineages[survivor]
	assert.False(t, lin.snapshotless)
	assert.Contains(t, lin.loserIDs, loserTitle)
	assert.Contains(t, lin.loserPairs, pairKey{"ja", "敗者"})
	assert.Contains(t, lin.ownIDs, survivorTitle)
	assert.NotContains(t, lin.ownIDs, loserTitle, "a moved row in the target snapshot is still the loser's")
}

func TestRepairMergeSurvivorTitles(t *testing.T) {
	medium := setup(t)

	loser1 := mkWork(t, medium, "ja", "咎狗の血 True Blood NITRO ARCHIVE",
		title{"ja", "咎狗の血 True Blood NITRO ARCHIVE", official}, title{"ja", "咎狗の血 True Blood", official},
		title{"", "咎狗の血 True Blood NITRO ARCHIVE", alias})
	survivor1 := mkWork(t, medium, "ja", "咎狗の血 True Blood", title{"ja", "咎狗の血 True Blood", official})

	loser2 := mkWork(t, medium, "ja", "敗者二", title{"zh-Hans", "败者的中文名", official})
	survivor2 := mkWork(t, medium, "ja", "勝者二",
		title{"ja", "勝者二", official}, title{"zh-Hans", "自己的中文名", alias})

	chainA := mkWork(t, medium, "ja", "鎖A", title{"ja", "鎖A", official})
	chainB := mkWork(t, medium, "ja", "鎖B", title{"ja", "鎖B", official})
	chainC := mkWork(t, medium, "ja", "鎖C", title{"ja", "鎖C", official})

	oldLoser := mkWork(t, medium, "ja", "旧敗者", title{"ja", "旧敗者", official})
	oldSurvivor := mkWork(t, medium, "ja", "旧勝者", title{"ja", "旧勝者", official})

	steinsLoser := mkWork(t, medium, "ja", "STEINS;GATE Nitro The Best! Vol.5",
		title{"ja", "STEINS;GATE Nitro The Best! Vol.5", official})
	steins := mkWork(t, medium, "ja", "STEINS;GATE",
		title{"ja", "STEINS;GATE", alias}, title{"en", "STEINS;GATE", official})

	borrowedLoser := mkWork(t, medium, "ja", "借り物 限定版",
		title{"ja", "借り物 限定版", official}, title{"ja", "借り物", alias})
	borrowed := mkWork(t, medium, "ja", "Borrowed", title{"en", "Borrowed", official})

	stray := mkWork(t, medium, "ja", "迷子", title{"ja", "迷子", official})
	orphan := mkWork(t, medium, "ja", "孤児", title{"en", "Orphan", official})

	twinLoser := mkWork(t, medium, "ja", "双子", title{"ja", "双子", official})
	twinSurvivor := mkWork(t, medium, "ja", "双子",
		title{"ja", "双子 完全版", official}, title{"ja", "双子", official})

	mixedOld := mkWork(t, medium, "ja", "混在旧", title{"en", "Shared", official})
	mixedNew := mkWork(t, medium, "ja", "混在新", title{"en", "Shared", official})
	mixed := mkWork(t, medium, "ja", "混在", title{"ja", "混在", official}, title{"en", "Mixed", official})

	bystander := mkWork(t, medium, "ja", "傍観者",
		title{"ja", "傍観者 別題", official}, title{"ja", "傍観者", official})

	legacyMerge(t, loser1, survivor1, true)
	legacyMerge(t, loser2, survivor2, true)
	legacyMerge(t, chainA, chainB, true)
	legacyMerge(t, chainB, chainC, true)
	legacyMerge(t, oldLoser, oldSurvivor, false)
	legacyMerge(t, stray, orphan, true)
	legacyMerge(t, steinsLoser, steins, true)
	legacyMerge(t, borrowedLoser, borrowed, true)
	legacyMerge(t, twinLoser, twinSurvivor, true)
	legacyMerge(t, mixedOld, mixed, false)
	legacyMerge(t, mixedNew, mixed, true)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = '2000-01-01'`).Error)

	require.Equal(t, "咎狗の血 True Blood NITRO ARCHIVE", shownOf(t, survivor1, "ja"), "precondition: the bug")
	require.Equal(t, "败者的中文名", shownOf(t, survivor2, "zh-Hans"), "precondition: the 9453 shape")
	require.Equal(t, "STEINS;GATE Nitro The Best! Vol.5", shownOf(t, steins, "ja"), "precondition: the work 60 shape")
	require.Equal(t, "鎖A", shownOf(t, chainC, "ja"), "precondition: the first link of the chain wins")
	require.Equal(t, "旧敗者", shownOf(t, oldSurvivor, "ja"), "precondition: snapshot-less")

	before := dbFingerprint(t)
	dry, planned, _ := repair(t, false)
	assert.Equal(t, before, dbFingerprint(t), "a dry run writes nothing")

	assert.Equal(t, 9, dry.Survivors)
	assert.Equal(t, 2, dry.SurvivorsSnapshotless)
	assert.Equal(t, 6, dry.Works)
	assert.Equal(t, 9, dry.Rows)
	assert.Equal(t, 6, dry.RowsBySnapshot)
	assert.Equal(t, 1, dry.RowsByDisplayName)
	assert.Equal(t, 2, dry.RowsPromoted)
	assert.Equal(t, 2, dry.WorksPromoted)
	assert.Equal(t, 1, dry.RowsDeleted, "the NITRO ARCHIVE loser also carried that title as a lang-less alias")

	st, applied, leftovers := repair(t, true)
	assert.Equal(t, planned, applied, "apply commits exactly the dry run's plan")
	assert.Equal(t, dry.Rows, st.Rows)

	assert.Equal(t, []titleState{
		{"", "咎狗の血 True Blood NITRO ARCHIVE", alias},
		{"ja", "咎狗の血 True Blood", official},
	}, titlesOf(t, survivor1), "the official became the alias the loser already had; its twin of the survivor's own title the merge itself dropped")
	assert.Equal(t, "咎狗の血 True Blood", shownOf(t, survivor1, "ja"))

	assert.Equal(t, []titleState{
		{"", "败者的中文名", alias},
		{"ja", "勝者二", official},
		{"zh-Hans", "自己的中文名", official},
	}, titlesOf(t, survivor2))
	assert.Equal(t, "自己的中文名", shownOf(t, survivor2, "zh-Hans"))

	assert.Equal(t, []titleState{
		{"", "STEINS;GATE Nitro The Best! Vol.5", alias},
		{"ja", "STEINS;GATE", official},
		{"en", "STEINS;GATE", official},
	}, titlesOf(t, steins), "the survivor's own ja alias takes the official slot the loser held")
	assert.Equal(t, "STEINS;GATE", shownOf(t, steins, "ja"))

	assert.Equal(t, []titleState{
		{"", "鎖A", alias}, {"", "鎖B", alias}, {"ja", "鎖C", official},
	}, titlesOf(t, chainC), "both links of the chain are losers to C")

	assert.Equal(t, []titleState{
		{"", "旧敗者", alias}, {"ja", "旧勝者", official},
	}, titlesOf(t, oldSurvivor))
	assert.Equal(t, []titleState{
		{"", "Shared", alias}, {"ja", "混在", official}, {"en", "Mixed", official},
	}, titlesOf(t, mixed), "a later merge's target snapshot does not vouch for a snapshot-less loser's rows")

	for _, c := range applied {
		switch {
		case c.WorkID == oldSurvivor:
			assert.Equal(t, reasonDisplayName, c.Reason)
		case c.Action == actionPromote:
			assert.Equal(t, reasonOwnAliasPromoted, c.Reason)
			assert.Contains(t, []int64{survivor2, steins}, c.WorkID)
		default:
			assert.Equal(t, reasonSnapshot, c.Reason)
		}
	}

	assert.Equal(t, []titleState{
		{"ja", "双子 完全版", official}, {"ja", "双子", official},
	}, titlesOf(t, twinSurvivor), "negative control: the loser's only title was the survivor's own too")
	assert.Equal(t, []titleState{
		{"ja", "傍観者 別題", official}, {"ja", "傍観者", official},
	}, titlesOf(t, bystander), "negative control: two officials on a work no merge touched")
	assert.Equal(t, []titleState{
		{"ja", "迷子", official}, {"en", "Orphan", official},
	}, titlesOf(t, orphan), "negative control: the survivor has no ja title of its own to keep")
	assert.Equal(t, []titleState{
		{"ja", "借り物 限定版", official}, {"ja", "借り物", alias}, {"en", "Borrowed", official},
	}, titlesOf(t, borrowed), "negative control: the only ja alias came from the loser, so nothing is promoted")

	var bumped []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work WHERE updated_at > '2000-01-01' ORDER BY id`).
		Scan(&bumped).Error)
	assert.ElementsMatch(t, []int64{survivor1, survivor2, chainC, oldSurvivor, mixed, steins}, bumped)

	assert.ElementsMatch(t, []leftover{
		{WorkID: twinSurvivor, OLang: "ja", DisplayName: "双子", Shown: "双子 完全版", ShownKind: official, Losers: 1},
		{WorkID: orphan, OLang: "ja", DisplayName: "孤児", Shown: "迷子", ShownKind: official, Losers: 1},
		{WorkID: borrowed, OLang: "ja", DisplayName: "Borrowed", Shown: "借り物 限定版", ShownKind: official, Losers: 1},
	}, leftovers)

	again, _, _ := repair(t, true)
	assert.Zero(t, again.Rows, "a second apply writes nothing")
	assert.Zero(t, again.Works)
}

func TestRepairSkipsSuppressedTitles(t *testing.T) {
	medium := setup(t)
	loser := mkWork(t, medium, "ja", "敗者", title{"ja", "敗者", official})
	survivor := mkWork(t, medium, "ja", "勝者", title{"ja", "勝者", official})
	legacyMerge(t, loser, survivor, true)
	require.NoError(t, testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key)
		VALUES (?, ?, ?, ?)`, editspec.TypeWork, survivor, editspec.FieldWorkTitles,
		editspec.WorkTitleIdentity(official, "ja", "勝者")).Error)

	st, _, _ := repair(t, false)
	assert.Zero(t, st.Rows, "the survivor's own official is hidden, so nothing of its own is left to show")
}

func TestRepairPromotesNoAliasIntoAnOfficialAnEditRemoved(t *testing.T) {
	medium := setup(t)
	loser := mkWork(t, medium, "ja", "敗者", title{"ja", "敗者", official})
	survivor := mkWork(t, medium, "ja", "Win", title{"en", "Win", official}, title{"ja", "勝者", alias})
	legacyMerge(t, loser, survivor, true)
	require.NoError(t, testDB.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key)
		VALUES (?, ?, ?, ?)`, editspec.TypeWork, survivor, editspec.FieldWorkTitles,
		editspec.WorkTitleIdentity(official, "ja", "勝者")).Error)

	st, _, _ := repair(t, false)
	assert.Zero(t, st.Rows, "the promoted row would be hidden the moment it became an official")
}

func TestRepairLeavesAnUnmergedWorkAlone(t *testing.T) {
	medium := setup(t)
	loser := mkWork(t, medium, "ja", "分離", title{"ja", "分離", official}, title{"en", "Bunri", official})
	survivor := mkWork(t, medium, "ja", "残留", title{"ja", "残留", official})
	legacyMerge(t, loser, survivor, true)
	var proposal int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_merge_proposal WHERE source_entity_id = ?`, loser).
		Scan(&proposal).Error)

	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve,
		repository.NewProposalRepository(testDB), repository.NewRevisionRepository(testDB))
	rebuilt, err := merge.Unmerge(t.Context(), proposal, nil)
	require.NoError(t, err)
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: rebuilt, Lang: "en", Title: "Bunri Remaster", Kind: official,
	}).Error)

	_, applied, _ := repair(t, true)
	assert.Equal(t, []titleState{
		{"ja", "分離", official}, {"en", "Bunri", official}, {"en", "Bunri Remaster", official},
	}, titlesOf(t, rebuilt), "the rebuild is the loser itself, not a work it was merged into")
	assert.Equal(t, []titleState{
		{"", "分離", alias}, {"en", "Bunri", official}, {"ja", "残留", official},
	}, titlesOf(t, survivor), "Unmerge leaves the moved rows on the survivor")
	require.Len(t, applied, 1)
}

func TestRepairPromotesAnAliasNamedLikeTheLoserOfficial(t *testing.T) {
	medium := setup(t)
	loser := mkWork(t, medium, "ja", "同名", title{"ja", "同名", official})
	survivor := mkWork(t, medium, "ja", "Same", title{"en", "Same", official}, title{"ja", "同名", alias})
	legacyMerge(t, loser, survivor, true)

	st, _, _ := repair(t, true)
	assert.Equal(t, 1, st.RowsPromoted)
	assert.Equal(t, []titleState{
		{"", "同名", alias}, {"en", "Same", official}, {"ja", "同名", official},
	}, titlesOf(t, survivor), "the demotion frees (ja, 同名, official) before the promotion takes it")
}
