package vndbtagmap

import (
	"testing"
)

func TestParseTagMapShapes(t *testing.T) {
	const src = `export const tagMap = {
  'Protagonist': '主人公',
  "Protagonist's Pronoun Choice": '主角自称',
  Pokémon: '宝可梦',
  ADV: '文字冒险',
  'Some Very Long English Tag Name That Prettier Wraps':
    '被折行的条目',
  'Trailing': '结尾',
}`
	got := Parse([]byte(src))
	want := map[string]string{
		"Protagonist":                  "主人公",
		"Protagonist's Pronoun Choice": "主角自称",
		"Pokémon":                      "宝可梦",
		"ADV":                          "文字冒险",
		"Some Very Long English Tag Name That Prettier Wraps": "被折行的条目",
		"Trailing": "结尾",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("parsed %d entries, want %d: %v", len(got), len(want), got)
	}
}

func TestEmbeddedMap(t *testing.T) {
	m := Embedded()
	if len(m) < 5000 {
		t.Fatalf("Embedded() has %d entries, want at least 5000", len(m))
	}
	if got := m["Protagonist"]; got != "主人公" {
		t.Errorf("Protagonist: got %q, want 主人公", got)
	}
	if got := m["Pure Love Story"]; got != "纯爱故事" {
		t.Errorf("Pure Love Story: got %q, want 纯爱故事", got)
	}
	if got := m["Love Overcomes All"]; got != "纯爱故事" {
		t.Errorf("Love Overcomes All: got %q, want 纯爱故事", got)
	}
}
