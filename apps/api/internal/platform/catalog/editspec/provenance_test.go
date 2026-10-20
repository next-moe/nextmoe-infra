package editspec_test

import (
	"encoding/json"
	"sort"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/datatypes"
)

func workProv(t *testing.T, workID int64) datatypes.JSON {
	t.Helper()
	var w model.CatalogWork
	if err := testDB.Select("id", "field_provenance").First(&w, workID).Error; err != nil {
		t.Fatalf("reload work %d: %v", workID, err)
	}
	return w.FieldProvenance
}

func labelProv(t *testing.T, labelID int64) datatypes.JSON {
	t.Helper()
	var l model.CatalogLabel
	if err := testDB.Select("id", "field_provenance").First(&l, labelID).Error; err != nil {
		t.Fatalf("reload label %d: %v", labelID, err)
	}
	return l.FieldProvenance
}

func TestEngineStampsCuratedProvenance(t *testing.T) {
	e := newTaxonomyEngine(t)
	work := createWork(t, "初期名")

	if got := provenance.FirstSource(workProv(t, work.ID), "display_name"); got != "" {
		t.Fatalf("a fresh work must carry no stamp, got %q", got)
	}

	mergeOn(t, e, editspec.TypeWork, work.ID, map[string]any{
		editspec.FieldWorkDisplayName:   "正式名",
		editspec.FieldWorkOLang:         "en",
		editspec.FieldWorkContentRating: float64(model.ContentRatingR18),
		editspec.FieldWorkDisplayNSFW:   true,
	})

	prov := workProv(t, work.ID)
	for _, column := range []string{"display_name", "olang", "content_rating", "display_nsfw"} {
		if got := provenance.FirstSource(prov, column); got != provenance.SourceCurated {
			t.Errorf("catalog_work.field_provenance[%s] head = %q, want %q", column, got, provenance.SourceCurated)
		}
	}

	label := model.CatalogLabel{DisplayName: "旧ブランド", Kind: model.LabelKindDoujinCircle}
	if err := testDB.Create(&label).Error; err != nil {
		t.Fatal(err)
	}
	mergeOn(t, e, editspec.TypeLabel, label.ID, map[string]any{editspec.FieldLabelName: "新ブランド"})
	if got := provenance.FirstSource(labelProv(t, label.ID), "display_name"); got != provenance.SourceCurated {
		t.Errorf("catalog_label.field_provenance[display_name] head = %q, want %q", got, provenance.SourceCurated)
	}
}

func TestProvenanceStampGoesToTheArrayHead(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "importer name")
	if err := testDB.Exec(
		`UPDATE catalog_work SET field_provenance =
		 '{"display_name":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		work.ID).Error; err != nil {
		t.Fatal(err)
	}

	mergeField(t, e, work.ID, editspec.FieldWorkDisplayName, "human name")

	var doc map[string][]provenance.Entry
	if err := json.Unmarshal(workProv(t, work.ID), &doc); err != nil {
		t.Fatal(err)
	}
	entries := doc["display_name"]
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want the curated stamp unshifted onto the vndb one", entries)
	}
	if entries[0].Source != provenance.SourceCurated || entries[1].Source != "vndb" {
		t.Fatalf("order = %+v, want curated first", entries)
	}
	if entries[0].At == "" {
		t.Error("the stamp must carry a timestamp")
	}
}

// The list is the contract: a field that stamps a field_provenance column must
// not be one of doc 21 §8.2's machine-owned fields (anchors, external rating
// snapshots, derived/rollup columns, machine image-grade rows). Adding a target
// here is a deliberate act, so the test names every one of them. Since R2c-2 a
// target may name a CHILD table (catalog_work_character), in which case it also
// carries a Rows resolver — the entity id is not that table's primary key.
func TestOnlyDeclaredFieldsStampProvenance(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterWork(reg, testDB); err != nil {
		t.Fatalf("register catalog.work: %v", err)
	}
	if err := editspec.RegisterTaxonomy(reg, testDB); err != nil {
		t.Fatalf("register taxonomy: %v", err)
	}
	if err := editspec.RegisterCharacter(reg, testDB); err != nil {
		t.Fatalf("register catalog.character: %v", err)
	}
	if err := editspec.RegisterTrait(reg, testDB); err != nil {
		t.Fatalf("register catalog.trait: %v", err)
	}

	var got []string
	for _, entityType := range []string{
		editspec.TypeWork, editspec.TypeLabel, editspec.TypeTag, editspec.TypeEngine, editspec.TypeSeries,
		editspec.TypeCharacter, editspec.TypeTrait,
	} {
		spec, ok := reg.Type(entityType)
		if !ok {
			t.Fatalf("%s is not registered", entityType)
		}
		for i := range spec.Fields {
			f := &spec.Fields[i]
			for _, target := range f.Provenance {
				got = append(got, f.Key+" -> "+target.Table+"."+target.Column)
			}
		}
	}
	sort.Strings(got)

	want := []string{
		"catalog.character.birthday_day -> catalog_character.birthday_day",
		"catalog.character.birthday_month -> catalog_character.birthday_month",
		"catalog.character.blood_type -> catalog_character.blood_type",
		"catalog.character.bust_cm -> catalog_character.bust_cm",
		"catalog.character.cup -> catalog_character.cup",
		"catalog.character.description -> catalog_character.description",
		"catalog.character.display_name -> catalog_character.display_name",
		"catalog.character.figure_hash -> catalog_character.figure_hash",
		"catalog.character.gender -> catalog_character.gender",
		"catalog.character.height_cm -> catalog_character.height_cm",
		"catalog.character.hip_cm -> catalog_character.hip_cm",
		"catalog.character.image_hash -> catalog_character.image_hash",
		"catalog.character.instance_of -> catalog_character.instance_of",
		"catalog.character.lang -> catalog_character.lang",
		"catalog.character.latin -> catalog_character.latin",
		"catalog.character.waist_cm -> catalog_character.waist_cm",
		"catalog.character.weight_kg -> catalog_character.weight_kg",
		"catalog.label.kind -> catalog_label.kind",
		"catalog.label.lang -> catalog_label.lang",
		"catalog.label.latin -> catalog_label.latin",
		"catalog.label.logo_hash -> catalog_label.logo_hash",
		"catalog.label.name -> catalog_label.display_name",
		"catalog.label.note -> catalog_label.note",
		"catalog.tag.kind -> catalog_tag.kind",
		"catalog.tag.name -> catalog_tag.name",
		"catalog.tag.note -> catalog_tag.note",
		"catalog.tag.sexual -> catalog_tag.sexual",
		"catalog.tag.tier -> catalog_tag.tier",
		"catalog.trait.aliases -> catalog_character_trait.alias",
		"catalog.trait.applicable -> catalog_character_trait.applicable",
		"catalog.trait.default_spoiler -> catalog_character_trait.default_spoil",
		"catalog.trait.description -> catalog_character_trait.description",
		"catalog.trait.description_zh -> catalog_character_trait.description_zh",
		"catalog.trait.group_id -> catalog_character_trait.group_tid",
		"catalog.trait.group_order -> catalog_character_trait.gorder",
		"catalog.trait.name -> catalog_character_trait.name",
		"catalog.trait.name_zh -> catalog_character_trait.name_zh",
		"catalog.trait.searchable -> catalog_character_trait.searchable",
		"catalog.trait.sexual -> catalog_character_trait.sexual",
		"catalog.work.content_rating -> catalog_work.content_rating",
		"catalog.work.covers_upstream -> catalog_work_cover.kind",
		"catalog.work.covers_upstream -> catalog_work_cover.portrait_pinned",
		"catalog.work.covers_upstream -> catalog_work_cover.sexual",
		"catalog.work.covers_upstream -> catalog_work_cover.violence",
		"catalog.work.display_name -> catalog_work.display_name",
		"catalog.work.display_nsfw -> catalog_work.display_nsfw",
		"catalog.work.olang -> catalog_work.olang",
		"catalog.work.roster -> catalog_work_character.kind",
		"catalog.work.roster -> catalog_work_character.spoiler",
		"catalog.work.screenshots_upstream -> catalog_work_screenshot.caption",
		"catalog.work.screenshots_upstream -> catalog_work_screenshot.sexual",
		"catalog.work.screenshots_upstream -> catalog_work_screenshot.violence",
	}
	if len(got) != len(want) {
		t.Fatalf("stamping fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stamping field %d = %q, want %q", i, got[i], want[i])
		}
	}
}
