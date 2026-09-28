package llmsuggest

import (
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func (f *hltbFixture) galgameWork(t *testing.T, name string) (int64, int64) {
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
	return w.ID, rel.ID
}

func (f *hltbFixture) workLabel(t *testing.T, workID int64, display, aliasName, aliasLatin string) int64 {
	t.Helper()
	lab := &model.CatalogLabel{DisplayName: display, Lang: "ja", Kind: model.LabelKindPublisher}
	require.NoError(t, f.db.Create(lab).Error)
	if aliasName != "" {
		var latin *string
		if aliasLatin != "" {
			latin = &aliasLatin
		}
		require.NoError(t, f.db.Create(&model.CatalogLabelAlias{
			LabelID: lab.ID, Name: aliasName, Latin: latin, Lang: "en",
			Kind: model.AliasKindSpellingVariant, Provenance: model.AliasProvenanceSource,
		}).Error)
	}
	require.NoError(t, f.db.Create(&model.CatalogWorkLabel{
		WorkID: workID, LabelID: lab.ID, Kind: model.WorkLabelKindDeveloper,
	}).Error)
	return lab.ID
}

func (f *hltbFixture) releaseLabel(t *testing.T, releaseID int64, display string) {
	t.Helper()
	lab := &model.CatalogLabel{DisplayName: display, Lang: "ja", Kind: model.LabelKindPublisher}
	require.NoError(t, f.db.Create(lab).Error)
	require.NoError(t, f.db.Create(&model.CatalogReleaseLabel{
		ReleaseID: releaseID, LabelID: lab.ID, Kind: model.WorkLabelKindPublisher,
	}).Error)
}

func (f *hltbFixture) hltbGame(t *testing.T, hltbID int64, status, appid, dev, pub string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"game": []any{map[string]string{
				"profile_steam": appid,
				"profile_dev":   dev,
				"profile_pub":   pub,
			}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, f.mirror.Exec(
		`INSERT INTO games (hltb_id, status, raw) VALUES (?, ?, ?::jsonb)`,
		hltbID, status, string(raw)).Error)
}

func (f *hltbFixture) refTitleDate(workID int64, hltbID, hash string) refItem {
	return refItem{
		EntityType: model.EntityTypeWork, EntityID: workID,
		SourceID: f.reg.idByKey["howlongtobeat"], ExternalID: hltbID,
		MatchedBy: matchedByHLTBTitleDate, Hash: hash,
	}
}

func (f *hltbFixture) runTitleDate(t *testing.T, items []refItem) map[string]chainResult {
	t.Helper()
	out := map[string]chainResult{}
	require.NoError(t, verifyHLTBTitleDateChain(f.db, f.up, f.reg, items, out))
	return out
}

func chainStepNames(ev any) []string {
	m, ok := ev.(map[string]any)
	if !ok {
		return nil
	}
	steps, ok := m["steps"].([]chainStep)
	if !ok {
		return nil
	}
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name
	}
	return names
}

func requireVerdict(t *testing.T, got chainResult, verdict string, steps ...string) {
	t.Helper()
	require.Equal(t, verdict, got.Verdict, got.Reason)
	require.Equal(t, steps, chainStepNames(got.Evidence), got.Reason)
}

func TestHLTBTitleDateSteamAgreementVerifies(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "anchored", "787480")
	f.hltbGame(t, 7401, "fetched", "787480", "", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7401", "h")})
	requireVerdict(t, got["h"], VerdictChainVerified, "title_date", "hltb_appid", "work_steam_anchor")
}

func TestHLTBTitleDateWorkLevelSteamAnchorVerifies(t *testing.T) {
	f := newHLTBFixture(t)
	w, _ := f.galgameWork(t, "work-level")
	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: w, SourceID: f.steam,
		ExternalID: "787480", LinkKind: model.LinkKindExact, MatchedBy: "test:steam",
	}).Error)
	f.hltbGame(t, 7402, "fetched", "787480", "", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7402", "h")})
	requireVerdict(t, got["h"], VerdictChainVerified, "title_date", "hltb_appid", "work_steam_anchor")
}

func TestHLTBTitleDateSteamConflictVetoesDeveloper(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "original", "111")
	f.workLabel(t, w, "Frontwing", "", "")
	f.hltbGame(t, 7403, "fetched", "222", "Frontwing", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7403", "h")})
	requireVerdict(t, got["h"], VerdictChainUnproven, "steam_conflict")
}

func TestHLTBTitleDateSharedAppidFallsThroughToDeveloper(t *testing.T) {
	f := newHLTBFixture(t)
	a := f.steamAnchoredWork(t, "release-holder", "555001")
	b, _ := f.galgameWork(t, "work-holder")
	require.NoError(t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: b, SourceID: f.steam,
		ExternalID: "555001", LinkKind: model.LinkKindExact, MatchedBy: "test:steam",
	}).Error)
	f.workLabel(t, a, "Frontwing", "", "")
	f.workLabel(t, b, "OtherStudio", "", "")
	f.hltbGame(t, 7501, "fetched", "555001", "Frontwing", "")
	f.hltbGame(t, 7502, "fetched", "555001", "Frontwing", "")

	got := f.runTitleDate(t, []refItem{
		f.refTitleDate(a, "7501", "match"),
		f.refTitleDate(b, "7502", "miss"),
	})
	requireVerdict(t, got["match"], VerdictChainVerified, "title_date", "developer")
	requireVerdict(t, got["miss"], VerdictChainUnproven, "developer_mismatch")
}

func TestHLTBTitleDateDeveloperViaAliasLatinVerifies(t *testing.T) {
	f := newHLTBFixture(t)
	w, _ := f.galgameWork(t, "alice")
	f.workLabel(t, w, "アリスソフト", "別名義", "AliceSoft")
	f.hltbGame(t, 7601, "fetched", "", "Alice Soft, MangaGamer", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7601", "h")})
	requireVerdict(t, got["h"], VerdictChainVerified, "title_date", "developer")
}

func TestHLTBTitleDateDeveloperViaReleaseLabelVerifies(t *testing.T) {
	f := newHLTBFixture(t)
	w, rel := f.galgameWork(t, "release-label")
	f.releaseLabel(t, rel, "MangaGamer")
	f.hltbGame(t, 7602, "fetched", "", "", "MangaGamer")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7602", "h")})
	requireVerdict(t, got["h"], VerdictChainVerified, "title_date", "developer")
}

func TestHLTBTitleDateDeletedLabelAndReleaseIgnored(t *testing.T) {
	f := newHLTBFixture(t)
	w, rel := f.galgameWork(t, "deleted-paths")
	dead := f.workLabel(t, w, "AliceSoft", "", "")
	f.releaseLabel(t, rel, "MangaGamer")
	require.NoError(t, f.db.Exec(`UPDATE catalog_label SET deleted_at = NOW() WHERE id = ?`, dead).Error)
	require.NoError(t, f.db.Exec(`UPDATE catalog_release SET deleted_at = NOW() WHERE id = ?`, rel).Error)
	f.hltbGame(t, 7701, "fetched", "", "AliceSoft, MangaGamer", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7701", "h")})
	requireVerdict(t, got["h"], VerdictChainUnproven, "work_labels")
}

func TestHLTBTitleDateShortKeysNeverMatch(t *testing.T) {
	f := newHLTBFixture(t)
	w, _ := f.galgameWork(t, "short")
	f.workLabel(t, w, "アリ", "", "")
	f.hltbGame(t, 7702, "fetched", "", "アリ", "")

	got := f.runTitleDate(t, []refItem{f.refTitleDate(w, "7702", "h")})
	requireVerdict(t, got["h"], VerdictChainUnproven, "hltb_developer")
}

func TestHLTBTitleDateMissingRecordAndMissingMirror(t *testing.T) {
	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "host", "4242")
	f.hltbGame(t, 7202, "pending", "4242", "Frontwing", "")

	got := f.runTitleDate(t, []refItem{
		f.refTitleDate(w, "7202", "pending"),
		f.refTitleDate(w, "7203", "absent"),
		f.refTitleDate(w, "0", "zero"),
		f.refTitleDate(w, "abc", "bad"),
	})
	requireVerdict(t, got["pending"], VerdictChainUnproven, "hltb_record")
	requireVerdict(t, got["absent"], VerdictChainUnproven, "hltb_record")
	requireVerdict(t, got["zero"], VerdictChainUnproven, "hltb_record")
	requireVerdict(t, got["bad"], VerdictChainUnproven, "hltb_record")

	f.up.HLTB = nil
	got = f.runTitleDate(t, []refItem{f.refTitleDate(w, "7202", "nil")})
	requireVerdict(t, got["nil"], VerdictChainUnproven, "hltb_mirror")
}

func TestHLTBTitleDateUnprovenStepNames(t *testing.T) {
	f := newHLTBFixture(t)
	noDev, _ := f.galgameWork(t, "no-dev")
	f.workLabel(t, noDev, "Frontwing", "", "")
	f.hltbGame(t, 7101, "fetched", "", "", "")

	noLabel, _ := f.galgameWork(t, "no-label")
	f.hltbGame(t, 7102, "fetched", "", "Frontwing", "")

	mismatch, _ := f.galgameWork(t, "mismatch")
	f.workLabel(t, mismatch, "OtherStudio", "", "")
	f.hltbGame(t, 7103, "fetched", "", "Frontwing", "")

	got := f.runTitleDate(t, []refItem{
		f.refTitleDate(noDev, "7101", "dev"),
		f.refTitleDate(noLabel, "7102", "lab"),
		f.refTitleDate(mismatch, "7103", "mix"),
	})
	requireVerdict(t, got["dev"], VerdictChainUnproven, "hltb_developer")
	requireVerdict(t, got["lab"], VerdictChainUnproven, "work_labels")
	requireVerdict(t, got["mix"], VerdictChainUnproven, "developer_mismatch")
}

func TestHLTBTitleDateIsChainFamily(t *testing.T) {
	require.True(t, chainFamily(matchedByHLTBTitleDate))

	f := newHLTBFixture(t)
	w := f.steamAnchoredWork(t, "routed", "787480")
	f.mirrorRecord(t, 7301, "787480")
	it := f.refTitleDate(w, "7301", "h")
	rel := it
	rel.EntityType = model.EntityTypeRelease
	rel.Hash = "rel"

	out, err := verifyChainBatch(f.db, f.up, f.reg, []refItem{it, rel})
	require.NoError(t, err)
	requireVerdict(t, out["h"], VerdictChainVerified, "title_date", "hltb_appid", "work_steam_anchor")
	requireVerdict(t, out["rel"], VerdictChainUnproven, "entity_type")
}
