package service

import "testing"

func TestTidyTitleKeepsBracketPairsWhole(t *testing.T) {
	for in, want := range map[string]string{
		"「猫耳妹妹推荐」":       "猫耳妹妹推荐",
		"《白色相簿2》剧情概要。":   "《白色相簿2》剧情概要",
		"暗号是「猫耳」":        "暗号是「猫耳」",
		"\"VS Code 版本\"": "VS Code 版本",
		"关于「一部非常非常非常非常非常非常非常非常长的作品名": "关于",
	} {
		if got := tidyTitle(in); got != want {
			t.Errorf("tidyTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
