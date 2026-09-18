package llmsuggest

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDeclaredDB(t *testing.T) (*gorm.DB, int16, int16, int16) {
	t.Helper()
	db, medium := setupWorkPairDB(t)
	require.NoError(t, srcbangumi.EnsureSchema(db))
	require.NoError(t, db.Exec(`TRUNCATE src_bangumi.subject RESTART IDENTITY CASCADE`).Error)
	var bgm, dlsite int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&bgm).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsite).Error)
	return db, medium, bgm, dlsite
}

func createBangumiSubject(t *testing.T, db *gorm.DB, id int64, infobox string) {
	t.Helper()
	require.NoError(t, db.Create(&srcbangumi.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id),
		InfoboxRaw: infobox, ParseError: "", Summary: "", Date: "",
		ParserVersion: srcbangumi.ParserVersion, IngestedAt: time.Now(),
	}).Error)
}

func exactWorkRef(t *testing.T, db *gorm.DB, workID int64, source int16, ext string) {
	t.Helper()
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
}

func dlsiteReleaseAnchor(t *testing.T, db *gorm.DB, workID int64, dlsite int16, workno string) int64 {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	require.NoError(t, db.Create(rel).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: dlsite,
		ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	return rel.ID
}

func TestDeclaredLinkCorroborates(t *testing.T) {
	db, medium, bgm, dlsite := setupDeclaredDB(t)
	a := createLiveWork(t, db, medium, "declared side alpha")
	b := createLiveWork(t, db, medium, "anchored side beta")
	createBangumiSubject(t, db, 9001, `{{Infobox|DLsite=RJ010001}}`)
	exactWorkRef(t, db, a, bgm, "9001")
	dlsiteReleaseAnchor(t, db, b, dlsite, "RJ010001")
	fileWorkPair(t, db, a, b, VerdictSame, "hash-declared")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, pairStatus(t, db, a, b))

	var action string
	require.NoError(t, db.Raw(`SELECT applied_action FROM src_llm.queue_verdict WHERE input_hash = 'hash-declared'`).Scan(&action).Error)
	assert.Equal(t, applyAccept, action)
}

func TestDeclaredTokenIsWholeNotPrefix(t *testing.T) {
	db, medium, bgm, dlsite := setupDeclaredDB(t)
	a := createLiveWork(t, db, medium, "prefix declarer")
	b := createLiveWork(t, db, medium, "prefix anchor")
	createBangumiSubject(t, db, 9002, `see RJ01000270`)
	exactWorkRef(t, db, a, bgm, "9002")
	dlsiteReleaseAnchor(t, db, b, dlsite, "RJ010002")
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.Empty(t, ev[1].DeclaredWorkno)
	assert.False(t, ev[1].exclusiveDeclared())
}

func TestDeclaredNeedsOneDeclarerAndOneAnchor(t *testing.T) {
	db, medium, bgm, dlsite := setupDeclaredDB(t)

	d1 := createLiveWork(t, db, medium, "two declarers a")
	d2 := createLiveWork(t, db, medium, "two declarers c")
	a1 := createLiveWork(t, db, medium, "two declarers b")
	createBangumiSubject(t, db, 9003, `RJ010003`)
	createBangumiSubject(t, db, 9004, `RJ010003`)
	exactWorkRef(t, db, d1, bgm, "9003")
	exactWorkRef(t, db, d2, bgm, "9004")
	dlsiteReleaseAnchor(t, db, a1, dlsite, "RJ010003")

	oneD := createLiveWork(t, db, medium, "two anchors a")
	twoA := createLiveWork(t, db, medium, "two anchors b")
	twoC := createLiveWork(t, db, medium, "two anchors c")
	createBangumiSubject(t, db, 9005, `RJ010004`)
	exactWorkRef(t, db, oneD, bgm, "9005")
	dlsiteReleaseAnchor(t, db, twoA, dlsite, "RJ010004")
	dlsiteReleaseAnchor(t, db, twoC, dlsite, "https://www.dlsite.com/maniax/work/=/product_id/RJ010004.html")

	rows := []QueueVerdict{
		{ID: 1, AID: min(d1, a1), BID: max(d1, a1)},
		{ID: 2, AID: min(oneD, twoA), BID: max(oneD, twoA)},
	}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusiveDeclared(), "two works declare the same workno")
	assert.False(t, ev[2].exclusiveDeclared(), "two works anchor the same workno")
}

func TestDeadBangumiRefDeclaresNothing(t *testing.T) {
	db, medium, bgm, dlsite := setupDeclaredDB(t)
	a := createLiveWork(t, db, medium, "dead declarer")
	b := createLiveWork(t, db, medium, "dead anchor")
	createBangumiSubject(t, db, 9006, `RJ010006`)
	dead := time.Now()
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: a, SourceID: bgm,
		ExternalID: "9006", LinkKind: model.LinkKindExact, MatchedBy: "test",
		DeadAt: &dead,
	}).Error)
	dlsiteReleaseAnchor(t, db, b, dlsite, "RJ010006")
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusiveDeclared())
}

func TestDeletedReleaseAnchorsNothing(t *testing.T) {
	db, medium, bgm, dlsite := setupDeclaredDB(t)
	a := createLiveWork(t, db, medium, "deleted-rel declarer")
	b := createLiveWork(t, db, medium, "deleted-rel anchor")
	createBangumiSubject(t, db, 9007, `RJ010007`)
	exactWorkRef(t, db, a, bgm, strconv.FormatInt(9007, 10))
	relID := dlsiteReleaseAnchor(t, db, b, dlsite, "RJ010007")
	require.NoError(t, db.Delete(&model.CatalogRelease{}, relID).Error)
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusiveDeclared())
}
