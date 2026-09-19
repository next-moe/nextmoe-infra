package vndbtitles

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecideOLangOnlyWhenEnglish(t *testing.T) {
	d := decide(decisionIn{
		OLang: vndbLangEnglish, OLangTitle: "Heart", ENTitle: "Heart",
	})
	assert.True(t, d.InsertOLang)
	assert.False(t, d.InsertEN)
	assert.False(t, d.OLangPresent)
	assert.False(t, d.ENPresent)
	assert.True(t, d.FillDisplay)
}

func TestDecideOLangAndOfficialEnglish(t *testing.T) {
	d := decide(decisionIn{
		OLang: "ru", OLangTitle: "Сердце", ENTitle: "Heart",
	})
	assert.True(t, d.InsertOLang)
	assert.True(t, d.InsertEN)
	assert.True(t, d.writes())
}

func TestDecideSameLangTitleOtherKindIsPresent(t *testing.T) {
	d := decide(decisionIn{
		Titles: []existingTitle{
			{Lang: "ru", Title: "Сердце"},
			{Lang: "en", Title: "Heart"},
		},
		OLang: "ru", OLangTitle: "Сердце", ENTitle: "Heart",
		DisplayName: "kept",
	})
	assert.False(t, d.InsertOLang)
	assert.False(t, d.InsertEN)
	assert.True(t, d.OLangPresent)
	assert.True(t, d.ENPresent)
	assert.False(t, d.writes())
}

func TestDecideTitleUnderAnotherLangIsPresent(t *testing.T) {
	d := decide(decisionIn{
		Titles: []existingTitle{{Lang: "en", Title: "Сердце"}, {Lang: "", Title: "Heart"}},
		OLang:  "ru", OLangTitle: "Сердце", ENTitle: "Heart",
		DisplayName: "kept",
	})
	assert.True(t, d.OLangPresent)
	assert.True(t, d.ENPresent)
	assert.False(t, d.writes())
}

func TestDecideEnglishEqualToOLangTitleIsNotAddedTwice(t *testing.T) {
	d := decide(decisionIn{OLang: "ru", OLangTitle: "Heart", ENTitle: "Heart", DisplayName: "kept"})
	assert.True(t, d.InsertOLang)
	assert.False(t, d.InsertEN)
	assert.True(t, d.ENPresent)
}

func TestDecideEmptyDisplayNameFilled(t *testing.T) {
	d := decide(decisionIn{OLang: "ja", OLangTitle: "こころ", DisplayName: ""})
	assert.True(t, d.FillDisplay)
	assert.False(t, d.SkipDisplayHuman)
}

func TestDecideWhitespaceDisplayNameFilled(t *testing.T) {
	d := decide(decisionIn{OLang: "ja", OLangTitle: "こころ", DisplayName: "   "})
	assert.True(t, d.FillDisplay)
	assert.False(t, d.SkipDisplayHuman)
}

func TestDecideNonEmptyDisplayNameUntouched(t *testing.T) {
	d := decide(decisionIn{OLang: "ja", OLangTitle: "こころ", DisplayName: "kept"})
	assert.False(t, d.FillDisplay)
	assert.False(t, d.SkipDisplayHuman)
}

func TestDecideHumanProvenanceSkipped(t *testing.T) {
	d := decide(decisionIn{
		OLang: "ja", OLangTitle: "こころ", DisplayName: "", DisplayHuman: true,
	})
	assert.False(t, d.FillDisplay)
	assert.True(t, d.SkipDisplayHuman)
	assert.True(t, d.InsertOLang)
}

func TestSummaryKeysAreNotSuffixes(t *testing.T) {
	seen := map[string]int{}
	for i, k := range summaryKeys {
		require.NotEmpty(t, k)
		seen[k]++
		assert.Equal(t, 1, seen[k], "key %q appears more than once", k)
		for j, other := range summaryKeys {
			if i == j {
				continue
			}
			assert.False(t, strings.HasSuffix(other, k), "key %q is a suffix of %q", k, other)
		}
	}
	st := Stats{
		WorksPopulation: 1, WorksMultiAnchor: 2, WorksVNMissing: 3, WorksChanged: 4,
		TitlesOLangInserted: 5, TitlesENInserted: 6, TitlesPresent: 7, TitlesLost: 8,
		DisplayFilled: 9, DisplayHumanSkipped: 10, DisplayLost: 11, Errors: 12,
	}
	vals := st.logValues()
	require.Equal(t, len(summaryKeys), len(vals))
	args := st.LogArgs()
	require.Equal(t, 2*len(summaryKeys), len(args))
	for i, k := range summaryKeys {
		assert.Equal(t, k, args[2*i], "LogArgs key %d", i)
		assert.Equal(t, vals[i], args[2*i+1], "LogArgs value for %s", k)
	}
}

func TestRunRequiresDSN(t *testing.T) {
	_, err := Run(context.Background(), Opts{})
	require.EqualError(t, err, "catalog DSN is required (--dsn); refusing to guess")
}
