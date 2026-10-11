package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func personCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Model(&model.CatalogPerson{}).Count(&n).Error)
	return n
}

func TestPersonLinkJoinsThePersonHoldingTheSpellingOnASharedWork(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)
	w := createWork(t, "W")

	host := createPerson(t, "木村 貴宏")
	held := createCreditName(t, &host.ID, "木村 貴宏")
	createCredit(t, w.ID, held.ID, role, nil)

	eg := createCreditName(t, nil, "木村貴宏（すがわらあわじ、すがわらあわぢ）")
	createCredit(t, w.ID, eg.ID, role, nil)
	alias := createCreditName(t, nil, "すがわらあわぢ")
	giveCredits(t, alias.ID, role, 1)
	mkCreditCandidate(t, eg.ID, alias.ID)

	before := personCount(t)
	link := acceptLink(t, eg.ID, alias.ID)
	assert.False(t, link.Created)
	assert.False(t, link.NeedsManual)
	assert.Equal(t, host.ID, link.PersonID)
	assert.Equal(t, before, personCount(t), "no person is minted next to the one already credited there")
	assert.Equal(t, host.ID, *personIDOfName(t, eg.ID))
	assert.Equal(t, host.ID, *personIDOfName(t, alias.ID))
}

func TestPersonLinkTwoHoldersOfTheSpellingNeedsManual(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)
	w1, w2 := createWork(t, "W1"), createWork(t, "W2")

	p1, p2 := createPerson(t, "P1"), createPerson(t, "P2")
	createCredit(t, w1.ID, createCreditName(t, &p1.ID, "Yuki").ID, role, nil)
	createCredit(t, w2.ID, createCreditName(t, &p2.ID, "yuki").ID, role, nil)

	a := createCreditName(t, nil, "YUKI")
	createCredit(t, w1.ID, a.ID, role, nil)
	b := createCreditName(t, nil, "Yu ki")
	createCredit(t, w2.ID, b.ID, role, nil)
	mkCreditCandidate(t, a.ID, b.ID)

	link := acceptLink(t, a.ID, b.ID)
	assert.True(t, link.NeedsManual)
	assert.Nil(t, personIDOfName(t, a.ID))
	assert.Nil(t, personIDOfName(t, b.ID))
}

func TestPersonLinkStillMintsWhenTheSpellingIsHeldOnlyElsewhere(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)

	other := createPerson(t, "同名")
	giveCredits(t, createCreditName(t, &other.ID, "同名").ID, role, 1)

	a := createCreditName(t, nil, "同名")
	giveCredits(t, a.ID, role, 1)
	b := createCreditName(t, nil, "どうめい")
	giveCredits(t, b.ID, role, 1)
	mkCreditCandidate(t, a.ID, b.ID)

	link := acceptLink(t, a.ID, b.ID)
	assert.True(t, link.Created)
	assert.NotEqual(t, other.ID, link.PersonID)
}

func TestPersonLinkStillMintsOnOneWorkUnderAShortFold(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)
	w := createWork(t, "W")

	host := createPerson(t, "ハム")
	createCredit(t, w.ID, createCreditName(t, &host.ID, "ハム").ID, role, nil)
	a := createCreditName(t, nil, "ハム(音楽)")
	createCredit(t, w.ID, a.ID, role, nil)
	b := createCreditName(t, nil, "はむ")
	giveCredits(t, b.ID, role, 1)
	mkCreditCandidate(t, a.ID, b.ID)

	link := acceptLink(t, a.ID, b.ID)
	assert.True(t, link.Created)
	assert.NotEqual(t, host.ID, link.PersonID)
}

func TestCoCreditAttachPlan(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)
	link := NewLinkService(testDB)
	w1, w2, w3, w4 := createWork(t, "W1"), createWork(t, "W2"), createWork(t, "W3"), createWork(t, "W4")

	host := createPerson(t, "彩 雅介")
	held := createCreditName(t, &host.ID, "彩 雅介")
	orphan := createCreditName(t, nil, "彩雅介(きまぐれアフター、T.O.P.)")
	for _, w := range []*model.CatalogWork{w1, w2} {
		createCredit(t, w.ID, held.ID, role, nil)
		createCredit(t, w.ID, orphan.ID, role, nil)
	}

	p1, p2 := createPerson(t, "nao"), createPerson(t, "NAO")
	createCredit(t, w2.ID, createCreditName(t, &p1.ID, "nao").ID, role, nil)
	createCredit(t, w2.ID, createCreditName(t, &p2.ID, "NAO").ID, role, nil)
	ambiguous := createCreditName(t, nil, "Nao")
	createCredit(t, w2.ID, ambiguous.ID, role, nil)

	short := createPerson(t, "ハム")
	createCredit(t, w3.ID, createCreditName(t, &short.ID, "ハム").ID, role, nil)
	weak := createCreditName(t, nil, "ハム(音楽)")
	createCredit(t, w3.ID, weak.ID, role, nil)

	namesake := createPerson(t, "紫陽花さん")
	createCredit(t, w4.ID, createCreditName(t, &namesake.ID, "紫陽花さん").ID, role, nil)
	male := createCreditName(t, nil, "紫陽花さん(男性)")
	female := createCreditName(t, nil, "紫陽花さん(女性)")
	addExternalRef(t, model.EntityTypeCreditName, male.ID, model.SourceErogameScape, "101", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeCreditName, female.ID, model.SourceErogameScape, "102", model.LinkKindExact)
	createCredit(t, w4.ID, male.ID, role, nil)
	createCredit(t, w4.ID, female.ID, role, nil)

	elsewhere := createCreditName(t, nil, "彩雅介")
	giveCredits(t, elsewhere.ID, role, 2)

	plan, err := link.CoCreditAttachPlan(t.Context())
	require.NoError(t, err)
	byName := map[int64]CoCreditAttach{}
	for _, a := range plan {
		byName[a.CreditNameID] = a
	}
	require.Contains(t, byName, orphan.ID)
	assert.Equal(t, host.ID, byName[orphan.ID].PersonID)
	assert.Equal(t, 1, byName[orphan.ID].Hosts)
	assert.True(t, byName[orphan.ID].Strong)
	require.Contains(t, byName, ambiguous.ID)
	assert.Equal(t, 2, byName[ambiguous.ID].Hosts)
	require.Contains(t, byName, weak.ID)
	assert.False(t, byName[weak.ID].Strong, "a three-letter fold over one shared work is not enough")
	assert.NotContains(t, byName, male.ID, "a source that lists two people under one fold keeps them apart")
	assert.NotContains(t, byName, female.ID)
	assert.NotContains(t, byName, elsewhere.ID, "a spelling held on no shared work is no evidence")

	ok, err := link.AttachName(t.Context(), orphan.ID, host.ID, nil)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, host.ID, *personIDOfName(t, orphan.ID))

	ok, err = link.AttachName(t.Context(), orphan.ID, p1.ID, nil)
	require.NoError(t, err)
	assert.False(t, ok, "a name linked since the plan was read stays where it is")
	assert.Equal(t, host.ID, *personIDOfName(t, orphan.ID))
}

func TestCoCreditPersonPairs(t *testing.T) {
	cleanTables(t)
	role := seededRoleID(t)
	link := NewLinkService(testDB)
	w := createWork(t, "W")

	vndb := createPerson(t, "妄想 くりえーしょん")
	createCredit(t, w.ID, createCreditName(t, &vndb.ID, "妄想 くりえーしょん").ID, role, nil)
	addExternalRef(t, model.EntityTypePerson, vndb.ID, model.SourceVNDB, "s100", model.LinkKindExact)
	eg := createPerson(t, "妄想くりえーしょん(妄想クリエーション)")
	createCredit(t, w.ID, createCreditName(t, &eg.ID, "妄想くりえーしょん(妄想クリエーション)").ID, role, nil)
	giveCredits(t, createCreditName(t, &eg.ID, "妄想クリエーション").ID, role, 2)

	pen := createPerson(t, "でじたろう(小坂崇氣)")
	createCredit(t, w.ID, createCreditName(t, &pen.ID, "小坂崇氣").ID, role, nil)
	real := createPerson(t, "小坂 崇氣")
	createCredit(t, w.ID, createCreditName(t, &real.ID, "小坂 崇氣").ID, role, nil)

	s1, s2 := createPerson(t, "Saki"), createPerson(t, "saki")
	createCredit(t, w.ID, createCreditName(t, &s1.ID, "Saki").ID, role, nil)
	createCredit(t, w.ID, createCreditName(t, &s2.ID, "saki").ID, role, nil)
	addExternalRef(t, model.EntityTypePerson, s1.ID, model.SourceVNDB, "s200", model.LinkKindExact)
	addExternalRef(t, model.EntityTypePerson, s2.ID, model.SourceVNDB, "s201", model.LinkKindExact)

	pairs, err := link.CoCreditPersonPairs(t.Context())
	require.NoError(t, err)
	got := map[[2]int64]CoCreditPersonPair{}
	for _, p := range pairs {
		got[[2]int64{p.AID, p.BID}] = p
	}
	require.Len(t, got, 3)

	same := got[[2]int64{vndb.ID, eg.ID}]
	assert.True(t, same.SameDisplay)
	assert.False(t, same.RefConflict)
	src, dst := same.SurvivorTarget()
	assert.Equal(t, [2]int64{eg.ID, vndb.ID}, [2]int64{src, dst}, "the VNDB-anchored person survives even with fewer credits")

	alike := got[[2]int64{pen.ID, real.ID}]
	assert.False(t, alike.SameDisplay, "a pen name over a real name goes to review")
	assert.False(t, alike.RefConflict)

	assert.True(t, got[[2]int64{s1.ID, s2.ID}].RefConflict, "two VNDB staff are two people")

	_, err = testMerge.ProposeMerge(t.Context(), model.EntityTypePerson, eg.ID, vndb.ID, 1, "seen")
	require.NoError(t, err)
	pairs, err = link.CoCreditPersonPairs(t.Context())
	require.NoError(t, err)
	for _, p := range pairs {
		if p.AID == vndb.ID && p.BID == eg.ID {
			assert.True(t, p.ProposalExists)
		}
	}
}
