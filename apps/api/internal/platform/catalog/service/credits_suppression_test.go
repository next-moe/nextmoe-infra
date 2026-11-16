package service

import (
	"reflect"
	"strings"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var curatedSource int16 = 12

func jsonKeysOf(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	out := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}

func roleByKey(t *testing.T, key string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_role WHERE key = ?`, key).Scan(&id).Error)
	require.NotZerof(t, id, "seeded role %q", key)
	return id
}

func suppressCredit(t *testing.T, workID int64, c *model.CatalogCredit) {
	t.Helper()
	var charID int64
	if c.CharacterID != nil {
		charID = *c.CharacterID
	}
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: workID, FieldKey: editspec.FieldWorkCredits,
		IdentityKey: editspec.CreditIdentity(c.RoleID, c.CreditNameID, charID),
	}).Error)
}

type creditReadFixture struct {
	work, other      *model.CatalogWork
	va, writer       *model.CatalogCreditName
	rosterCh, onlyCh *model.CatalogCharacter
	roleVA, roleScen int64
	badVA, goodVA    *model.CatalogCredit
	onlyCredit       *model.CatalogCredit
	scenCredit       *model.CatalogCredit
	otherScen        *model.CatalogCredit
}

// seedCreditReads builds one work whose credits cover every shape the read
// sites branch on: a roster character with two VAs, a credit-only character,
// and a role with no character at all.
func seedCreditReads(t *testing.T) creditReadFixture {
	t.Helper()
	cleanTables(t)
	var f creditReadFixture
	f.work = createWork(t, "作品1")
	f.other = createWork(t, "作品2")
	f.roleVA, f.roleScen = roleByKey(t, "voice-actor"), roleByKey(t, "scenario")
	f.va = createCreditName(t, nil, "声優A")
	f.writer = createCreditName(t, nil, "脚本B")
	f.rosterCh = createCharacter(t, "花名册キャラ")
	f.onlyCh = createCharacter(t, "credit だけキャラ")
	createWorkCharacter(t, f.work.ID, f.rosterCh.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	f.badVA = createCredit(t, f.work.ID, f.va.ID, f.roleVA, &f.rosterCh.ID)
	f.goodVA = createCredit(t, f.work.ID, f.writer.ID, f.roleVA, &f.rosterCh.ID)
	f.onlyCredit = createCredit(t, f.work.ID, f.va.ID, f.roleVA, &f.onlyCh.ID)
	f.scenCredit = createCredit(t, f.work.ID, f.va.ID, f.roleScen, nil)
	f.otherScen = createCredit(t, f.other.ID, f.va.ID, f.roleScen, nil)
	return f
}

func TestCreditsSuppressionExcludedOnEveryReadSite(t *testing.T) {
	f := seedCreditReads(t)
	read := NewReadService(testDB)
	pub := newPublicSvc()
	ctx := t.Context()

	t.Run("WorkCredits", func(t *testing.T) {
		rows, err := read.WorkCredits(ctx, f.work.ID)
		require.NoError(t, err)
		require.Len(t, rows, 4)
		suppressCredit(t, f.work.ID, f.badVA)
		rows, err = read.WorkCredits(ctx, f.work.ID)
		require.NoError(t, err)
		require.Len(t, rows, 3)
		for _, r := range rows {
			assert.NotEqual(t, editspec.CreditIdentity(f.badVA.RoleID, f.badVA.CreditNameID, f.rosterCh.ID), r.Identity)
		}
	})

	t.Run("PublicWorkCredits", func(t *testing.T) {
		rec, found, err := pub.WorkDetail(ctx, f.work.ID, PublicInclude{Credits: true}, true, 0, PublicFields{})
		require.NoError(t, err)
		require.True(t, found)
		total := 0
		for _, g := range rec.Credits {
			total += len(g.Credits)
		}
		assert.Equal(t, 3, total, "the public projection reads the same rows")
	})

	t.Run("WorkCharactersVA", func(t *testing.T) {
		chars, err := read.loadWorkCharacters(ctx, f.work.ID)
		require.NoError(t, err)
		for _, c := range chars {
			if c.CharacterID != f.rosterCh.ID {
				continue
			}
			require.Len(t, c.Va, 1, "the suppressed VA must leave the roster's va[]")
			assert.Equal(t, f.writer.ID, c.Va[0].CreditNameID)
		}
	})

	t.Run("NameWorksTotalAndItemsAgree", func(t *testing.T) {
		// The VA still holds the credit-only role and the scenario role on this
		// work, so nothing drops yet; suppress those too and the work leaves.
		res, err := read.NameWorks(ctx, f.va.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
		require.NoError(t, err)
		assert.EqualValues(t, 2, res.Total)
		require.Len(t, res.Works, 2)

		suppressCredit(t, f.work.ID, f.onlyCredit)
		suppressCredit(t, f.work.ID, f.scenCredit)
		res, err = read.NameWorks(ctx, f.va.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
		require.NoError(t, err)
		assert.EqualValues(t, 1, res.Total, "the total must not count a work whose every credit is suppressed")
		require.Len(t, res.Works, 1, "and the page must agree with the total")
		assert.Equal(t, f.other.ID, res.Works[0].Brief.WorkID)
	})

	t.Run("NameWorksRoles", func(t *testing.T) {
		// Restore the scenario credit on work 1 so the work is listed again but
		// one of its two roles is suppressed.
		require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ? AND field_key = ? AND identity_key = ?",
			editspec.TypeWork, f.work.ID, editspec.FieldWorkCredits,
			editspec.CreditIdentity(f.scenCredit.RoleID, f.scenCredit.CreditNameID, 0)).
			Delete(&editing.SuppressedRow{}).Error)
		res, err := read.NameWorks(ctx, f.va.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
		require.NoError(t, err)
		assert.EqualValues(t, 2, res.Total)
		for _, w := range res.Works {
			if w.Brief.WorkID != f.work.ID {
				continue
			}
			require.Len(t, w.Roles, 1, "the suppressed VA role must not be listed")
			assert.Equal(t, f.roleScen, w.Roles[0].RoleID)
		}
	})

	t.Run("PublicNameCredits", func(t *testing.T) {
		p, found, err := pub.Name(ctx, f.va.ID, true, EntityWorksGate{NSFW: true}, 50, 0)
		require.NoError(t, err)
		require.True(t, found)
		for _, c := range p.Credits {
			if c.Work.ID != f.work.ID {
				continue
			}
			require.Len(t, c.Roles, 1)
			assert.Equal(t, "scenario", c.Roles[0].RoleKey)
		}
	})

	t.Run("CharacterWorksTotalItemsAndVoices", func(t *testing.T) {
		// rosterCh keeps its roster edge, so the work stays; its voices drop the
		// suppressed one.
		res, err := read.CharacterWorks(ctx, f.rosterCh.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
		require.NoError(t, err)
		assert.EqualValues(t, 1, res.Total)
		require.Len(t, res.Works, 1)
		require.Len(t, res.Works[0].Voices, 1)
		assert.Equal(t, f.writer.ID, res.Works[0].Voices[0].CreditNameID)

		// onlyCh has no roster edge, so suppressing its only credit removes the
		// work from both halves of the union.
		res, err = read.CharacterWorks(ctx, f.onlyCh.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
		require.NoError(t, err)
		assert.EqualValues(t, 0, res.Total, "the union total must drop the credit-only work")
		assert.Empty(t, res.Works, "and the page must agree with the total")
	})
}

func TestSuppressedVACreditLeavesRosterUnion(t *testing.T) {
	f := seedCreditReads(t)
	read := NewReadService(testDB)
	ctx := t.Context()

	before, err := read.loadWorkCharacters(ctx, f.work.ID)
	require.NoError(t, err)
	require.Len(t, before, 2, "roster edge + credit-only character")

	suppressCredit(t, f.work.ID, f.badVA)
	after, err := read.loadWorkCharacters(ctx, f.work.ID)
	require.NoError(t, err)
	require.Len(t, after, 2, "suppressing one of two VAs must not remove the character")
	byID := map[int64]WorkCharacterRow{}
	for _, c := range after {
		byID[c.CharacterID] = c
	}
	roster := byID[f.rosterCh.ID]
	require.Len(t, roster.Va, 1, "va[] loses exactly the suppressed credit")
	assert.Equal(t, f.writer.ID, roster.Va[0].CreditNameID)
	assert.Equal(t, model.WorkCharacterKindMain, roster.Kind, "the roster edge itself is untouched")

	// The credit-only character exists on this work solely through its credit,
	// so suppressing it removes the character from the union entirely. That is
	// charter ruling 2 applied without an exemption, and it is a documented cost.
	suppressCredit(t, f.work.ID, f.onlyCredit)
	after, err = read.loadWorkCharacters(ctx, f.work.ID)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, f.rosterCh.ID, after[0].CharacterID)
}

func TestWorkCreditsOrdersCuratedFirst(t *testing.T) {
	cleanTables(t)
	work := createWork(t, "並び順")
	role := roleByKey(t, "scenario")
	bangumi, dlsite := int16(3), int16(4)

	mk := func(name string, source *int16) int64 {
		cn := createCreditName(t, nil, name)
		c := createCredit(t, work.ID, cn.ID, role, nil)
		if source != nil {
			require.NoError(t, testDB.Model(c).Update("source_id", *source).Error)
		}
		return cn.ID
	}
	// bangumi < curated < dlsite lexicographically, so before the human-lane
	// term the curated row collated between the two importers.
	bgmName := mk("bangumi 署名", &bangumi)
	curatedName := mk("人手署名", &curatedSource)
	dlName := mk("dlsite 署名", &dlsite)

	rows, err := NewReadService(testDB).WorkCredits(t.Context(), work.ID)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, curatedName, rows[0].CreditNameID, "the human lane leads its role group")
	assert.Equal(t, bgmName, rows[1].CreditNameID)
	assert.Equal(t, dlName, rows[2].CreditNameID)
}

func TestIdentityRoundTripsToExactlyOneCreditRow(t *testing.T) {
	f := seedCreditReads(t)
	read := NewReadService(testDB)
	pub := newPublicSvc()
	ctx := t.Context()

	assertOneRow := func(t *testing.T, workID int64, identity string) {
		t.Helper()
		require.NotEmpty(t, identity, "every 1:1 face must carry an identity")
		var n int64
		require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_credit c
			WHERE c.work_id = ? AND `+editspec.CreditIdentitySQL("c")+` = ?`, workID, identity).
			Scan(&n).Error)
		assert.EqualValuesf(t, 1, n, "identity %q resolved to %d rows", identity, n)
	}

	rows, err := read.WorkCredits(ctx, f.work.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, r := range rows {
		assertOneRow(t, f.work.ID, r.Identity)
	}

	rec, _, err := pub.WorkDetail(ctx, f.work.ID, PublicInclude{Credits: true}, true, 0, PublicFields{})
	require.NoError(t, err)
	for _, g := range rec.Credits {
		for _, c := range g.Credits {
			assertOneRow(t, f.work.ID, c.Identity)
		}
	}

	nw, err := read.NameWorks(ctx, f.va.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, nw.Works)
	for _, w := range nw.Works {
		for _, r := range w.Roles {
			assertOneRow(t, w.Brief.WorkID, r.Identity)
		}
	}
	p, _, err := pub.Name(ctx, f.va.ID, true, EntityWorksGate{NSFW: true}, 50, 0)
	require.NoError(t, err)
	for _, c := range p.Credits {
		for _, r := range c.Roles {
			assertOneRow(t, c.Work.ID, r.Identity)
		}
	}

	// The collapsing faces must not grow one. A DISTINCT row there stands for
	// 1..N credit rows, so a single value would be a lie and an array would let
	// one click suppress rows the reader never saw.
	for _, typ := range []any{dto.WorkCharacterVA{}, dto.VoiceName{}, dto.PublicRosterVoice{}, dto.PublicVoiceName{}} {
		assert.NotContainsf(t, jsonKeysOf(t, typ), "identity",
			"%T is a DISTINCT collapse of 1..N credit rows and must not carry an identity", typ)
	}
}
