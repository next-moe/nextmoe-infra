package orglabels

import (
	"context"
	"testing"

	srcv "api/internal/platform/catalog/srcvndb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalisationPublisherIsEvidenceButNotAttribution(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	cleanAll(t)

	mkWork(t, 910)
	mkWorkAnchor(t, sourceVNDB, "v910", 910)

	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES
		('p90','co','en','Sekai Project','','',''),
		('p91','co','ja','きゃべつそふと','','','')`).Error)
	require.NoError(t, testDB.Create(&srcv.Release{ID: "rJA", OLang: "ja", Official: true}).Error)
	require.NoError(t, testDB.Create(&srcv.Release{ID: "rEN", OLang: "en", Official: true}).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_vn (id,vid,rtype) VALUES
		('rJA','v910','complete'),('rEN','v910','complete')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_producers (id,pid,developer,publisher) VALUES
		('rJA','p91',true,true),('rEN','p90',false,true)`).Error)

	orgs, err := loadVNDBOrgs(testDB, 0)
	require.NoError(t, err)
	by := map[string]orgRec{}
	for _, o := range orgs {
		by[o.extID] = o
	}

	assert.Equal(t, []int64{910}, by["p90"].works, "the EN publisher stays visible as evidence")
	assert.Empty(t, by["p90"].attribWorks, "the EN publisher is not attributable to a Japanese work")
	assert.True(t, by["p90"].editionAware, "vndb states editions, so the empty set is an ANSWER")
	assert.Equal(t, []int64{910}, by["p91"].attribWorks, "the original developer is")
}

func TestMintFollowsAttributionNotEvidence(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	cleanAll(t)

	mkWork(t, 920)
	mkWorkAnchor(t, sourceVNDB, "v920", 920)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES
		('p92','co','en','Some Localiser','','','')`).Error)
	require.NoError(t, testDB.Create(&srcv.Release{ID: "rL", OLang: "en", Official: true}).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_vn (id,vid,rtype) VALUES ('rL','v920','complete')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_producers (id,pid,developer,publisher) VALUES ('rL','p92',false,true)`).Error)

	_, err := anchorAll(context.Background(), testDB, testDB, "vndb", 0, true)
	require.NoError(t, err)

	var edges int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM catalog_work_label WHERE work_id = 920`).Scan(&edges).Error)
	assert.Zero(t, edges, "a company known only from the English release must not be credited with the Japanese work")
}

func TestPatchGroupIsNeverAttributed(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	cleanAll(t)

	mkWork(t, 930)
	mkWorkAnchor(t, sourceVNDB, "v930", 930)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES
		('p93','ng','ja','パッチ班','','','')`).Error)
	require.NoError(t, testDB.Create(&srcv.Release{ID: "rP", OLang: "ja", Patch: true}).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_vn (id,vid,rtype) VALUES ('rP','v930','complete')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_producers (id,pid,developer,publisher) VALUES ('rP','p93',true,false)`).Error)

	orgs, err := loadVNDBOrgs(testDB, 0)
	require.NoError(t, err)
	for _, o := range orgs {
		if o.extID == "p93" {
			assert.Empty(t, o.attribWorks, "a patch group did not make the work")
			assert.Equal(t, []int64{930}, o.works, "…but it is still evidence")
			return
		}
	}
	t.Fatal("p93 not loaded")
}

func TestBangumiCountsOnlyDeveloperAndPublisherAndNeverMints(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	cleanAll(t)

	mkWork(t, 940)
	mkWorkAnchor(t, sourceBangumi, "940", 940)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_bangumi.person (id,name,type,summary,comments,collects,parser_version,ingested_at,infobox_raw,infobox_parsed,parse_error)
		 VALUES (77,'ある会社',2,'',0,0,1,now(),'','{}',''), (78,'主題歌バンド',3,'',0,0,1,now(),'','{}',''),
		        (79,'配信会社',2,'',0,0,1,now(),'','{}','')`).Error)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_bangumi.subject_person (subject_id,person_id,position,appear_eps) VALUES
		 (940,77,1001,''), (940,78,1011,''), (940,79,1,'')`).Error)

	orgs, err := loadBGMOrgs(testDB, 0)
	require.NoError(t, err)
	require.Len(t, orgs, 3)
	works := map[string][]int64{}
	for _, o := range orgs {
		works[o.extID] = o.works
		assert.False(t, o.canCreate, "bangumi org %s must not mint", o.extID)
	}
	assert.Equal(t, []int64{940}, works["77"], "the developer")
	assert.Empty(t, works["78"], "a theme-song credit is not a label")
	assert.Empty(t, works["79"], "nor is an unmapped position")

	st, err := anchorAll(context.Background(), testDB, testDB, "bangumi", 0, true)
	require.NoError(t, err)
	assert.Zero(t, st.NewLabels)
	var labels, edges int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_label`).Scan(&labels).Error)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_label`).Scan(&edges).Error)
	assert.Zero(t, labels, "the bangumi lane anchors and never mints")
	assert.Zero(t, edges)
}
