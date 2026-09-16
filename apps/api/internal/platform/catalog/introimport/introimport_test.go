package introimport

import (
	"context"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/introimport")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("catalog/introimport", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("catalog/introimport", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("catalog/introimport", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("catalog/introimport", "src_vndb schema failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func TestBackfill(t *testing.T) {
	db := testDB
	for _, tbl := range []string{"catalog_work_intro", "catalog_external_ref", "catalog_work"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	require.NoError(t, db.Exec("TRUNCATE src_vndb.vn").Error)

	var vndbID, bangumiID int16
	db.Raw("SELECT id FROM catalog_source WHERE key='vndb'").Scan(&vndbID)
	db.Raw("SELECT id FROM catalog_source WHERE key='bangumi'").Scan(&bangumiID)
	require.NotZero(t, vndbID)
	require.NotZero(t, bangumiID)

	mkWork := func(name string, site *string) int64 {
		w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name, ContentRating: 0, Status: 0, Site: site}
		require.NoError(t, db.Create(&w).Error)
		return w.ID
	}
	anchor := func(workID int64, vid string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: workID, SourceID: vndbID, ExternalID: vid,
			LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
		}).Error)
	}
	vn := func(id, desc string) {
		require.NoError(t, db.Create(&srcvndb.VN{ID: id, OLang: "ja", Description: desc, IngestedAt: time.Now()}).Error)
	}

	w1 := mkWork("有描述", nil)
	anchor(w1, "v1")
	vn("v1", "A bodyless doujin blurb.")
	w2 := mkWork("锚无描述", nil)
	anchor(w2, "v2")
	mkWork("无锚", nil)
	site := "galgame_wiki"
	w4 := mkWork("已认领", &site)
	anchor(w4, "v4")
	vn("v4", "Claimed work — bridged, never copied.")
	w5 := mkWork("他源已有", nil)
	anchor(w5, "v5")
	vn("v5", "Would-be vndb blurb — must NOT stack a second en row.")
	require.NoError(t, db.Create(&model.CatalogWorkIntro{
		WorkID: w5, Lang: "en", Intro: "An earlier bangumi-sourced English summary.", SourceID: bangumiID,
	}).Error)

	ctx := context.Background()

	st, err := Run(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	assert.EqualValues(t, 4, st.TotalBodyless, "W1/W2/W3/W5 (claimed W4 excluded)")
	assert.EqualValues(t, 3, st.WithVNDBAnchor, "W1/W2/W5")
	assert.EqualValues(t, 1, st.SkippedNoAnchor, "W3")
	assert.EqualValues(t, 1, st.SkippedEmptyDesc, "W2 (anchor, no vn row)")
	assert.EqualValues(t, 1, st.Already, "W5 (en row from another source)")
	assert.EqualValues(t, 1, st.IntrosWritten, "W1 only")
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro").Scan(&n).Error)
	assert.EqualValues(t, 1, n, "dry run writes nothing (W5's pre-existing row only)")

	st, err = Run(ctx, db, Options{DryRun: false})
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.IntrosWritten)
	assert.EqualValues(t, 1, st.WorksCovered)
	var row model.CatalogWorkIntro
	require.NoError(t, db.Where("work_id = ?", w1).First(&row).Error)
	assert.Equal(t, "en", row.Lang)
	assert.EqualValues(t, vndbID, row.SourceID)
	assert.Equal(t, "A bodyless doujin blurb.", row.Intro)
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro WHERE work_id = ?", w4).Scan(&n).Error)
	assert.EqualValues(t, 0, n, "claimed work is never materialized")
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro WHERE work_id = ?", w5).Scan(&n).Error)
	assert.EqualValues(t, 1, n, "no second en row stacked onto W5")

	st, err = Run(ctx, db, Options{DryRun: false})
	require.NoError(t, err)
	assert.EqualValues(t, 2, st.Already, "W1 (own row) + W5 (other-source row)")
	assert.EqualValues(t, 0, st.IntrosWritten, "second run writes nothing")
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro").Scan(&n).Error)
	assert.EqualValues(t, 2, n)
}

func TestStripVNDBMarkup(t *testing.T) {
	cases := map[string]string{
		"A blurb.\r\n\r\n\r\n[From [url=https://www.dlsite.com/x]DLsite[/url]]": "A blurb.\n\n[From DLsite]",
		"[i]Turnabout[/i] begins. [spoiler]The butler did it.[/spoiler] Fin.":   "Turnabout begins.  Fin.",
		"Plain text stays.":                     "Plain text stays.",
		"Open spoiler [spoiler]runs to the end": "Open spoiler",
	}
	for in, want := range cases {
		assert.Equal(t, want, stripVNDBMarkup(in), in)
	}
}

func TestBackfillWritesPlainText(t *testing.T) {
	db := testDB
	for _, tbl := range []string{"catalog_work_intro", "catalog_external_ref", "catalog_work"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	require.NoError(t, db.Exec("TRUNCATE src_vndb.vn").Error)
	var vndbID int16
	require.NoError(t, db.Raw("SELECT id FROM catalog_source WHERE key='vndb'").Scan(&vndbID).Error)

	mk := func(name, vid, desc string) int64 {
		w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
		require.NoError(t, db.Create(&w).Error)
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: w.ID, SourceID: vndbID, ExternalID: vid,
			LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
		}).Error)
		require.NoError(t, db.Create(&srcvndb.VN{ID: vid, OLang: "ja", Description: desc, IngestedAt: time.Now()}).Error)
		return w.ID
	}
	marked := mk("标记", "v10", "A doujin blurb.\n\n[From [url=https://www.dlsite.com/x]DLsite[/url]]")
	spoilerOnly := mk("全剧透", "v11", "[spoiler]Everything is a spoiler.[/spoiler]")

	st, err := Run(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.IntrosWritten)
	assert.EqualValues(t, 1, st.SkippedEmptyDesc, "a description that is all spoiler leaves nothing to write")

	var row model.CatalogWorkIntro
	require.NoError(t, db.Where("work_id = ?", marked).First(&row).Error)
	assert.Equal(t, "A doujin blurb.\n\n[From DLsite]", row.Intro)
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro WHERE work_id = ?", spoilerOnly).Scan(&n).Error)
	assert.Zero(t, n)
}
