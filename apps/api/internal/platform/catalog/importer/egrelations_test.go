package importer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedEGWork(t *testing.T, gid string) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, display_nsfw)
		VALUES (1, 'ja', 'eg-'||?::text, 0, 0, '{}', false) RETURNING id`, gid).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, ?, 0, 'rule:test')`, workID, gid).Error)
	return workID
}

func seedEGRel(t *testing.T, pk int, subject, object int64, kind string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO game_relations (raw, game_subject, game_object, synced_at, pk)
		VALUES (?, ?, ?, now(), ?)`, fmt.Sprintf(`{"kind": %q}`, kind), subject, object, pk).Error)
}

func TestEGRelations(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)
	require.NoError(t, testDB.Exec(`CREATE TABLE IF NOT EXISTS game_relations
		(raw text, game_subject bigint, game_object bigint, synced_at timestamptz, pk bigint)`).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE game_relations`).Error)

	w1 := seedEGWork(t, "100")
	w2 := seedEGWork(t, "200")
	w3 := seedEGWork(t, "300")
	w4 := seedEGWork(t, "400")
	seedEGWork(t, "500")

	seedEGRel(t, 1, 200, 100, "sequel")
	seedEGRel(t, 2, 200, 100, "sequel")
	seedEGRel(t, 3, 300, 100, "fandisk")
	seedEGRel(t, 4, 400, 100, "transplant")
	seedEGRel(t, 5, 500, 100, "bundling")
	seedEGRel(t, 6, 500, 100, "banana")
	seedEGRel(t, 7, 999, 100, "sequel")
	seedEGRel(t, 8, 100, 100, "remake")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_relation (a_work_id, b_work_id, relation_type_id, source_id)
		VALUES (?, ?, 4, 2)`, w3, w1).Error)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunEGRelations()
	require.NoError(t, err)
	assert.Equal(t, 8, dry.TotalRows)
	assert.Equal(t, 1, dry.SkippedByDesign, "bundling")
	assert.Equal(t, 1, dry.SkippedUnmapped, "banana")
	assert.Equal(t, 6, dry.Mapped)
	assert.Equal(t, 2, dry.Edges, "sequel + transplant")
	assert.Equal(t, 1, dry.Folded, "duplicate sequel row")
	assert.Equal(t, 1, dry.AlreadyInDB, "vndb asserted the fandisc pair first")
	assert.Equal(t, 1, dry.SkippedUnanchored)
	assert.Equal(t, 1, dry.SkippedSelf)
	assert.Zero(t, dry.EdgesWritten)
	assert.Equal(t, dry.Mapped, dry.Edges+dry.Folded+dry.AlreadyInDB+dry.SkippedUnanchored+dry.SkippedSelf)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_relation`), "dry writes nothing (only the vndb fixture row)")

	st, err := New(testDB, testDB, Options{}).RunEGRelations()
	require.NoError(t, err)
	assert.Equal(t, 2, st.EdgesWritten)

	assert.Equal(t, int64(1), scalarInt(t,
		`SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(w2)+` AND b_work_id=`+itoa64(w1)+` AND relation_type_id=2 AND source_id=5`),
		"w2 is the sequel of w1")
	assert.Zero(t, scalarInt(t,
		`SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(w1)+` AND b_work_id=`+itoa64(w2)+` AND relation_type_id=2`),
		"no inverted sequel edge")
	assert.Equal(t, int64(1), scalarInt(t,
		`SELECT count(*) FROM catalog_work_relation WHERE a_work_id=`+itoa64(w1)+` AND b_work_id=`+itoa64(w4)+` AND relation_type_id=12 AND source_id=5`),
		"transplant normalized to (min,max)")

	st2, err := New(testDB, testDB, Options{}).RunEGRelations()
	require.NoError(t, err)
	assert.Zero(t, st2.Edges)
	assert.Zero(t, st2.EdgesWritten)
	assert.Equal(t, 4, st2.AlreadyInDB, "sequel + dup + fandisc + transplant all in db now")
	assert.Equal(t, st2.Mapped, st2.Edges+st2.Folded+st2.AlreadyInDB+st2.SkippedUnanchored+st2.SkippedSelf)
}

// 212193 (EG 19150) is a transplant of EG 14276, which 458 holds only as an
// eg-xlink-twin of its exact 16470.
func TestEGRelationsReachATwinHolder(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)
	require.NoError(t, testDB.Exec(`CREATE TABLE IF NOT EXISTS game_relations
		(raw text, game_subject bigint, game_object bigint, synced_at timestamptz, pk bigint)`).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE game_relations`).Error)

	orig := seedEGWork(t, "16470")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, '14276', 2, 'rule:eg-xlink-twin')`, orig).Error)
	port := seedEGWork(t, "19150")
	seedEGRel(t, 1, 19150, 14276, "transplant")

	st, err := New(testDB, testDB, Options{}).RunEGRelations()
	require.NoError(t, err)
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(
		`SELECT count(*) FROM catalog_work_relation WHERE a_work_id=%d AND b_work_id=%d AND relation_type_id=12 AND source_id=5`,
		min(orig, port), max(orig, port))))
}
