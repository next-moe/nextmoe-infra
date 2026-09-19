package getchuattach

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoRelationMintsLive(t *testing.T) {
	requireDB(t)
	lid := seedMintBrand(t, 7, "Mint Brand")
	insertItem(t, item{
		GetchuID: "900", Title: "Brand New Getchu Only Game", Brand: "Mint Brand", BrandID: 7,
		ReleaseDate: "2001/06/15", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Zero(t, st.MintedQuarantined)
	assert.Equal(t, 1, st.MintGroups)
	w := mintedWork(t, "900")
	assert.Equal(t, mediumID(t), w.MediumID)
	assert.Equal(t, model.OLangDefault, w.OLang)
	assert.Equal(t, "Brand New Getchu Only Game", w.DisplayName)
	assert.Equal(t, model.ContentRatingR18, w.ContentRating)
	assert.Equal(t, model.WorkStatusLive, w.Status)

	var titles []model.CatalogWorkTitle
	require.NoError(t, testDB.Where("work_id = ?", w.ID).Find(&titles).Error)
	require.Len(t, titles, 1)
	assert.Equal(t, "Brand New Getchu Only Game", titles[0].Title)
	assert.Equal(t, "ja", titles[0].Lang)
	assert.Equal(t, model.WorkTitleKindOfficial, titles[0].Kind)

	rel := attachedRelease(t, "900")
	assert.Equal(t, model.ReleaseKindPhysical, rel.Kind)
	require.NotNil(t, rel.Title)
	assert.Equal(t, "Brand New Getchu Only Game", *rel.Title)
	require.NotNil(t, rel.ReleasedY)
	assert.Equal(t, int16(2001), *rel.ReleasedY)
	assert.Equal(t, int16(6), *rel.ReleasedM)
	assert.Equal(t, int16(15), *rel.ReleasedD)
	assert.Equal(t, ruleWorkImport, getchuRefs(t, "900")[0].MatchedBy)

	var edge model.CatalogWorkLabel
	require.NoError(t, testDB.Where("work_id = ?", w.ID).First(&edge).Error)
	assert.Equal(t, lid, edge.LabelID)
	assert.Equal(t, model.WorkLabelKindDeveloper, edge.Kind)

	var revs []model.CatalogRevision
	require.NoError(t, testDB.Where("entity_id IN ?", []int64{w.ID, rel.ID}).Order("entity_type").Find(&revs).Error)
	require.Len(t, revs, 2)
	assert.Equal(t, model.RevisionActionImported, revs[0].Action)
	assert.Equal(t, model.RevisionActionImported, revs[1].Action)
}

func TestShortTitleEqualityQuarantines(t *testing.T) {
	requireDB(t)
	live := mkWork(t, "同棲")
	mkReleaseYMD(t, live, i16(1999), i16(1), i16(1))
	seedMintBrand(t, 8, "ShortBrand")
	insertItem(t, item{
		GetchuID: "901", Title: "同棲", Brand: "ShortBrand", BrandID: 8,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedQuarantined)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, 1, st.Candidates)
	w := mintedWork(t, "901")
	assert.Equal(t, model.WorkStatusQuarantine, w.Status)
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate WHERE a_id IN (?, ?) AND b_id IN (?, ?)`,
		w.ID, live, w.ID, live).Scan(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestQuarantinedWorkIsARelation(t *testing.T) {
	requireDB(t)
	q := mkWorkStatus(t, "Quarantine Relation Title", model.WorkStatusQuarantine)
	mkReleaseYMD(t, q, i16(2001), i16(1), i16(1))
	seedMintBrand(t, 9, "QBrand")
	insertItem(t, item{
		GetchuID: "902", Title: "Quarantine Relation Title", Brand: "QBrand", BrandID: 9,
		ReleaseDate: "2002/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedQuarantined)
	w := mintedWork(t, "902")
	assert.Equal(t, model.WorkStatusQuarantine, w.Status)
	assert.NotEqual(t, q, w.ID)
}

func TestEditionsFoldIntoOneWork(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 10, "FoldBrand")
	insertItem(t, item{
		GetchuID: "903", Title: "Fold Game 初回版", Brand: "FoldBrand", BrandID: 10,
		ReleaseDate: "2001/01/01", Adult: true,
	})
	insertItem(t, item{
		GetchuID: "904", Title: "Fold Game 通常版", Brand: "FoldBrand", BrandID: 10,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintGroups)
	assert.Equal(t, 1, st.MintedLive)
	a, b := attachedRelease(t, "903"), attachedRelease(t, "904")
	assert.Equal(t, a.WorkID, b.WorkID)
	assert.NotEqual(t, a.ID, b.ID)
	w := mintedWork(t, "903")
	assert.Equal(t, "Fold Game", w.DisplayName)
	require.NotNil(t, a.Title)
	require.NotNil(t, b.Title)
	assert.Equal(t, "Fold Game 初回版", *a.Title)
	assert.Equal(t, "Fold Game 通常版", *b.Title)
	assert.Len(t, getchuRefs(t, "903"), 1)
	assert.Len(t, getchuRefs(t, "904"), 1)
}

func TestSameTitleOtherBrandIsNotFolded(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 11, "BrandOne")
	seedMintBrand(t, 12, "BrandTwo")
	insertItem(t, item{
		GetchuID: "905", Title: "Shared Mint Title", Brand: "BrandOne", BrandID: 11,
		ReleaseDate: "2001/01/01", Adult: true,
	})
	insertItem(t, item{
		GetchuID: "906", Title: "Shared Mint Title", Brand: "BrandTwo", BrandID: 12,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 2, st.MintedLive)
	assert.Equal(t, 2, st.MintGroups)
	assert.NotEqual(t, attachedRelease(t, "905").WorkID, attachedRelease(t, "906").WorkID)
}
