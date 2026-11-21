// Package gamescope keeps all-ages games that are not visual novels out of the
// galgame medium. The Bangumi, ErogameScape and DLsite minting lanes took every
// game those sources list, and on 2026-11-21 4,687 such works — Blue Archive,
// the Pokémon line, Python, AVG Anti-Virus — were deleted by hand.
package gamescope

import (
	"regexp"
	"strings"
)

var (
	vnGenre = regexp.MustCompile(`(?i)ADV|AVG|ノベル|のべる|novel|恋愛|恋爱|ギャルゲ|galgame|\bGAL\b|視覚|视觉小说|視覺小說|` +
		`文字冒[险險]|文字游戏|アドベンチャー|あどべんちゃー|adventure|乙女|紙芝居|电子小说|互动小说|交互小说|互动影[像视]|影游|` +
		`読み物|ボイスドラマ|恋活|ラブコメ|育成|养成|ミステリー|推理|サスペンス|探索|脱出|ホラー|冒[险險]|解[谜密]|恐怖|Horror|` +
		`デート|コミュニケ`)
	nonVNGenre = regexp.MustCompile(`(?i)RPG|ロールプレイング|\bACT\b|アクション|STG|シューティング|FTG|格[闘斗]|対戦|パズル|` +
		`\bPUZ\b|\bRAC\b|レース|スポーツ|\bSPG\b|ローグ|タワーディフェンス|MMO|音ゲ|リズム|弾幕|FPS|TPS|Shooter|Platform|` +
		`Fighting|Racing|Sports|Puzzle|Role-Playing|Action|Roguelike|TCG|カードゲーム|麻雀|将棋|囲碁|クイズ|タイピング|射击|` +
		`动作|角色扮演|卡牌|竞速|格斗|节奏|音游|Programming|ブロック崩し|ピンボール|\bMUG\b|ストラテジー|策略|経営|经营`)
)

// NonVNGenre reports a free-text genre (an ErogameScape genre, a Bangumi
// 游戏类型) that names a non-VN genre and no VN or adventure one. Plain
// シミュレーション is not one: on console it is mostly the dating sim. An empty
// genre is not one either: most are freeware novels.
func NonVNGenre(genre string) bool {
	return !vnGenre.MatchString(genre) && nonVNGenre.MatchString(genre)
}

// DLsiteNonVN reports an all-ages DLsite product that is neither an adventure
// (ADV) nor a digital novel (DNV).
func DLsiteNonVN(workType, ageCategory string) bool {
	if strings.TrimSpace(ageCategory) != "1" {
		return false
	}
	switch workType {
	case "ADV", "DNV":
		return false
	}
	return true
}
