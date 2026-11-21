package gamescope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNonVNGenre(t *testing.T) {
	for genre, want := range map[string]bool{
		"角色收集RPG":              true,
		"学園×青春×物語RPG":          true,
		"Programming Language": true,
		"Third-Person, Action": true,
		"2D対戦格闘":               true,
		"リズムゲーム":               true,
		"脱衣麻雀":                 true,
		"ADV":                  false,
		"恋愛AVG":                false,
		"AVG+ACT 真·武斗派学园恋爱喜剧": false,
		"ロールプレイングノベル":         false,
		"恐怖 / 动作 / 冒险":        false,
		"お嬢様育成シミュレーション":       false,
		"シミュレーション":            false,
		"":                    false,
		"同人ゲーム":               false,
	} {
		assert.Equal(t, want, NonVNGenre(genre), genre)
	}
}

func TestDLsiteNonVN(t *testing.T) {
	assert.True(t, DLsiteNonVN("RPG", "1"))
	assert.True(t, DLsiteNonVN("ETC", "1"))
	assert.False(t, DLsiteNonVN("ADV", "1"))
	assert.False(t, DLsiteNonVN("DNV", "1"))
	assert.False(t, DLsiteNonVN("RPG", "3"))
	assert.False(t, DLsiteNonVN("ACN", "2"))
}
