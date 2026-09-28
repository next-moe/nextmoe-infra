package srcbangumi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSteamAppIDs(t *testing.T) {
	cases := []struct {
		name, infobox string
		want          []string
	}{
		{"store url in a link item",
			`{"Fields":[{"Key":"链接","Items":[{"Key":"Steam","Value":"https://store.steampowered.com/app/70400/X/"}]}]}`,
			[]string{"70400"}},
		{"community and steamdb urls",
			`{"Fields":[{"Key":"website","Value":"https://steamcommunity.com/app/2/"},{"Key":"SteamDB","Value":"https://steamdb.info/app/1/"}]}`,
			[]string{"1", "2"}},
		{"bare number under a full-width steam key",
			`{"Fields":[{"Key":"Steaｍ","Value":"351640 (2015-06-18)"}]}`,
			[]string{"351640"}},
		{"bare number under another key is not an appid",
			`{"Fields":[{"Key":"售价","Value":"55"}]}`,
			[]string{}},
		{"prose under a steam key is not an appid",
			`{"Fields":[{"Key":"Steam","Value":"Demolition"}]}`,
			[]string{}},
		{"undecodable", `not json`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, SteamAppIDs([]byte(c.infobox)))
		})
	}
}
