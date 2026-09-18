package getchuattach

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	bundleNin1    = regexp.MustCompile(`(?i)[0-9]+in1`)
	bundleNTitle  = regexp.MustCompile(`[0-9]+タイトル`)
	bundleWord    = regexp.MustCompile(`(?i)(^|[^a-z0-9])(collection|complete|anniversary|history|trilogy|set|pack|box)([^a-z]|$)`)
	bundleNeedles = []string{
		"コレクション", "コンプリート", "全巻", "トリロジー", "総集編", "セット", "パック",
		"ボックス", "同梱", "+", "＋", "wパッケージ", "厳選集", "傑作選", "作品集",
	}
	goodsNeedles = []string{
		"抱き枕", "タペストリー", "アクリル", "フィギュア", "スエード", "ポスター",
		"キーホルダー", "ぬいぐるみ", "マウスパッド", "クッション", "シーツ", "オナホ",
		"グッズ", "缶バッジ", "色紙", "抱きまくら", "抱まくら",
	}
	extrasTitle = []string{"画像集", "CG集", "壁紙", "素材集", "アクセサリー"}
	extrasSub   = []string{"CG集", "アクセサリー集"}
	extrasGenre = []string{"CGイラスト集", "アクセサリ"}

	generalSubgenre = regexp.MustCompile(`(?i)(アクション|fps|rts|シューティング|スポーツ|レース|フライト|格闘)`)
	addonTitle      = []string{
		"アペンド", "追加ディスク", "プラグイン", "パワーアップキット", "拡張",
		"DLC", "キャラクターパック", "性格パック", "アップデート", "ブースター",
	}
	reissueNeedles = []string{
		"廉価版", "普及版", "新装版", "新価格版", "価格改定版", "再販", "再生産",
		"再プレス", "再出荷", "ヌキコレ", "best windows", "セレクション", "selection",
		"特別価格", "応援セール", "キャンペーン", "the best", "hb edition", "dvdpg",
		"アウトレット", "記念", "best price", "ベストプライス", "ベスト・プライス",
	}
	reissueBrands = []string{"ベスコレ", "ホビコレ"}
)

func screened(it item, st *Stats) bool {
	// 269 unanchored items are bundles and 161 are goods bundles (抱き枕カバー,
	// タペストリー, アクリル) on the 2026-09-18 hold-out of 3,737.
	if isBundle(it.Title, it.Subgenre) {
		st.Bundles++
		return true
	}
	if isGoods(it.Title) {
		st.Goods++
		return true
	}
	return false
}

func mintGate(it item, snap snapshot, st *Stats) bool {
	// 1,153 unanchored items have no adult notice (KOEI, Square Enix, font and
	// material CDs, VOCALOID), measured 2026-09-18.
	if !it.Adult {
		st.AllAges++
		return true
	}
	if isExtras(it.Title, it.Genre, it.Subgenre) {
		st.Extras++
		return true
	}
	if isGeneralGame(it.Subgenre) {
		st.General++
		return true
	}
	if isAddon(it.Title, it.Genre, it.Subgenre) {
		st.Addons++
		return true
	}
	if isReissue(it.Title, it.Subgenre, it.Brand) {
		st.Reissues++
		return true
	}
	if strings.TrimSpace(it.ReleaseDate) == "発売中止" {
		st.Cancelled++
		return true
	}
	if catalogDay(it.ReleaseDate) == "" {
		st.Undated++
		return true
	}
	if !sharesAdultEGBrand(it, snap) {
		st.BrandUnknown++
		return true
	}
	// A same-brand EG game with a related title was the item's own work in
	// 872 of 932 hold-out items whose dates differ by more than 31 days
	// (2026-09-18). EG lists every adult game, so such an item is another
	// edition of a work the catalog already holds, never a new one.
	if len(sameBrandRelatedGames(it, snap)) > 0 || anyBrandEGEdition(it, snap) {
		st.EGEditions++
		return true
	}
	return false
}

func isBundle(title, subgenre string) bool {
	n := nfkcFold(title)
	for _, s := range bundleNeedles {
		if strings.Contains(n, strings.ToLower(s)) {
			return true
		}
	}
	if bundleNin1.MatchString(n) || bundleNTitle.MatchString(title) || bundleWord.MatchString(n) {
		return true
	}
	return strings.Contains(nfkcFold(subgenre), "セット商品")
}

func isGoods(title string) bool {
	n := nfkcFold(title)
	for _, s := range goodsNeedles {
		if strings.Contains(n, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func isExtras(title, genre, subgenre string) bool {
	t, g, s := nfkcFold(title), nfkcFold(genre), nfkcFold(subgenre)
	for _, n := range extrasTitle {
		if strings.Contains(t, strings.ToLower(n)) {
			return true
		}
	}
	for _, n := range extrasSub {
		if strings.Contains(s, strings.ToLower(n)) {
			return true
		}
	}
	if parts := subgenreParts(subgenre); len(parts) == 1 && parts[0] == "その他" {
		return true
	}
	for _, n := range extrasGenre {
		if strings.Contains(g, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// isGeneralGame holds when every Getchu subgenre is an action-like one.
// LEFT4 DEAD and GTA V carry the 18+ notice and an 18+ EG brand; 41 of 16,212
// anchored adult items (BALDR, DUEL SAVIOR) are such games too, and VNDB and
// EG list those, so a Getchu mint never needs them (measured 2026-09-18).
func isGeneralGame(subgenre string) bool {
	parts := subgenreParts(subgenre)
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !generalSubgenre.MatchString(p) {
			return false
		}
	}
	return true
}

func subgenreParts(subgenre string) []string {
	s := strings.ReplaceAll(nfkcFold(subgenre), "[一覧]", "")
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == '、' || r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isAddon(title, genre, subgenre string) bool {
	t := nfkcFold(title)
	for _, n := range addonTitle {
		if strings.Contains(t, strings.ToLower(n)) {
			return true
		}
	}
	return strings.Contains(nfkcFold(genre), "アペンド") || strings.Contains(nfkcFold(subgenre), "アペンド")
}

func isReissue(title, subgenre, brand string) bool {
	t := nfkcFold(title)
	for _, n := range reissueNeedles {
		if strings.Contains(t, strings.ToLower(n)) {
			return true
		}
	}
	if strings.Contains(nfkcFold(subgenre), "廉価版") {
		return true
	}
	b := nfkcFold(brand)
	for _, n := range reissueBrands {
		if strings.Contains(b, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

func nfkcFold(s string) string {
	return strings.ToLower(norm.NFKC.String(s))
}
