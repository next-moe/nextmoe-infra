package getchuattach

import (
	"testing"
	"time"

	"api/internal/platform/catalog/titlekey"

	"github.com/stretchr/testify/assert"
)

func TestBaseTitle(t *testing.T) {
	assert.Equal(t, "蒼き迷宮のエルヴィンテイル", BaseTitle("蒼き迷宮のエルヴィンテイル クローディル限定版"))
	assert.Equal(t, "戦国†恋姫BRAVE弐 ～戦乱の九州、島津編～", BaseTitle("戦国†恋姫BRAVE弐 ～戦乱の九州、島津編～ 豪華版"))
	assert.Equal(t, "RON RON 3on6", BaseTitle("RON RON 3on6 通常版"))
	assert.Equal(t, "今日のおかず 最凶痴女上司麗子！ 第一話", BaseTitle("今日のおかず 最凶痴女上司麗子！ 第一話"))
	assert.Equal(t, "Bracket Game", BaseTitle("Bracket Game【げっちゅ屋限定版】"))
}

func TestBrandKeys(t *testing.T) {
	hook := egBrandKeys("HOOKSOFT(HOOK)", "")
	assert.Contains(t, hook, titlekey.Loose("HOOKSOFT(HOOK)"))
	assert.Contains(t, hook, titlekey.Loose("HOOKSOFT"))

	eg := egBrandKeys("エルフ", "elf")
	gc := getchuBrandKeys("elf")
	assert.True(t, brandKeysOverlap(eg, gc), "elf/エルフ via furigana")

	parts := getchuBrandKeys("A／B（C）")
	assert.NotContains(t, parts, titlekey.Loose("A"), "1-rune part dropped")
	assert.Contains(t, parts, titlekey.Loose("B"))
}

func TestRelatedTitle(t *testing.T) {
	assert.True(t, relatedTitle("今日のおかず 家庭教師は魔女先生", "家庭教師は魔女先生"))
	assert.True(t, relatedTitle("家庭教師は魔女先生", "今日のおかず 家庭教師は魔女先生"))
	assert.True(t, relatedTitle("〇眠〇漢Episode1", "催眠痴漢Episode1"))
	assert.False(t, relatedTitle("〇眠", "催眠痴漢Episode1"))
	assert.True(t, relatedTitle(
		"NEW BREEDER～エッチな獣娘を狩って自分好みに○○！～ 限定版 ピンナップガールズスキットル付き",
		"New Breeder 〜エッチな獣娘を狩って自分好みに調教！〜"), "spacing, case, tilde and edition tail ignored")
	assert.False(t, relatedTitle("NEW BREEDER～エッチな獣娘を狩って自分好みに○○！～", "Old Breeder 〜エッチな獣娘を狩って自分好みに調教！〜"))
	assert.False(t, censorMatch("家庭教師は魔女先生", "家庭教師は魔女先生"), "no censor rune, no censor match")
}

func TestScreens(t *testing.T) {
	assert.True(t, isBundle("Foo コレクション", ""))
	assert.False(t, isBundle("普通のゲームタイトル", ""))
	assert.True(t, isGoods("抱き枕カバー付属"))
	assert.False(t, isGoods("普通のゲームタイトル"))
	assert.True(t, isExtras("画像集スペシャル", "", ""))
	assert.False(t, isExtras("普通のゲームタイトル", "", ""))
	assert.True(t, isAddon("アペンドディスク", "", ""))
	assert.False(t, isAddon("普通のゲームタイトル", "", ""))
	assert.True(t, isReissue("廉価版 Windows", "", ""))
	assert.False(t, isReissue("普通のゲームタイトル", "", ""))
	assert.True(t, isReissue("眠れぬ羊と孤独な狼 Best Price", "", ""))
	assert.True(t, isGoods("青春アンソロジー 抱きまくらカバー付"))
	assert.True(t, isBundle("ミレニアムBOX2000 Vol.1", ""))
	assert.False(t, isBundle("Boxer Rebellion", ""))
	assert.True(t, isBundle("ツンツンクールな13人！Norn作品厳選集", ""))
	assert.True(t, isExtras("P/ECE ブルー", "", "その他 [一覧]"))
	assert.False(t, isExtras("鈴菜日記", "", "アドベンチャー、その他 [一覧]"))
	assert.True(t, isGeneralGame("アクション [一覧]"))
	assert.True(t, isGeneralGame("FPS [一覧]"))
	assert.False(t, isGeneralGame("アクション、アドベンチャー [一覧]"))
	assert.False(t, isGeneralGame(""))
}

func TestPlaceholderGetchuDateIsNoDate(t *testing.T) {
	assert.Equal(t, "", catalogDay("0001/01/01"))
	assert.Equal(t, "1997-05-23", catalogDay("1997/05/23"))
	y, _, _ := releaseParts("0001/01/01", time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	assert.Nil(t, y)
}
