package registry

type Term struct {
	Key        string   `json:"key"`
	EntailedBy []string `json:"entailed_by,omitempty"`
	Doc        string   `json:"doc,omitempty"`
}

type Vocabulary struct {
	Name  string `json:"name"`
	Terms []Term `json:"terms"`
	// WholeWords: a page word counts only standing alone. By substring the gold
	// set caught 「原作」 read as author (作) and 「企画・原案」 as illustrator (画).
	// Other vocabularies need the substring: 「電撃文庫」 is a 文庫.
	WholeWords bool `json:"whole_words,omitempty"`
}

// Vocabularies is every vocabulary a property may name; a property naming one
// missing here fails TestEveryVocabPropertyNamesAKnownVocabulary.
var Vocabularies = []Vocabulary{
	{Name: "medium", Terms: []Term{{Key: "game"}, {Key: "novel"}, {Key: "manga"}, {Key: "anime"}, {Key: "audio"}}},
	{Name: "work_status", Terms: []Term{
		{Key: "ongoing", EntailedBy: []string{"続刊", "以下続刊", "連載中"}},
		{Key: "complete", EntailedBy: []string{"完結", "最終巻"}},
		{Key: "hiatus", EntailedBy: []string{"休載", "刊行停止"}},
		{Key: "unknown"},
	}},
	{Name: "content_rating", Terms: []Term{{Key: "all_ages"}, {Key: "r15", EntailedBy: []string{"R15", "R-15"}}, {Key: "r18", EntailedBy: []string{"18禁", "R18", "R-18", "成人向け"}}}},
	{Name: "display_safety", Terms: []Term{{Key: "unjudged"}, {Key: "safe"}, {Key: "suggestive"}, {Key: "explicit"}}},
	{Name: "title_kind", Terms: []Term{{Key: "official"}, {Key: "alternative"}, {Key: "romanized"}, {Key: "translated"}}},
	{Name: "part_kind", Terms: []Term{
		{Key: "volume", Doc: "A bound volume; the part an ISBN edition contains, so a classification, not a page word."},
		{Key: "chapter", EntailedBy: []string{"話", "章"}},
		{Key: "season"}, {Key: "episode"}, {Key: "disc"},
	}},
	{Name: "edition_kind", Terms: []Term{
		{Key: "standard"},
		{Key: "limited", EntailedBy: []string{"限定版", "特装版", "特裝版"}},
		{Key: "reprint", EntailedBy: []string{"新装版", "新裝版", "復刻"}},
	}},
	{Name: "format", Terms: []Term{
		{Key: "paperback", EntailedBy: []string{"文庫", "文庫判", "新書"}},
		{Key: "hardcover", EntailedBy: []string{"単行本", "四六判", "B6判"}},
		{Key: "digital", EntailedBy: []string{"電子書籍", "電子版", "電子書"}},
	}},
	{Name: "release_status", Terms: []Term{{Key: "announced", EntailedBy: []string{"近刊", "予約", "発売予定"}}, {Key: "released"}, {Key: "cancelled", EntailedBy: []string{"発売中止", "刊行中止"}}}},
	{Name: "portion", Terms: []Term{{Key: "full"}, {Key: "partial"}}},
	{Name: "org_kind", Terms: []Term{{Key: "company"}, {Key: "brand"}, {Key: "label"}, {Key: "magazine"}, {Key: "platform"}, {Key: "circle"}, {Key: "translation_group"}}},
	{Name: "book_role", WholeWords: true, Terms: []Term{
		{Key: "author", EntailedBy: []string{"著", "著者", "作", "作者", "作家", "文"}},
		{Key: "illustrator", EntailedBy: []string{"イラスト", "イラストレーター", "絵", "画", "插畫", "插画", "繪者"}},
		{Key: "original_work", EntailedBy: []string{"原作"}},
		{Key: "character_design", EntailedBy: []string{"キャラクター原案", "キャラクターデザイン"}},
		{Key: "translator", EntailedBy: []string{"翻訳", "訳", "譯者", "译者"}},
		{Key: "publisher", EntailedBy: []string{"発行", "出版"}},
	}},
	{Name: "price_basis", Terms: []Term{
		{Key: "tax_included", EntailedBy: []string{"税込", "含稅", "含税"}},
		{Key: "tax_excluded", EntailedBy: []string{"本体", "税抜", "+税", "未稅"}},
	}},
	{Name: "book_category", Terms: []Term{{Key: "light_novel"}, {Key: "light_bungei"}, {Key: "shin_bungei"}, {Key: "general"}}},
}

func VocabularyByName(name string) (Vocabulary, bool) {
	for _, v := range Vocabularies {
		if v.Name == name {
			return v, true
		}
	}
	return Vocabulary{}, false
}
