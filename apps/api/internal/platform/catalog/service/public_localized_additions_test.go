package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"api/internal/platform/catalog/model"
)

func addLabelAlias(t *testing.T, labelID int64, name, lang string, kind, provenance int16) {
	t.Helper()
	if err := testDB.Create(&model.CatalogLabelAlias{
		LabelID: labelID, Name: name, Lang: lang, Kind: kind, Provenance: provenance,
	}).Error; err != nil {
		t.Fatalf("create label alias %s/%s: %v", lang, name, err)
	}
}

func TestPublicLabelsListCarriesAliases(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	withAliases := createLabel(t, "ねこねこソフト", model.LabelKindGameBrand)
	bare := createLabel(t, "ＫＩＤ", model.LabelKindGameBrand)
	addLabelAlias(t, withAliases, "猫猫社", "zh-Hans", model.AliasKindTranslation, model.AliasProvenanceSource)
	addLabelAlias(t, withAliases, "NekoNeko Soft", "en", model.AliasKindSpellingVariant, model.AliasProvenanceSource)
	addLabelAlias(t, withAliases, "ねこねこソフト", "ja", model.AliasKindSpellingVariant, model.AliasProvenanceSource)
	addLabelAlias(t, withAliases, "nekoneko-hint", "", model.AliasKindSearchHint, model.AliasProvenanceSource)

	page, err := svc.LabelsList(t.Context(), LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList: %v", err)
	}
	items := map[int64]int{}
	for i, it := range page.Items {
		items[it.ID] = i
	}
	rich := page.Items[items[withAliases]]
	detail, found, err := svc.Label(t.Context(), withAliases, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("Label detail: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(rich.Aliases, detail.Aliases) {
		t.Fatalf("list aliases = %+v, detail aliases = %+v — the two faces must agree", rich.Aliases, detail.Aliases)
	}
	if len(rich.Aliases) != 2 {
		t.Fatalf("aliases = %+v, want 2 (display_name row and the search hint both dropped)", rich.Aliases)
	}
	if rich.Localized["zh-Hans"].Value != "猫猫社" {
		t.Fatalf("list localized = %+v, want the zh-Hans alias still elected", rich.Localized)
	}

	empty := page.Items[items[bare]]
	if empty.Aliases == nil || len(empty.Aliases) != 0 {
		t.Fatalf("aliasless list row = %#v, want an allocated empty slice so the face emits []", empty.Aliases)
	}
	raw, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if aliases, present := wire["aliases"]; !present {
		t.Fatalf("list row has no aliases key: %s", raw)
	} else if arr, ok := aliases.([]any); !ok || len(arr) != 0 {
		t.Fatalf("aliasless list row aliases = %#v, want []", aliases)
	}
}

func TestPublicCharacterTraitLocalized(t *testing.T) {
	cleanTables(t)
	if err := testDB.Exec("TRUNCATE catalog_character_trait RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate trait vocab: %v", err)
	}
	svc := newPublicSvc()

	ch := createCharacter(t, "テスト嬢")
	mkTrait := func(tid, gid, name, nameZh string, provenance int16) int64 {
		tr := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, NameZh: nameZh, NameZhProvenance: provenance,
			GroupTID: gid, Searchable: true, Applicable: true,
		}
		if err := testDB.Create(tr).Error; err != nil {
			t.Fatalf("create trait %s: %v", name, err)
		}
		return tr.ID
	}
	link := func(traitID int64) {
		if err := testDB.Create(&model.CatalogCharacterTraitLink{
			CharacterID: ch.ID, TraitID: traitID, SpoilerLevel: 0,
		}).Error; err != nil {
			t.Fatalf("link trait: %v", err)
		}
	}
	mkTrait("i0", "", "Hair", "毛发", model.TraitNameZhProvenanceCurated)
	link(mkTrait("i1", "i0", "Long Hair", "长发", model.TraitNameZhProvenanceCurated))
	link(mkTrait("i2", "i0", "Braided Hair", "编发", model.TraitNameZhProvenanceMachine))
	link(mkTrait("i3", "", "Hidden Past", "", model.TraitNameZhProvenanceCurated))

	rec, found, err := svc.Character(t.Context(), ch.ID, false, EntityWorksGate{NSFW: false}, 0, 50, 0)
	if err != nil || !found {
		t.Fatalf("character: found=%v err=%v", found, err)
	}
	byName := map[string]int{}
	for i, tr := range rec.Traits {
		byName[tr.Name] = i
	}
	if len(rec.Traits) != 3 {
		t.Fatalf("traits = %+v, want 3", rec.Traits)
	}

	curated := rec.Traits[byName["Long Hair"]]
	if curated.Localized["zh-Hans"].Value != "长发" || curated.Localized["zh-Hans"].Machine {
		t.Fatalf("curated trait localized = %+v, want 长发 unflagged", curated.Localized)
	}
	if curated.Localized["zh-Hans"].Kind != "translation" {
		t.Fatalf("trait localized kind = %q, want translation", curated.Localized["zh-Hans"].Kind)
	}
	if curated.GroupLocalized["zh-Hans"].Value != "毛发" || curated.GroupLocalized["zh-Hans"].Machine {
		t.Fatalf("group localized = %+v, want 毛发 unflagged", curated.GroupLocalized)
	}
	machine := rec.Traits[byName["Braided Hair"]]
	if machine.Localized["zh-Hans"].Value != "编发" || !machine.Localized["zh-Hans"].Machine {
		t.Fatalf("machine trait localized = %+v, want 编发 flagged machine=true", machine.Localized)
	}
	if machine.GroupLocalized["zh-Hans"].Machine {
		t.Fatalf("group localized = %+v, the group's own provenance is curated", machine.GroupLocalized)
	}

	bare := rec.Traits[byName["Hidden Past"]]
	if bare.Localized == nil || len(bare.Localized) != 0 {
		t.Fatalf("trait without a zh name = %#v, want an empty map", bare.Localized)
	}
	if bare.GroupLocalized == nil || len(bare.GroupLocalized) != 0 {
		t.Fatalf("root trait group_localized = %#v, want an empty map", bare.GroupLocalized)
	}
	raw, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal trait: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal trait: %v", err)
	}
	for _, key := range []string{"localized", "group_localized"} {
		m, ok := wire[key].(map[string]any)
		if !ok || len(m) != 0 {
			t.Fatalf("trait %s = %#v, want {}", key, wire[key])
		}
	}
}
