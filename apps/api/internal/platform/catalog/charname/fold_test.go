package charname

import "testing"

func TestFoldName(t *testing.T) {
	cases := []struct {
		a, b  string
		equal bool
	}{
		{"蓮佛雪之進", "蓮仏 雪之進", true},
		{"冬月十夜", "冬月 十夜", true},
		{"硯川・e・涙香", "硯川・E・涙香", true},
		{"髙橋しょう子", "高橋しょう子", true},
		{"硯川・e・涙香", "硯川・ユーフラジー・涙香", false},
		{"蓮見榛佳", "蓮見槇夏", false},
	}
	for _, c := range cases {
		if got := Fold(c.a) == Fold(c.b); got != c.equal {
			t.Errorf("Fold(%q) vs Fold(%q): equal=%v, want %v", c.a, c.b, got, c.equal)
		}
	}
}
