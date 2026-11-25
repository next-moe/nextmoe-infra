package walkthrough

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyEndingsSplitsTheSingleAxisType(t *testing.T) {
	c := Chart{Endings: []Ending{
		{Key: "e1", ImportType: "heroine", Characters: []string{"c1"}},
		{Key: "e2", ImportType: "heroine", Characters: []string{"c1", "c2"}},
		{Key: "e3", ImportType: "harem", Characters: []string{"c1"}},
		{Key: "e4", ImportType: "bad", Characters: []string{"c1"}},
		{Key: "e5", ImportType: "normal"},
		{Key: "e6", ImportType: "true", Characters: []string{"c1", "c2"}},
		{Key: "e7", ImportType: "after", Characters: []string{"c1"}},
		{Key: "e8", ImportType: "other"},
		{Key: "e9", RouteType: ptr(RouteCommon), Outcome: ptr("happy")},
	}}
	ClassifyEndings(&c)
	type axes struct {
		route, outcome string
		after, other   bool
		endingType     string
	}
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	var got []axes
	for _, e := range c.Endings {
		assert.Empty(t, e.ImportType)
		got = append(got, axes{deref(e.RouteType), deref(e.Outcome), e.After, e.Other, EndingType(e)})
	}
	assert.Equal(t, []axes{
		{"heroine", "happy", false, false, "heroine"},
		{"harem", "happy", false, false, "harem"},
		{"harem", "happy", false, false, "harem"},
		{"heroine", "bad", false, false, "bad"},
		{"common", "normal", false, false, "normal"},
		{"harem", "true", false, false, "true"},
		{"heroine", "", true, false, "after"},
		{"", "", false, true, "other"},
		{"common", "happy", false, false, "other"},
	}, got)
}
