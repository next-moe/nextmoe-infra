package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertSubject(t *testing.T, id int64, stype, platform int, name, nameCN string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, parser_version, ingested_at)
		VALUES (?, ?, ?, ?, '', '', ?, '', false, '', false, 0, 0, 'v', now())`,
		id, stype, name, nameCN, platform).Error)
}

func TestBangumiXmedia(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	w100 := seedAnchoredWork(t, 100)

	insertSubject(t, 200, 2, 0, "アニメ200", "动画200")
	insertSubject(t, 201, 1, 1001, "漫画201", "漫画201cn")
	insertSubject(t, 202, 1, 1002, "小説202", "")
	insertSubject(t, 203, 1, 1003, "画集203", "")
	insertSubject(t, 300, 2, 0, "無関係アニメ", "")

	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_relation (subject_id, relation_type, related_subject_id, item_order) VALUES
		(100, 1, 200, 0), (200, 1, 100, 0),  -- anime, both directions (bidirectional 改编) → one edge
		(100, 1, 201, 0),                     -- manga (galgame is subject)
		(202, 1, 100, 0),                     -- novel (galgame is the related side)
		(100, 1, 203, 0)`).Error)

	dry, err := New(testDB, nil, Options{DryRun: true}).RunBangumiXmedia()
	require.NoError(t, err)
	assert.Equal(t, 1, dry.RegisteredAnime)
	assert.Equal(t, 1, dry.RegisteredManga)
	assert.Equal(t, 1, dry.RegisteredNovel)
	assert.Equal(t, 1, dry.SkippedPlatform, "画集 platform is not manga/novel")
	assert.Equal(t, 3, dry.Edges, "anime + manga + novel; the art book has no edge")
	assert.Zero(t, dry.EdgesWritten)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE medium_id IN (2,3,4)`), "dry writes no works")

	st, err := New(testDB, nil, Options{}).RunBangumiXmedia()
	require.NoError(t, err)
	assert.Equal(t, 3, st.EdgesWritten)

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w JOIN catalog_external_ref r ON r.entity_type=5 AND r.entity_id=w.id AND r.source_id=3 AND r.link_kind=0 AND r.matched_by='rule:bangumi-xmedia-import' WHERE w.medium_id=4 AND w.site IS NULL AND r.external_id='200'`), "anime 200 registered + anchored")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w JOIN catalog_external_ref r ON r.entity_type=5 AND r.entity_id=w.id AND r.source_id=3 AND r.matched_by='rule:bangumi-xmedia-import' WHERE w.medium_id=2 AND r.external_id='201'`), "manga 201")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w JOIN catalog_external_ref r ON r.entity_type=5 AND r.entity_id=w.id AND r.source_id=3 AND r.matched_by='rule:bangumi-xmedia-import' WHERE w.medium_id=3 AND r.external_id='202'`), "novel 202")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE source_id=3 AND matched_by='rule:bangumi-xmedia-import' AND external_id IN ('203','300')`), "art book skipped; unreachable anime not registered")

	animeW := scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE source_id=3 AND matched_by='rule:bangumi-xmedia-import' AND external_id='200'`)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(animeW)+` AND b_work_id=`+itoa64(w100)+` AND relation_type_id=1 AND source_id=3`), "anime adaptation_of galgame")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(w100)+` AND b_work_id=`+itoa64(animeW)+` AND relation_type_id=1`), "not the reverse direction")

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title t JOIN catalog_external_ref r ON r.entity_type=5 AND r.entity_id=t.work_id AND r.external_id='200' AND r.matched_by='rule:bangumi-xmedia-import' WHERE t.lang='zh' AND t.title='动画200'`), "zh title for anime 200")

	st2, err := New(testDB, nil, Options{}).RunBangumiXmedia()
	require.NoError(t, err)
	assert.Zero(t, st2.RegisteredAnime+st2.RegisteredManga+st2.RegisteredNovel)
	assert.Zero(t, st2.EdgesWritten)
	assert.Equal(t, 3, st2.AlreadyWork)
	assert.Equal(t, 3, st2.AlreadyEdge)
}

func TestBangumiXmediaProbableOccupancy(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	gExact := seedAnchoredWork(t, 100)

	var gProbable int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'ja','probable-galgame',0,0,'{}','{}',false) RETURNING id`).Scan(&gProbable).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, '110', 1, 'test:probable-galgame')`, gProbable).Error)

	var xHeld int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (4,'ja','held-anime',0,0,'{}','{}',false) RETURNING id`).Scan(&xHeld).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, '400', 1, 'test:probable-xmedia')`, xHeld).Error)

	insertSubject(t, 400, 2, 0, "既存アニメ400", "")
	insertSubject(t, 401, 2, 0, "未登録アニメ401", "")
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_relation (subject_id, relation_type, related_subject_id, item_order) VALUES
		(100, 1, 400, 0),
		(110, 1, 401, 0)`).Error)

	st, err := New(testDB, nil, Options{}).RunBangumiXmedia()
	require.NoError(t, err)
	assert.Equal(t, 1, st.AlreadyWork)
	assert.Zero(t, st.RegisteredAnime)
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(xHeld)+` AND b_work_id=`+itoa64(gExact)+` AND relation_type_id=1`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_relation WHERE b_work_id=`+itoa64(gProbable)))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by='rule:bangumi-xmedia-import'`))
}

func TestBangumiXmediaSkipsAStubMergedIntoItsGalgame(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	merged := seedAnchoredWork(t, 100)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, '500', 1, 'rule:bangumi-xmedia-import')`, merged).Error)
	insertSubject(t, 500, 1, 1002, "小説500", "")
	insertSubject(t, 501, 2, 0, "アニメ501", "")
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_relation (subject_id, relation_type, related_subject_id, item_order) VALUES
		(500, 1, 100, 0), (100, 1, 501, 0)`).Error)

	st, err := New(testDB, nil, Options{}).RunBangumiXmedia()
	require.NoError(t, err, "one self-pair must not fail the batch")
	assert.Equal(t, 1, st.SkippedSelf, "the novel's stub was merged into the galgame it adapts")
	assert.Equal(t, 1, st.EdgesWritten, "the anime edge still lands")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_relation WHERE a_work_id = b_work_id`))
}
