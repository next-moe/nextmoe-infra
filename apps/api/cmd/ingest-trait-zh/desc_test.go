package main

import (
	"testing"

	catsvc "api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteTraitLinks(t *testing.T) {
	names := map[string]string{"i12": "Small breasts"}

	rewritten, linked := rewriteTraitLinks("[url=/i12]i12[/url]", names)
	assert.Equal(t, "[url=/i12]Small breasts[/url]", rewritten)
	assert.Equal(t, []string{"i12"}, linked)
	assert.Equal(t, "Small breasts", catsvc.PlainTraitDescription(rewritten))

	rewritten, linked = rewriteTraitLinks("[url=https://vndb.org/i12]small breasts[/url]", names)
	assert.Equal(t, "[url=https://vndb.org/i12]small breasts[/url]", rewritten)
	assert.Equal(t, []string{"i12"}, linked)
	assert.Equal(t, "small breasts", catsvc.PlainTraitDescription(rewritten))

	rewritten, linked = rewriteTraitLinks("[url=https://example.com/x]keep[/url]", names)
	assert.Equal(t, "[url=https://example.com/x]keep[/url]", rewritten)
	assert.Empty(t, linked)
	assert.Equal(t, "keep", catsvc.PlainTraitDescription(rewritten))

	rewritten, linked = rewriteTraitLinks("[url=/i12]i12[/url]", nil)
	assert.Equal(t, "[url=/i12]i12[/url]", rewritten)
	assert.Equal(t, []string{"i12"}, linked)
}

func TestBuildGlossaryOrderAndDedup(t *testing.T) {
	self := traitLex{Name: "Toned", NameZh: "健美"}
	group := traitLex{Name: "Hair", NameZh: "毛发"}
	parents := []traitLex{
		{Name: "Body", NameZh: "身体"},
		{Name: "Hair", NameZh: "毛发重复"},
		{Name: "Empty", NameZh: ""},
	}
	linked := []traitLex{
		{Name: "Small breasts", NameZh: "贫乳"},
		{Name: "Toned", NameZh: "该忽略"},
		{Name: "NoZh", NameZh: ""},
	}
	got := buildGlossary(self, &group, parents, linked)
	assert.Equal(t, []glossPair{
		{"Toned", "健美"},
		{"Hair", "毛发"},
		{"Body", "身体"},
		{"Small breasts", "贫乳"},
	}, got)

	assert.Empty(t, buildGlossary(traitLex{Name: "X", NameZh: ""}, nil, nil, nil))
}

func TestDescribeUserMessage(t *testing.T) {
	with := describeUserMessage("Ahoge", "A strand.", []glossPair{
		{"Ahoge", "呆毛"},
		{"Hair", "毛发"},
	})
	assert.Equal(t, "特征名：Ahoge\n术语表：\n- Ahoge → 呆毛\n- Hair → 毛发\n\n英文释义：\nA strand.", with)

	empty := describeUserMessage("Ahoge", "A strand.", nil)
	assert.Equal(t, "特征名：Ahoge\n\n英文释义：\nA strand.", empty)
	assert.NotContains(t, empty, "术语表")
}

func TestSourceHashHello(t *testing.T) {
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", sourceHash("hello"))
}

func TestParseIDList(t *testing.T) {
	ids, err := parseIDList("1, 2,3")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, ids)
	ids, err = parseIDList("")
	require.NoError(t, err)
	assert.Nil(t, ids)
	_, err = parseIDList("1,x")
	require.Error(t, err)
}

func TestBareTraitRefsBecomeCuratedNames(t *testing.T) {
	byTID := map[string]traitLex{
		"i568":  {VndbTID: "i568", Name: "Anal Sex", NameZh: "肛交", GroupTID: "i43"},
		"i18":   {VndbTID: "i18", Name: "Ponytail", NameZh: "马尾辫", GroupTID: "i1"},
		"i43":   {VndbTID: "i43", Name: "Engages in (Sexual)", NameZh: "主动(性)"},
		"i1":    {VndbTID: "i1", Name: "Hair", NameZh: "毛发"},
		"i1597": {VndbTID: "i1597", Name: "Anal Fingering", NameZh: "肛门指交"},
	}
	row := descRow{Name: "Anal Sex", NameZh: "肛交", Description: "Use this on the receiver and i568 on the other. Not for i18 (see [url=/i1597]i1597[/url]), nor i99999 or vi568."}
	labels := bareRefLabels(row, byTID)
	assert.Equal(t, "「肛交」（主动(性)）", labels["i568"])
	assert.Equal(t, "「马尾辫」", labels["i18"])
	_, unknown := labels["i99999"]
	assert.False(t, unknown)

	zh := "此特征用于接受方，插入方使用 i568。\n\n扎马尾辫请使用 i18。未知 i99999 保留。"
	assert.Equal(t, "此特征用于接受方，插入方使用「肛交」（主动(性)）。\n\n扎马尾辫请使用「马尾辫」。未知 i99999 保留。",
		replaceBareTraitRefs(zh, labels))
	assert.Equal(t, "见 https://vndb.org/i568 与 vi568", replaceBareTraitRefs("见 https://vndb.org/i568 与 vi568", labels))
}
