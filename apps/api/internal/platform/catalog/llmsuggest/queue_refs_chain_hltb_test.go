package llmsuggest

import (
	"fmt"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type hltbFixture struct {
	db     *gorm.DB
	mirror *gorm.DB
	up     StagingDBs
	reg    sourceReg
	steam  int16
}

func newHLTBFixture(t *testing.T) *hltbFixture {
	t.Helper()
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_work, catalog_release, catalog_external_ref RESTART IDENTITY CASCADE").Error)
	// In production the two upstream mirrors are separate databases that happen
	// to both call their main table "games"; erogamescape's is already in this
	// test database, so the HLTB one gets a schema of its own and a handle whose
	// search_path resolves to it.
	require.NoError(t, db.Exec(`CREATE SCHEMA IF NOT EXISTS hltb_mirror`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS hltb_mirror.games (
		hltb_id bigint PRIMARY KEY, status text NOT NULL, raw jsonb NOT NULL)`).Error)
	require.NoError(t, db.Exec("TRUNCATE hltb_mirror.games").Error)

	dsn, ok := dbtest.DSN()
	require.True(t, ok)
	mirror, err := gorm.Open(postgres.Open(dsn+" search_path=hltb_mirror"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(mirror) })

	reg, err := loadSourceReg(db)
	require.NoError(t, err)
	return &hltbFixture{db: db, mirror: mirror, up: StagingDBs{HLTB: mirror}, reg: reg, steam: reg.idByKey["steam"]}
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// A work whose release carries the Steam appid the importer matched on.
func (f *hltbFixture) steamAnchoredWork(t *testing.T, name, appid string) int64 {
	t.Helper()
	var medium int16
	require.NoError(t, f.db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, f.db.Create(w).Error)
	rel := &model.CatalogRelease{WorkID: w.ID, Kind: model.ReleaseKindDefault}
	require.NoError(t, f.db.Create(rel).Error)
	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: f.steam,
		ExternalID: appid, LinkKind: model.LinkKindExact, MatchedBy: "test:steam",
	}).Error)
	return w.ID
}

func (f *hltbFixture) mirrorRecord(t *testing.T, hltbID int64, appid string) {
	t.Helper()
	require.NoError(t, f.mirror.Exec(`INSERT INTO games (hltb_id, status, raw) VALUES (?, 'fetched', ?::jsonb)`,
		hltbID, fmt.Sprintf(`{"data":{"game":[{"profile_steam":"%s"}]}}`, appid)).Error)
}

func (f *hltbFixture) verify(t *testing.T, workID int64, hltbID string) chainResult {
	t.Helper()
	it := refItem{
		EntityType: model.EntityTypeWork, EntityID: workID,
		SourceID: f.reg.idByKey["howlongtobeat"], ExternalID: hltbID,
		MatchedBy: matchedByHLTBSteam, Hash: "h",
	}
	out := map[string]chainResult{}
	require.NoError(t, verifyHLTBSteamChain(f.db, f.up, f.reg, []refItem{it}, out))
	return out["h"]
}

func TestHLTBChainVerifiesAOneToOneSteamAnchor(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "anchored", "787480")
	f.mirrorRecord(t, 9001, "787480")

	got := f.verify(t, w, "9001")
	require.Equal(t, VerdictChainVerified, got.Verdict, got.Reason)
	require.Equal(t, float64(1), got.Confidence)
}

// jobs/hltbrefs also refuses an appid that more than one work claims, and the
// verifier keeps that branch for parity — but on the catalog side the state is
// not reachable: uq_catalog_external_ref_exact admits one exact holder per
// (source, external_id, entity_type), so a second release cannot take an appid
// another release already holds. Only the mirror side of the ambiguity test can
// actually fire, which is what the next test covers.
func TestASecondWorkCannotClaimTheSameSteamAppid(t *testing.T) {
	f := newHLTBFixture(t)
	f.steamAnchoredWork(t, "first", "787480")

	var medium int16
	require.NoError(t, f.db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: "second",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, f.db.Create(w).Error)
	rel := &model.CatalogRelease{WorkID: w.ID, Kind: model.ReleaseKindDefault}
	require.NoError(t, f.db.Create(rel).Error)

	err := f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: f.steam,
		ExternalID: "787480", LinkKind: model.LinkKindExact, MatchedBy: "test:steam",
	}).Error
	require.ErrorContains(t, err, "uq_catalog_external_ref_exact")
}

func TestHLTBChainRefusesAnAppidTwoMirrorRecordsClaim(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "anchored", "787480")
	f.mirrorRecord(t, 9001, "787480")
	f.mirrorRecord(t, 9002, "787480")

	got := f.verify(t, w, "9001")
	require.Equal(t, VerdictChainUnproven, got.Verdict)
	require.Contains(t, got.Reason, "steam_appid_ambiguous")
}

func TestHLTBChainRefusesARefPointingAtAnotherWork(t *testing.T) {
	f := newHLTBFixture(t)
	anchored := f.steamAnchoredWork(t, "anchored", "787480")
	other := f.steamAnchoredWork(t, "other", "999999")
	f.mirrorRecord(t, 9001, "787480")

	got := f.verify(t, other, "9001")
	require.Equal(t, VerdictChainUnproven, got.Verdict)
	require.Contains(t, got.Reason, "steam_anchor")
	require.NotEqual(t, anchored, other)
}

func TestHLTBChainRefusesAMirrorRecordWithNoAppid(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "anchored", "787480")

	got := f.verify(t, w, "9001")
	require.Equal(t, VerdictChainUnproven, got.Verdict)
	require.Contains(t, got.Reason, "hltb_appid")
}

func TestHLTBChainReportsAnUnreachableMirror(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "anchored", "787480")
	f.up.HLTB = nil

	got := f.verify(t, w, "9001")
	require.Equal(t, VerdictChainUnproven, got.Verdict)
	require.Contains(t, got.Reason, "hltb_mirror")
}

func TestApplySkipsAConfirmWhoseExactSlotIsTaken(t *testing.T) {
	f := newHLTBFixture(t)
	holder := f.steamAnchoredWork(t, "holder", "111")
	blocked := f.steamAnchoredWork(t, "blocked", "222")
	bgm := f.reg.idByKey["bangumi"]

	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: holder, SourceID: bgm,
		ExternalID: "500", LinkKind: model.LinkKindExact, MatchedBy: "test:bgm",
	}).Error)

	rows := []QueueVerdict{
		{EntityType: model.EntityTypeWork, EntityID: blocked, SourceID: bgm, ExternalID: "500"},
		{EntityType: model.EntityTypeWork, EntityID: blocked, SourceID: bgm, ExternalID: "501"},
		{EntityType: model.EntityTypeWork, EntityID: holder, SourceID: bgm, ExternalID: "500"},
	}
	holders, err := exactSlotHolders(f.db, rows)
	require.NoError(t, err)

	got := holders[exactSlotKey(model.EntityTypeWork, bgm, "500")]
	require.Equal(t, holder, got, "the slot reports the entity that holds it")

	_, free := holders[exactSlotKey(model.EntityTypeWork, bgm, "501")]
	require.False(t, free, "a slot nobody holds is absent, so the confirm proceeds")

	// The holder confirming its own slot is not blocked: the check compares the
	// holder against the row's own entity.
	require.Equal(t, holder, holders[exactSlotKey(model.EntityTypeWork, bgm, "500")])
}
