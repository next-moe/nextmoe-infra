package entityintromt

import "testing"

func TestSubstanceRunes(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"…!?――【】", 0}, // markup glyphs carry nothing to translate
		{"沙耶", 2},
		{"Robo Clinic 2", 11}, // letters and the digit, not the spaces
		{"名前:\n———\n", 2},
	}
	for _, c := range cases {
		if got := substanceRunes(c.in); got != c.want {
			t.Fatalf("substanceRunes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
