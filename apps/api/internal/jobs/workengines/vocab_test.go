package workengines

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroupKeyFoldsSpellings(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Ren'Py", "renpy"},
		{"Renpy", "renpy"},
		{"RenPy Engine", "renpy"},
		{"吉里吉里2/KAG3", "kirikiri"},
		{"ＫＲＫＲ２", "kirikiri"},
		{"KRKR2/KAG3", "kirikiri"},
		{"ティラノビルダー", "tyranoscript"},
		{"Tyranobuilder", "tyranoscript"},
		{"RPGツクールVX Ace", "rpgmaker"},
		{"RPG-Maker 2000", "rpgmaker"},
		{"Unity（Steam版）", "unity"},
		{"BGI/Ethornell", "bgiethornell"},
		{"PC", "pc"},
	} {
		assert.Equal(t, c.want, groupKey(c.in), c.in)
	}
}

func TestAliasTargetsAreTheirOwnGroups(t *testing.T) {
	for target := range aliasesByName {
		k := engineKey(target)
		_, aliased := aliasKeys[k]
		assert.False(t, aliased, "%s must not itself fold into another engine", target)
	}
}
