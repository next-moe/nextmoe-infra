package service

import (
	"encoding/json"
	"testing"

	"api/internal/platform/agent/tools"
)

func card(id, title string, localized ...string) tools.Card {
	l := map[string]map[string]string{}
	for i, v := range localized {
		l[string(rune('a'+i))] = map[string]string{"value": v}
	}
	raw, _ := json.Marshal(map[string]any{"id": id, "title": title, "localized": l})
	return tools.Card{ID: id, Data: raw}
}

func ids(cards []tools.Card) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.ID)
	}
	return out
}

func TestPickCardsKeepsWhatTheAnswerNamesInOrder(t *testing.T) {
	search := foundCards{cards: []tools.Card{
		card("work-1", "千恋＊万花", "Senren＊Banka"),
		card("work-2", "サノバウィッチ", "魔女的夜宴"),
		card("work-3", "RIDDLE JOKER"),
		card("work-4", "喫茶ステラと死神の蝶", "星光咖啡馆与死神之蝶"),
	}}
	got := ids(pickCards([]foundCards{search}, "先玩《魔女的夜宴》，再玩《千恋 万花》。riddle joker 也不错。"))
	if want := []string{"work-2", "work-1", "work-3"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPickCardsPrefersTheLaterLookupOfTheSameEntity(t *testing.T) {
	brief := card("character-9", "朝武芳乃")
	full := card("character-9", "朝武芳乃", "Tomotake Yoshino")
	got := pickCards([]foundCards{{cards: []tools.Card{brief}}, {cards: []tools.Card{full}}}, "朝武芳乃是巫女。")
	if len(got) != 1 || string(got[0].Data) != string(full.Data) {
		t.Fatalf("the detail lookup's card must replace the search's, got %s", got)
	}
}

func TestPickCardsFallsBackToLookupsOnly(t *testing.T) {
	search := foundCards{cards: []tools.Card{card("company-1", "柚子软件")}}
	lookup := foundCards{cards: []tools.Card{card("character-9", "朝武芳乃")}, lookup: true}
	if got := ids(pickCards([]foundCards{search}, "资料库里几乎搜不到柚子社。")); len(got) != 0 {
		t.Fatalf("an unnamed search must show no cards, however few it found, got %v", got)
	}
	if got := ids(pickCards([]foundCards{search, lookup}, "她是一位巫女。")); len(got) != 1 || got[0] != "character-9" {
		t.Fatalf("a direct lookup must still show its card, got %v", got)
	}
}

func TestIsLookup(t *testing.T) {
	for tool, want := range map[string]bool{"get_work": true, "get_entity": true, "kungal__get_topic": true, "search_catalog": false, "kungal__search_topics": false, "web_search": false} {
		if isLookup(tool) != want {
			t.Errorf("%s: want %v", tool, want)
		}
	}
}

func TestPickCardsSkipsNamesTooShortToMatch(t *testing.T) {
	short := foundCards{cards: []tools.Card{card("character-1", "桜"), card("work-5", "AIR"), card("work-6", "x"), card("work-7", "y"), card("work-8", "z")}}
	if got := ids(pickCards([]foundCards{short}, "樱花盛开，空气清新 air")); len(got) != 0 {
		t.Fatalf("one-rune and three-letter names must not match loosely, got %v", got)
	}
	if got := ids(pickCards([]foundCards{short}, "Key 的《AIR》很经典")); len(got) != 1 || got[0] != "work-5" {
		t.Fatalf("a short title in 《》 must match, got %v", got)
	}
}
