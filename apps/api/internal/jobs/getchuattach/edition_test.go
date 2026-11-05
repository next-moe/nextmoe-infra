package getchuattach

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
)

func TestItemPlatform(t *testing.T) {
	for title, want := range map[string]string{
		"君色パトラIF 完全生産限定版 PlayStation 5版":         "ps5",
		"君色パトラIF 完全生産限定版 Nintendo Switch版":       "swi",
		"君色パトラIF 完全生産限定版":                        pcPlatform,
		"進撃の巨人3 Nintendo Switch2版":               "sw2",
		"零 ～紅い蝶～ REMAKE Nintendo Switch 2版":      "sw2",
		"true tears ～トゥルーティアーズ～ [PlayStation 2]": "ps2",
		"PSP 黄昏時～怪談ロマンス～ 豪華版":                    "psp",
		"PS3 STEINS；GATE 比翼恋理のだーりん":              "ps3",
		"すうぃ～と☆SwitcH ～まじわるシセンでとろけるカラダ～ DVDPG":   "dvd",
		"LEWDNESS～Vita sexualis～":                pcPlatform,
		"天神乱漫 Happy Go Lucky!! 初回限定版 [PSP]":      "psp",
	} {
		assert.Equal(t, want, itemPlatform(item{Title: title}), title)
	}
	assert.Equal(t, "dvd", itemPlatform(item{Title: "Plain", Media: "DVD PlayersGame"}))
	assert.Equal(t, "bdp", itemPlatform(item{Title: "Plain", Media: "Blu-ray PlayersGame"}))
	assert.Equal(t, "ps2", itemPlatform(item{Title: "Plain", Media: "PS2ソフト"}))
}

func TestFitPlatform(t *testing.T) {
	assert.Equal(t, fitUnknown, fitPlatform("ps5", nil))
	assert.Equal(t, fitExact, fitPlatform("ps5", []string{"ps5"}))
	assert.Equal(t, fitNone, fitPlatform("ps5", []string{"win"}))
	assert.Equal(t, fitExact, fitPlatform(pcPlatform, []string{"win", "and", "ios"}))
	assert.Equal(t, fitExact, fitPlatform(pcPlatform, []string{"p98"}))
	assert.Equal(t, fitNone, fitPlatform(pcPlatform, []string{"swi"}))
}

func TestEditionFormat(t *testing.T) {
	assert.Equal(t, formatDigital, itemFormat(item{Title: "X", Media: "ダウンロード作品"}))
	assert.Equal(t, formatDigital, itemFormat(item{Title: "X", Media: "DLカード"}))
	assert.Equal(t, formatPhysical, itemFormat(item{Title: "X", Media: "DVD-ROM"}))
	assert.Equal(t, formatDigital, rowFormat(relRow{Kind: model.ReleaseKindDefault, Title: "初恋マスターアップ DL豪華版"}))
	assert.Equal(t, formatDigital, rowFormat(relRow{Kind: model.ReleaseKindDefault, Title: "枯れない世界と終わる花 ダウンロード版"}))
	assert.Equal(t, formatPhysical, rowFormat(relRow{Kind: model.ReleaseKindDefault, Title: "初恋マスターアップ 豪華版"}))
	assert.Equal(t, formatUnknown, rowFormat(relRow{Kind: model.ReleaseKindDefault, Title: "アンスリウム-i entrust to you-"}))
	assert.Equal(t, formatDigital, rowFormat(relRow{Kind: model.ReleaseKindDigital}))
	assert.Equal(t, formatPhysical, rowFormat(relRow{Kind: model.ReleaseKindPhysical}))
	assert.Equal(t, formatUnknown, rowFormat(relRow{Kind: model.ReleaseKindDefault, Title: "Handle With Care"}))
}

func TestChooseBreaksTiesOnTheTitle(t *testing.T) {
	pool := []relRow{
		{ID: 180087, Title: "君が望む永遠 ～Enhanced Edition R～ - 初回生産版"},
		{ID: 186034, Title: "君が望む永遠 ～Enhanced Edition R～ - 初回生産豪華版 涼宮遙Ver."},
		{ID: 194021, Title: "君が望む永遠 ～Enhanced Edition R～ - 初回生産豪華版 涼宮茜Ver."},
	}
	got := choose(item{Title: "君が望む永遠 ～Enhanced Edition R～ 初回生産豪華版（涼宮遙Ver.）"}, pool, placedDay)
	assert.Equal(t, placement{ReleaseID: 186034, By: placedDay, Tie: tieTitle}, got)

	untitled := []relRow{{ID: 9}, {ID: 4}, {ID: 7}}
	assert.Equal(t, placement{ReleaseID: 4, By: placedDay, Tie: tieLowest}, choose(item{Title: "Any"}, untitled, placedDay))
	assert.Equal(t, placement{ReleaseID: 9, By: placedJAN}, choose(item{Title: "Any"}, untitled[:1], placedJAN))
}
