package service

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func editContextRegistry(t *testing.T) *editing.Registry {
	t.Helper()
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, testDB))
	return reg
}

func TestEditContextCoversEveryListField(t *testing.T) {
	reg := editContextRegistry(t)
	registered := map[string]bool{}
	for _, typ := range reg.Types() {
		spec, _ := reg.Type(typ)
		for i := range spec.Fields {
			registered[spec.Fields[i].Key] = true
		}
	}
	for _, typ := range reg.Types() {
		spec, _ := reg.Type(typ)
		for i := range spec.Fields {
			f := spec.Fields[i]
			if f.Kind != editing.KindList || strings.HasSuffix(f.Key, editing.SuppressedFieldSuffix) {
				continue
			}
			assert.Truef(t, HasContextRows(f.Key) || SnapshotOnlyListField(f.Key),
				"%s is a list field with neither an entry in contextRowSources nor a reason in snapshotOnlyListFields", f.Key)
			if registered[editing.SuppressedFieldKey(f.Key)] {
				assert.Truef(t, ContextRowsPublishIdentity(f.Key),
					"%s has a .suppressed companion but its context rows publish no identity key", f.Key)
			}
		}
	}
	for key := range contextRowSources {
		assert.Truef(t, registered[key], "contextRowSources lists %s, which no type registers", key)
	}
	for key := range snapshotOnlyListFields {
		assert.Truef(t, registered[key], "snapshotOnlyListFields lists %s, which no type registers", key)
	}
}

func TestEditContextRowQueriesRunOnTheSchema(t *testing.T) {
	reg := editContextRegistry(t)
	svc := NewEditContextService(testDB)
	sources, err := svc.SourceKeys(t.Context())
	require.NoError(t, err)
	require.Equal(t, "vndb", sources[2])
	for _, typ := range reg.Types() {
		spec, _ := reg.Type(typ)
		for i := range spec.Fields {
			key := spec.Fields[i].Key
			if !HasContextRows(key) {
				continue
			}
			rows, truncated, err := svc.Rows(t.Context(), typ, key, -1, sources)
			require.NoErrorf(t, err, "%s", key)
			assert.Empty(t, rows, key)
			assert.False(t, truncated, key)
		}
	}
}

type contextFixture struct {
	work       *model.CatalogWork
	character  *model.CatalogCharacter
	label      int64
	person     *model.CatalogPerson
	creditName *model.CatalogCreditName
	upstream   *model.CatalogCredit
	curated    *model.CatalogCredit
}

func seedEditContext(t *testing.T) contextFixture {
	t.Helper()
	cleanTables(t)
	cleanTagTables(t)
	var f contextFixture
	f.work = createWork(t, "コンテキスト")
	f.character = createCharacter(t, "キャラ")
	role := roleByKey(t, "scenario")
	writer := createCreditName(t, nil, "脚本A")
	other := createCreditName(t, nil, "脚本B")
	f.upstream = createCredit(t, f.work.ID, writer.ID, role, nil)
	f.curated = createCredit(t, f.work.ID, other.ID, role, nil)
	require.NoError(t, testDB.Model(f.upstream).Update("source_id", 2).Error)
	require.NoError(t, testDB.Model(f.curated).Update("source_id", curatedSource).Error)

	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: f.work.ID, Lang: "ja", Title: "コンテキスト", Kind: model.WorkTitleKindOfficial,
	}).Error)
	tagID := createCanonicalTag(t, "純愛", 0, 0)
	require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{SourceID: 2, SourceName: "Pure Love", TagID: tagID}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{WorkID: f.work.ID, Name: "Pure Love", SourceID: 2}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{WorkID: f.work.ID, Name: "Unmapped", SourceID: 3}).Error)
	createWorkCharacter(t, f.work.ID, f.character.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	alias := createCharacterAlias(t, f.character.ID, "きゃら", "ja")
	require.NoError(t, testDB.Model(alias).Update("source_id", 2).Error)

	trait := createTrait(t, "i42", "眼鏡")
	require.NoError(t, testDB.Create(&model.CatalogCharacterTraitLink{
		CharacterID: f.character.ID, TraitID: trait.ID, SourceID: 2,
	}).Error)
	f.label = createLabel(t, "ブランド", 0)
	parent := createLabel(t, "親会社", 0)
	vndb := int16(2)
	require.NoError(t, testDB.Create(&model.CatalogLabelAlias{
		LabelID: f.label, Name: "Brand", Lang: "en", Kind: model.AliasKindSpellingVariant, SourceID: &vndb,
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogLabelRelation{
		LabelID: f.label, OtherLabelID: parent, Relation: model.LabelRelationSubsidiary, SourceID: vndb,
		MatchedBy: "import:test",
	}).Error)

	f.person = createPerson(t, "声優C")
	f.creditName = createCreditName(t, &f.person.ID, "声優C")
	addExternalRef(t, model.EntityTypePerson, f.person.ID, 2, "s123", model.LinkKindExact)
	addExternalRef(t, model.EntityTypePerson, f.person.ID, 16, "https://example.test/c", model.LinkKindRelated)
	nameAlias := createNameAlias(t, f.creditName.ID, "せいゆうC", "ja")
	require.NoError(t, testDB.Model(nameAlias).Update("source_id", 2).Error)
	return f
}

func identitiesOf(rows []ContextRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Identity != "" {
			out = append(out, r.Identity)
		}
	}
	return out
}

func TestEditContextIdentitiesAreKeysTheSuppressionFieldAccepts(t *testing.T) {
	f := seedEditContext(t)
	reg := editContextRegistry(t)
	svc := NewEditContextService(testDB)
	sources, err := svc.SourceKeys(t.Context())
	require.NoError(t, err)

	cases := []struct {
		entityType string
		field      string
		entityID   int64
		want       int
	}{
		{editspec.TypeWork, editspec.FieldWorkTitles, f.work.ID, 1},
		{editspec.TypeWork, editspec.FieldWorkTagIDs, f.work.ID, 1},
		{editspec.TypeWork, editspec.FieldWorkCredits, f.work.ID, 2},
		{editspec.TypeWork, editspec.FieldWorkRoster, f.work.ID, 1},
		{editspec.TypeCharacter, editspec.FieldCharacterAliases, f.character.ID, 1},
		{editspec.TypeCharacter, editspec.FieldCharacterTraits, f.character.ID, 1},
		{editspec.TypeLabel, editspec.FieldLabelAliases, f.label, 1},
		{editspec.TypeLabel, editspec.FieldLabelRelations, f.label, 1},
		{editspec.TypePerson, editspec.FieldPersonLinks, f.person.ID, 1},
		{editspec.TypeCreditName, editspec.FieldCreditNameAliases, f.creditName.ID, 1},
	}
	for _, c := range cases {
		rows, _, err := svc.Rows(t.Context(), c.entityType, c.field, c.entityID, sources)
		require.NoError(t, err, c.field)
		ids := identitiesOf(rows)
		require.Lenf(t, ids, c.want, "%s identities", c.field)
		spec, _ := reg.Type(c.entityType)
		companion, ok := spec.Field(editing.SuppressedFieldKey(c.field))
		require.True(t, ok, c.field)
		values := make([]any, len(ids))
		for i, id := range ids {
			values[i] = id
		}
		assert.NoErrorf(t, companion.Validate(values), "%s: the face published keys its own suppression field refuses", c.field)
	}

	rows, _, err := svc.Rows(t.Context(), editspec.TypeWork, editspec.FieldWorkTagIDs, f.work.ID, sources)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		if strings.Contains(string(r.Value), "Unmapped") {
			assert.Empty(t, r.Identity, "an upstream tag with no canonical tag cannot be keyed")
			assert.Equal(t, "bangumi", r.SourceKey)
		}
	}
}

func TestEditContextIdentityRoundTripHidesTheRow(t *testing.T) {
	f := seedEditContext(t)
	reg := editContextRegistry(t)
	svc := NewEditContextService(testDB)
	read := NewReadService(testDB)
	ctx := t.Context()
	sources, err := svc.SourceKeys(ctx)
	require.NoError(t, err)

	rows, _, err := svc.Rows(ctx, editspec.TypeWork, editspec.FieldWorkCredits, f.work.ID, sources)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var upstream ContextRow
	for _, r := range rows {
		if r.SourceKey == "vndb" {
			upstream = r
		} else {
			assert.True(t, r.HumanLane, "the curated credit is the human lane")
		}
	}
	require.Equal(t, editspec.CreditIdentity(f.upstream.RoleID, f.upstream.CreditNameID, 0), upstream.Identity)
	assert.False(t, upstream.HumanLane)
	assert.False(t, upstream.Suppressed)

	spec, _ := reg.Type(editspec.TypeWork)
	companion, _ := spec.Field(editing.SuppressedFieldKey(editspec.FieldWorkCredits))
	require.NoError(t, testDB.Transaction(func(tx *gorm.DB) error {
		return companion.Apply(ctx, tx, f.work.ID, []any{upstream.Identity})
	}))

	credits, err := read.WorkCredits(ctx, f.work.ID)
	require.NoError(t, err)
	require.Len(t, credits, 1, "the suppressed upstream credit leaves the read face")
	assert.NotEqual(t, upstream.Identity, credits[0].Identity)

	rows, _, err = svc.Rows(ctx, editspec.TypeWork, editspec.FieldWorkCredits, f.work.ID, sources)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, r.Identity == upstream.Identity, r.Suppressed)
	}

	require.NoError(t, testDB.Delete(f.upstream).Error)
	rows, _, err = svc.Rows(ctx, editspec.TypeWork, editspec.FieldWorkCredits, f.work.ID, sources)
	require.NoError(t, err)
	require.Len(t, rows, 2, "a suppression whose row is gone is still listed")
	orphan := rows[len(rows)-1]
	assert.Equal(t, upstream.Identity, orphan.Identity)
	assert.True(t, orphan.Suppressed)
	assert.False(t, orphan.Present)
	assert.Empty(t, orphan.SourceKey)
	assert.Nil(t, orphan.Value)
}

func TestEditContextHeadProvenance(t *testing.T) {
	f := seedEditContext(t)
	svc := NewEditContextService(testDB)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET field_provenance = ?::jsonb WHERE id = ?`,
		`{"olang":[{"source":"curated","at":"2026-10-19T00:00:00Z"},{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}`,
		f.work.ID).Error)
	head, err := svc.HeadProvenance(t.Context(), "catalog_work", f.work.ID)
	require.NoError(t, err)
	require.Len(t, head["olang"], 2)
	assert.Equal(t, "curated", head["olang"][0].Source)
	assert.Empty(t, head["display_name"])

	missing, err := svc.HeadProvenance(t.Context(), "catalog_work", f.work.ID+1000)
	require.NoError(t, err)
	assert.Empty(t, missing)
}
