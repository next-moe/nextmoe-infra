package service

import (
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
	srcv "api/internal/platform/catalog/srcvndb"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestMintLinkGuardsEveryIdentityTemplate(t *testing.T) {
	cases := []struct {
		entity int16
		source string
		id     string
		want   string
	}{
		{model.EntityTypeWork, "vndb", "v17", "https://vndb.org/v17"},
		{model.EntityTypeWork, "vndb", "17", ""},
		{model.EntityTypeWork, "bangumi", "51", "https://bgm.tv/subject/51"},
		{model.EntityTypeWork, "erogamescape", "23956", "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game=23956"},
		{model.EntityTypeWork, "howlongtobeat", "68151", "https://howlongtobeat.com/game/68151"},
		{model.EntityTypeWork, "steam", "1917450", "https://store.steampowered.com/app/1917450"},
		{model.EntityTypeWork, "dlsite", "RJ01088277", ""},
		{model.EntityTypeWork, "curated", "12", ""},
		{model.EntityTypeCharacter, "vndb", "c88", "https://vndb.org/c88"},
		{model.EntityTypeCharacter, "vndb", "v88", ""},
		{model.EntityTypeCharacter, "bangumi", "12345", "https://bgm.tv/character/12345"},
		{model.EntityTypeCharacter, "erogamescape", "10251", "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/character.php?character=10251"},
		{model.EntityTypeLabel, "vndb", "p129", "https://vndb.org/p129"},
		{model.EntityTypeLabel, "bangumi", "7404", "https://bgm.tv/person/7404"},
		{model.EntityTypeLabel, "erogamescape", "1", "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/brand.php?brand=1"},
		{model.EntityTypeLabel, "dlsite", "RG01000107", ""},
		{model.EntityTypePerson, "vndb", "s131", "https://vndb.org/s131"},
		{model.EntityTypeCreditName, "vndb", "258", ""},
		{model.EntityTypeCreditName, "erogamescape", "6275", "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/creater.php?creater=6275"},
		{model.EntityTypeCreditName, "dlsite", "78444", ""},
	}
	for _, c := range cases {
		l, ok := mintLink(c.entity, linkAnchor{Source: c.source, ExternalID: c.id, LinkKind: model.LinkKindExact})
		if c.want == "" {
			require.False(t, ok, "%d %s %q must not mint, got %+v", c.entity, c.source, c.id, l)
			continue
		}
		require.True(t, ok, "%d %s %q", c.entity, c.source, c.id)
		require.Equal(t, dto.PublicLink{Source: c.source, URL: c.want, Identity: true, PageID: c.id}, l)
	}

	_, ok := mintLink(model.EntityTypeWork, linkAnchor{Source: "vndb", ExternalID: "v1", LinkKind: model.LinkKindProbable})
	require.False(t, ok, "a probable anchor is not an identity")
}

func TestRelatedLinkURLSkipsWhatItCannotAddress(t *testing.T) {
	cases := []struct{ source, id, want string }{
		{"web", "https://mill-soft.jp", "https://mill-soft.jp"},
		{"web", "mill-soft.jp", ""},
		{"official_site", "www.alcot.biz", "https://www.alcot.biz"},
		{"official_site", "http://example.jp/top.html", "http://example.jp/top.html"},
		{"official_site", "（已失效）http://www.green-bunny.com", ""},
		{"twitter", "alcot_official", "https://x.com/alcot_official"},
		{"twitter", "not a handle", ""},
		{"cien", "29601", "https://ci-en.dlsite.com/creator/29601"},
		{"steam", "1917450", "https://store.steampowered.com/app/1917450"},
		{"steam", "https://store.steampowered.com/app/1658930/_/?curator_clanid=38391122", "https://store.steampowered.com/app/1658930/_/?curator_clanid=38391122"},
		{"steam", "https://evil.example/app/1", ""},
		{"pixiv", "1338845", "https://www.pixiv.net/users/1338845"},
		{"pixiv", "http://www.pixiv.net/member.php?id=59388", "http://www.pixiv.net/member.php?id=59388"},
		{"dmm", "https://al.dmm.co.jp/?lurl=x&af_id=someone", ""},
		{"dlsite", "RJ01088277", ""},
	}
	for _, c := range cases {
		got, ok := relatedLinkURL(c.source, c.id)
		if c.want == "" {
			require.False(t, ok, "%s %q must be skipped, got %q", c.source, c.id, got)
			continue
		}
		require.True(t, ok, "%s %q", c.source, c.id)
		require.Equal(t, c.want, got)
	}
}

func TestOrderLinksKeepsTheMostSpecificOfOneAddress(t *testing.T) {
	in := []dto.PublicLink{
		{Source: "official_site", URL: "https://ci-en.dlsite.com/creator/65"},
		{Source: "twitter", URL: "https://x.com/brand"},
		{Source: "web", URL: "http://www.twitter.com/brand/"},
		{Source: "cien", URL: "https://ci-en.dlsite.com/creator/65"},
		{Source: "web", URL: "https://www.example.jp/"},
		{Source: "official_site", URL: "https://example.jp"},
		{Source: "vndb", URL: "https://vndb.org/p24", Identity: true, PageID: "p24"},
		{Source: "web", URL: "https://vndb.org/p24"},
	}
	require.Equal(t, []dto.PublicLink{
		{Source: "vndb", URL: "https://vndb.org/p24", Identity: true, PageID: "p24"},
		{Source: "twitter", URL: "https://x.com/brand"},
		{Source: "cien", URL: "https://ci-en.dlsite.com/creator/65"},
		{Source: "official_site", URL: "https://example.jp"},
	}, orderLinks(in))
}

func seedStaffAliases(t *testing.T, rows ...srcv.StaffAlias) {
	t.Helper()
	require.NoError(t, srcv.EnsureSchema(testDB))
	require.NoError(t, testDB.Exec(`TRUNCATE src_vndb.staff_alias`).Error)
	for _, r := range rows {
		require.NoError(t, testDB.Create(&r).Error)
	}
}

func TestCreditNameLinksPreferTheNameOwnAnchors(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	// Production: alias 258 is 水橋かおり, staff s131; s258 is Na-Ga.
	seedStaffAliases(t,
		srcv.StaffAlias{AID: 258, ID: "s131", Name: "水橋 かおり"},
		srcv.StaffAlias{AID: 430, ID: "s258", Name: "Na-Ga"},
		srcv.StaffAlias{AID: 259, ID: "s131", Name: "裏名義"},
	)
	pe := &model.CatalogPerson{DisplayName: "水橋かおり", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(pe).Error)
	addExternalRef(t, model.EntityTypePerson, pe.ID, srcVNDB, "s131", model.LinkKindExact)
	addExternalRef(t, model.EntityTypePerson, pe.ID, srcBangumiPub, "4423", model.LinkKindExact)
	addExternalRef(t, model.EntityTypePerson, pe.ID, int16(10), "kaori_m", model.LinkKindRelated)

	n := &model.CatalogCreditName{PersonID: &pe.ID, Name: "水橋かおり", Lang: "ja", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(n).Error)
	addExternalRef(t, model.EntityTypeCreditName, n.ID, srcVNDB, "258", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, n.ID, srcBangumiPub, "9999", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, n.ID, srcErogamescape, "6275", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, n.ID, int16(4), "78444", model.LinkKindExact)

	rec, found, err := svc.Name(ctx, n.ID, false, false, 50, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []dto.PublicLink{
		{Source: "vndb", URL: "https://vndb.org/s131", Identity: true, PageID: "s131"},
		{Source: "bangumi", URL: "https://bgm.tv/person/9999", Identity: true, PageID: "9999"},
		{Source: "erogamescape", URL: "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/creater.php?creater=6275", Identity: true, PageID: "6275"},
		{Source: "twitter", URL: "https://x.com/kaori_m"},
	}, rec.Links, "the alias resolves to its staff page, the name's own bangumi beats the person's, and the person's s131 collapses into the name's")

	bare := &model.CatalogCreditName{Name: "名無し", Lang: "ja", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(bare).Error)
	addExternalRef(t, model.EntityTypeCreditName, bare.ID, srcVNDB, "430", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, bare.ID, srcVNDB, "77777", model.LinkKindExact)
	rec, found, err = svc.Name(ctx, bare.ID, false, false, 50, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []dto.PublicLink{
		{Source: "vndb", URL: "https://vndb.org/s258", Identity: true, PageID: "s258"},
	}, rec.Links, "a name with no person still links its own anchors; an alias vndb does not know mints nothing")

	hidden := &model.CatalogCreditName{PersonID: &pe.ID, Name: "裏名義", Lang: "ja",
		LinkVisibility: model.LinkVisibilityHidden, FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, testDB.Create(hidden).Error)
	addExternalRef(t, model.EntityTypeCreditName, hidden.ID, srcVNDB, "259", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, hidden.ID, srcErogamescape, "35605", model.LinkKindExact)
	rec, found, err = svc.Name(ctx, hidden.ID, false, false, 50, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []dto.PublicLink{
		{Source: "erogamescape", URL: "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/creater.php?creater=35605", Identity: true, PageID: "35605"},
	}, rec.Links, "a hidden person link withholds the staff page and every person-level link; the name's own creater page stays")
}

func TestCharacterLinksOnDetailAndListLanes(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	ch := createCharacter(t, "真木 めぐる")
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcVNDB, "c88", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcBangumiPub, "12345", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCharacter, ch.ID, srcErogamescape, "10251", model.LinkKindExact)
	want := []dto.PublicLink{
		{Source: "vndb", URL: "https://vndb.org/c88", Identity: true, PageID: "c88"},
		{Source: "bangumi", URL: "https://bgm.tv/character/12345", Identity: true, PageID: "12345"},
		{Source: "erogamescape", URL: "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/character.php?character=10251", Identity: true, PageID: "10251"},
	}

	rec, found, err := svc.Character(ctx, ch.ID, false, false, 0, 0, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, rec.Links)

	bare := createCharacter(t, "無印")
	page, err := svc.CharactersList(ctx, []int64{ch.ID, bare.ID}, "", 10, CharacterListInclude{Links: true}, false, CharacterTraitFilter{}, false)
	require.NoError(t, err)
	got := map[int64][]dto.PublicLink{}
	for _, row := range page.Items {
		got[row.ID] = row.Links
	}
	require.Equal(t, want, got[ch.ID])
	require.NotNil(t, got[bare.ID], "no anchors is an empty block, not a missing one")
	require.Empty(t, got[bare.ID])
}
