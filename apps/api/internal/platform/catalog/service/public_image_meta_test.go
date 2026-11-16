package service

import (
	"context"
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

func sexualPtr(v int16) *int16 { return &v }

func assertImageMetaJSONOmitsViolence(t *testing.T, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal image_meta: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal image_meta: %v", err)
	}
	if _, ok := obj["violence"]; ok {
		t.Fatalf("image_meta JSON = %s, want no violence key", raw)
	}
}

func TestPopulatedImageMetaJSONOmitsViolence(t *testing.T) {
	assertImageMetaJSONOmitsViolence(t, dto.PublicImageMeta{
		Width: 800, Height: 600, Thumbhash: "th", Sexual: sexualPtr(1),
	})
}

func metaFetcher(t *testing.T, calls *int, table map[string]ImageMeta) ImageMetaFunc {
	t.Helper()
	return func(_ context.Context, hashes []string) (map[string]ImageMeta, error) {
		*calls++
		out := make(map[string]ImageMeta, len(hashes))
		for _, h := range hashes {
			if m, ok := table[h]; ok {
				out[h] = m
			}
		}
		return out, nil
	}
}

func TestUngradedMetaIsServedButNotCached(t *testing.T) {
	hash := hash64("cafe")
	table := map[string]ImageMeta{hash: {Width: 800, Height: 600, Thumbhash: "th"}}
	calls := 0
	svc := (&PublicService{}).WithImageMeta(metaFetcher(t, &calls, table))
	ctx := context.Background()

	got := svc.resolveImageMeta(ctx, []string{hash})
	if got[hash].Width != 800 || got[hash].Sexual != nil {
		t.Fatalf("ungraded entry must still be served: %+v", got[hash])
	}
	if _, cached := svc.metaCache.get(hash); cached {
		t.Fatal("an ungraded entry must not be cached: the nightly grader fills it in behind this read")
	}

	table[hash] = ImageMeta{Width: 800, Height: 600, Thumbhash: "th", Sexual: sexualPtr(1)}
	got = svc.resolveImageMeta(ctx, []string{hash})
	if calls != 2 {
		t.Fatalf("fetches = %d, want a second one: nothing was cached to serve it from", calls)
	}
	if got[hash].Sexual == nil || *got[hash].Sexual != 1 {
		t.Fatalf("graded entry = %+v, want sexual 1", got[hash])
	}
	if _, cached := svc.metaCache.get(hash); !cached {
		t.Fatal("a complete entry must be cached")
	}

	svc.resolveImageMeta(ctx, []string{hash})
	if calls != 2 {
		t.Fatalf("fetches = %d, want the cached entry to answer", calls)
	}
}

func TestThumbhashlessMetaStaysUncached(t *testing.T) {
	hash := hash64("beef")
	calls := 0
	svc := (&PublicService{}).WithImageMeta(metaFetcher(t, &calls, map[string]ImageMeta{
		hash: {Width: 10, Height: 10, Sexual: sexualPtr(0)},
	}))

	svc.resolveImageMeta(context.Background(), []string{hash})
	if _, cached := svc.metaCache.get(hash); cached {
		t.Fatal("a graded entry still missing its thumbhash must not be cached")
	}
}

func TestRosterCarriesImageMetaOnlyForResolvedHashes(t *testing.T) {
	svc := &PublicService{cdnBase: testCDNBase}
	portrait, figure := hash64("a1"), hash64("a2")
	rows := []WorkCharacterRow{{
		CharacterID: 7, DisplayName: "藍", ImageHash: &portrait, FigureHash: &figure,
	}}

	out := svc.publicRoster(rows, map[string]ImageMeta{
		portrait: {Width: 256, Height: 256, Thumbhash: "th", Sexual: sexualPtr(2)},
	})
	if len(out) != 1 {
		t.Fatalf("roster = %+v", out)
	}
	got := out[0]
	if got.ImageMeta == nil || got.ImageMeta.Width != 256 || got.ImageMeta.Thumbhash != "th" {
		t.Fatalf("image_meta = %+v, want the resolved entry", got.ImageMeta)
	}
	if got.ImageMeta.Sexual == nil || *got.ImageMeta.Sexual != 2 {
		t.Fatalf("image_meta.sexual = %v, want 2", got.ImageMeta.Sexual)
	}
	assertImageMetaJSONOmitsViolence(t, got.ImageMeta)
	if got.Figure == "" {
		t.Fatal("figure URL must still render without its metadata")
	}
	if got.FigureMeta != nil {
		t.Fatalf("figure_meta = %+v, want absent when the lookup did not answer for it", got.FigureMeta)
	}
}

func TestEntityFacesCarryImageMeta(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	photo, logo := hash64("70"), hash64("1090")
	charImage, charFigure := hash64("c1"), hash64("c2")

	p := createPerson(t, "Photographed")
	if err := testDB.Model(p).Update("photo_hash", photo).Error; err != nil {
		t.Fatalf("set photo hash: %v", err)
	}
	name := createCreditName(t, &p.ID, "写真あり")

	labelID := createLabel(t, "Logo Brand", model.LabelKindGameBrand)
	if err := testDB.Model(&model.CatalogLabel{}).Where("id = ?", labelID).
		Update("logo_hash", logo).Error; err != nil {
		t.Fatalf("set logo hash: %v", err)
	}

	ch := createCharacter(t, "立ち絵あり")
	if err := testDB.Model(ch).Updates(map[string]any{
		"image_hash": charImage, "figure_hash": charFigure,
	}).Error; err != nil {
		t.Fatalf("set character art: %v", err)
	}

	calls := 0
	svc := newPublicSvcCDN().WithImageMeta(metaFetcher(t, &calls, map[string]ImageMeta{
		photo:     {Width: 400, Height: 600, Thumbhash: "ph", Sexual: sexualPtr(0)},
		logo:      {Width: 200, Height: 80, Thumbhash: "lg", Sexual: sexualPtr(0)},
		charImage: {Width: 256, Height: 256, Thumbhash: "ci", Sexual: sexualPtr(1)},
	}))

	gotName, found, err := svc.Name(ctx, name.ID, false, EntityWorksGate{NSFW: false}, 50, 0)
	if err != nil || !found {
		t.Fatalf("name face: found=%v err=%v", found, err)
	}
	if gotName.PhotoMeta == nil || gotName.PhotoMeta.Thumbhash != "ph" {
		t.Fatalf("photo_meta = %+v, want the resolved entry", gotName.PhotoMeta)
	}
	if gotName.PhotoMeta.Sexual == nil || *gotName.PhotoMeta.Sexual != 0 {
		t.Fatalf("photo_meta.sexual = %v, want an explicit 0", gotName.PhotoMeta.Sexual)
	}

	gotLabel, found, err := svc.Label(ctx, labelID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("label face: found=%v err=%v", found, err)
	}
	if gotLabel.LogoMeta == nil || gotLabel.LogoMeta.Width != 200 {
		t.Fatalf("logo_meta = %+v, want the resolved entry", gotLabel.LogoMeta)
	}

	labels, err := svc.LabelsList(ctx, LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("labels list: %v", err)
	}
	if len(labels.Items) != 1 || labels.Items[0].LogoMeta == nil ||
		labels.Items[0].LogoMeta.Thumbhash != "lg" {
		t.Fatalf("labels list logo_meta = %+v", labels.Items)
	}

	graph, found, err := svc.LabelRelationGraph(ctx, labelID, false)
	if err != nil || !found {
		t.Fatalf("label graph: found=%v err=%v", found, err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].LogoMeta == nil {
		t.Fatalf("graph seed logo_meta = %+v", graph.Nodes)
	}

	gotChar, found, err := svc.Character(ctx, ch.ID, false, EntityWorksGate{NSFW: false}, 2, 50, 0)
	if err != nil || !found {
		t.Fatalf("character face: found=%v err=%v", found, err)
	}
	if gotChar.ImageMeta == nil || gotChar.ImageMeta.Sexual == nil || *gotChar.ImageMeta.Sexual != 1 {
		t.Fatalf("image_meta = %+v, want the resolved entry", gotChar.ImageMeta)
	}
	if gotChar.Figure == "" {
		t.Fatal("figure URL must render without its metadata")
	}
	if gotChar.FigureMeta != nil {
		t.Fatalf("figure_meta = %+v, want absent: the lookup did not answer for it", gotChar.FigureMeta)
	}
}

func TestEntityFacesServeWithoutTheImageService(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	photo := hash64("71")
	p := createPerson(t, "Unresolvable")
	if err := testDB.Model(p).Update("photo_hash", photo).Error; err != nil {
		t.Fatalf("set photo hash: %v", err)
	}
	name := createCreditName(t, &p.ID, "写真あり")

	labelID := createLabel(t, "No Meta Brand", model.LabelKindGameBrand)
	if err := testDB.Model(&model.CatalogLabel{}).Where("id = ?", labelID).
		Update("logo_hash", hash64("1091")).Error; err != nil {
		t.Fatalf("set logo hash: %v", err)
	}

	svc := newPublicSvcCDN()

	gotName, found, err := svc.Name(ctx, name.ID, false, EntityWorksGate{NSFW: false}, 50, 0)
	if err != nil || !found {
		t.Fatalf("name face: found=%v err=%v", found, err)
	}
	if gotName.PhotoHash != photo {
		t.Fatalf("photo_hash = %q, want the face to serve unchanged", gotName.PhotoHash)
	}
	if gotName.PhotoMeta != nil {
		t.Fatalf("photo_meta = %+v, want absent with no image client wired", gotName.PhotoMeta)
	}

	gotLabel, found, err := svc.Label(ctx, labelID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("label face: found=%v err=%v", found, err)
	}
	if gotLabel.LogoMeta != nil {
		t.Fatalf("logo_meta = %+v, want absent with no image client wired", gotLabel.LogoMeta)
	}
}
