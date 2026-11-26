package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createSourcedCredit(t *testing.T, workID, nameID, roleID int64, characterID *int64, source int16, note string) *model.CatalogCredit {
	t.Helper()
	c := &model.CatalogCredit{
		WorkID: workID, CreditNameID: nameID, RoleID: roleID, CharacterID: characterID,
		Spoiler: model.SpoilerNone, SourceID: &source, Note: note,
	}
	require.NoError(t, testDB.Create(c).Error)
	return c
}

func voiceNames(rows []WorkCharacterRow, characterID int64) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if r.CharacterID == characterID {
			for _, v := range r.Va {
				out[v.Name] = v.Note
			}
		}
	}
	return out
}

// Work 2452: VNDB credits 藤咲ウサ, Bangumi names the same actress 桜咲千依.
func TestBangumiVoiceYieldsToTheCreditedName(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	role := roleByKey(t, "voice-actor")
	work := createWork(t, "テトメト")

	person := &model.CatalogPerson{DisplayName: "桜咲千依"}
	require.NoError(t, testDB.Create(person).Error)
	credited := createCreditName(t, &person.ID, "藤咲ウサ")
	main := createCreditName(t, &person.ID, "桜咲千依")
	other := createCreditName(t, nil, "別名義")
	english := createCreditName(t, nil, "Y. Chang")
	japanese := createCreditName(t, nil, "杉田智和")
	pc := createCreditName(t, nil, "PC声優")
	ps2 := createCreditName(t, nil, "PS2声優")

	heroine := createCharacter(t, "ヒロイン")
	dubbed := createCharacter(t, "吹替キャラ")
	ported := createCharacter(t, "移植キャラ")
	for _, c := range []*model.CatalogCharacter{heroine, dubbed, ported} {
		createWorkCharacter(t, work.ID, c.ID, model.WorkCharacterKindMain, model.SpoilerNone)
	}

	createSourcedCredit(t, work.ID, credited.ID, role, &heroine.ID, model.SourceVNDB, "")
	createSourcedCredit(t, work.ID, main.ID, role, &heroine.ID, model.SourceBangumi, "主角")
	createSourcedCredit(t, work.ID, other.ID, role, &heroine.ID, model.SourceBangumi, "主角")

	createSourcedCredit(t, work.ID, english.ID, role, &dubbed.ID, model.SourceVNDB, "English")
	createSourcedCredit(t, work.ID, japanese.ID, role, &dubbed.ID, model.SourceBangumi, "配角")

	createSourcedCredit(t, work.ID, pc.ID, role, &ported.ID, model.SourceVNDB, "PC")
	createSourcedCredit(t, work.ID, ps2.ID, role, &ported.ID, model.SourceVNDB, "PS2")

	rows, err := read.loadWorkCharacters(ctx, work.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"藤咲ウサ": ""}, voiceNames(rows, heroine.ID), "every Bangumi voice yields to the credited one")
	assert.Equal(t, map[string]string{"Y. Chang": "English", "杉田智和": ""}, voiceNames(rows, dubbed.ID),
		"an English dub does not cover the original, and Bangumi's appearance kind is not a version note")
	assert.Equal(t, map[string]string{"PC声優": "PC", "PS2声優": "PS2"}, voiceNames(rows, ported.ID))

	cw, err := read.CharacterWorks(ctx, heroine.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	require.Len(t, cw.Works, 1)
	require.Len(t, cw.Works[0].Voices, 1)
	assert.Equal(t, credited.ID, cw.Works[0].Voices[0].CreditNameID)

	credits, err := read.WorkCredits(ctx, work.ID)
	require.NoError(t, err)
	assert.Len(t, credits, 5, "the two outvoiced Bangumi rows leave the credit list")

	nw, err := read.NameWorks(ctx, main.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, nw.Total, "the main name's credit list does not reach the work through the outvoiced row")
	assert.Empty(t, nw.Works)

	nw, err = read.NameWorks(ctx, japanese.ID, EntityWorksGate{NSFW: true, WithTotal: true}, 50, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, nw.Total)
}
