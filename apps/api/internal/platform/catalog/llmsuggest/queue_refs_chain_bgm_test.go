package llmsuggest

import (
	"strconv"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type bgmSteamFixture struct {
	db     *gorm.DB
	reg    sourceReg
	medium int16
}

func newBgmSteamFixture(t *testing.T) *bgmSteamFixture {
	t.Helper()
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_work, catalog_release, catalog_external_ref, src_bangumi.subject RESTART IDENTITY CASCADE").Error)
	reg, err := loadSourceReg(db)
	require.NoError(t, err)
	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	return &bgmSteamFixture{db: db, reg: reg, medium: medium}
}

func (f *bgmSteamFixture) work(t *testing.T, name string, subject int64, steamLinks ...string) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: f.medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, f.db.Create(w).Error)
	if subject == 0 {
		return w.ID
	}
	items := ""
	for i, l := range steamLinks {
		if i > 0 {
			items += ","
		}
		items += `{"Key":"Steam","Value":"` + l + `"}`
	}
	require.NoError(t, f.db.Create(&srcbangumi.Subject{
		ID: subject, Type: 4, Name: name, IngestedAt: time.Now(),
		InfoboxParsed: []byte(`{"Fields":[{"Key":"链接","Items":[` + items + `]}]}`),
	}).Error)
	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: w.ID, SourceID: f.reg.id(sourceKeyBangumi),
		ExternalID: strconv.FormatInt(subject, 10), LinkKind: model.LinkKindExact, MatchedBy: "test:bgm",
	}).Error)
	return w.ID
}

func (f *bgmSteamFixture) verify(t *testing.T, workID int64, appid string) chainResult {
	t.Helper()
	it := refItem{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: f.reg.id(sourceKeySteam),
		ExternalID: appid, MatchedBy: matchedByBgmSteam, Hash: "h",
	}
	out := map[string]chainResult{}
	require.NoError(t, verifyBgmSteamChain(f.db, f.reg, []refItem{it}, out))
	return out["h"]
}

func TestBgmSteamChainVerifiesTheAppidTheSubjectNames(t *testing.T) {
	f := newBgmSteamFixture(t)
	w := f.work(t, "named", 71, "https://store.steampowered.com/app/1001/")

	got := f.verify(t, w, "1001")
	require.Equal(t, VerdictChainVerified, got.Verdict, got.Reason)
	require.Equal(t, float64(1), got.Confidence)
}

func TestBgmSteamChainRefusals(t *testing.T) {
	f := newBgmSteamFixture(t)
	named := f.work(t, "named", 71, "https://store.steampowered.com/app/1001/")
	two := f.work(t, "two", 72, "https://store.steampowered.com/app/1002/", "https://store.steampowered.com/app/1003/")
	unanchored := f.work(t, "unanchored", 0)
	taken := f.work(t, "taken", 74, "https://store.steampowered.com/app/1004/")
	holder := f.work(t, "holder", 0)
	rel := &model.CatalogRelease{WorkID: holder, Kind: model.ReleaseKindDefault}
	require.NoError(t, f.db.Create(rel).Error)
	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: f.reg.id(sourceKeySteam),
		ExternalID: "1004", LinkKind: model.LinkKindExact, MatchedBy: "rule:vndb-extlink-steam",
	}).Error)

	for _, c := range []struct {
		name   string
		work   int64
		appid  string
		reason string
	}{
		{"a different appid than the subject names", named, "9999", "subject_names_appid"},
		{"a subject naming two appids", two, "1002", "subject_names_appid"},
		{"no bangumi anchor", unanchored, "1001", "work_exact_bangumi"},
		{"an appid another work holds", taken, "1004", "steam_anchor"},
	} {
		got := f.verify(t, c.work, c.appid)
		require.Equal(t, VerdictChainUnproven, got.Verdict, c.name)
		require.Contains(t, got.Reason, c.reason, c.name)
	}
}
