package eganchors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTitleKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"NFKC width folding", "ＤＥＡＲ", "dear"},
		{"punctuation and spaces dropped", "Clear -クリア-", "clearクリア"},
		{"fullwidth tilde dropped", "A～B", "ab"},
		{"wave dash dropped", "A〜B", "ab"},
		{"case folded", "Dear", "dear"},
		{"CJK kept", "海の唄", "海の唄"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, titleKey(tc.in))
		})
	}
	assert.Equal(t, titleKey("A～B"), titleKey("A〜B"))
}

func TestTitleKeysCorroborate(t *testing.T) {
	assert.True(t, titleKeysCorroborate("dear", "dear"))
	assert.True(t, titleKeysCorroborate(titleKey("海の唄がきこえる"), titleKey("海の唄がきこえる 上巻")))
	assert.False(t, titleKeysCorroborate("abc", "abcdef"))
	assert.False(t, titleKeysCorroborate("ab", "abcd"))
}
