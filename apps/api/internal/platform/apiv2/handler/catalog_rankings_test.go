package handler

import (
	"testing"

	catsvc "api/internal/platform/catalog/service"

	"github.com/stretchr/testify/require"
)

func TestParseRankingsDefaults(t *testing.T) {
	f, p := parseRankings(&listRankingsInput{})
	require.Nil(t, p)
	require.Equal(t, catsvc.RankByConsensus, f.By)
	require.Equal(t, catsvc.RankMetricLower, f.Metric)
	require.False(t, f.Asc)

	f, p = parseRankings(&listRankingsInput{Order: "asc"})
	require.Nil(t, p)
	require.Equal(t, catsvc.RankMetricUpper, f.Metric, "a worst-of list is bad with confidence")

	f, p = parseRankings(&listRankingsInput{Order: "asc", Metric: "mean"})
	require.Nil(t, p)
	require.Equal(t, catsvc.RankMetricMean, f.Metric)
}

func TestParseRankingsMetricVocabularyFollowsBy(t *testing.T) {
	_, p := parseRankings(&listRankingsInput{Metric: "median"})
	require.NotNil(t, p, "median is a per-source statistic")
	f, p := parseRankings(&listRankingsInput{By: "erogamescape", Metric: "median"})
	require.Nil(t, p)
	require.Equal(t, "erogamescape", f.By)
	_, p = parseRankings(&listRankingsInput{By: "erogamescape", Metric: "divergence"})
	require.NotNil(t, p)
	_, p = parseRankings(&listRankingsInput{By: "metacritic"})
	require.NotNil(t, p)
}

func TestParseRankingsSources(t *testing.T) {
	f, p := parseRankings(&listRankingsInput{Sources: "erogamescape, bangumi,erogamescape", MinSources: "2"})
	require.Nil(t, p)
	require.Equal(t, []string{"erogamescape", "bangumi"}, f.Sources)
	require.Equal(t, 2, f.MinSources)

	f, p = parseRankings(&listRankingsInput{Sources: "vndb,bangumi,dlsite,erogamescape,nextmoe,howlongtobeat"})
	require.Nil(t, p)
	require.Nil(t, f.Sources, "every source is the stored consensus, not a scripted subset")

	_, p = parseRankings(&listRankingsInput{By: "vndb", Sources: "vndb"})
	require.NotNil(t, p)
	_, p = parseRankings(&listRankingsInput{By: "vndb", MinSources: "2"})
	require.NotNil(t, p)
	_, p = parseRankings(&listRankingsInput{MinSources: "7"})
	require.NotNil(t, p)
}

func TestParseRankingsMinVotes(t *testing.T) {
	f, p := parseRankings(&listRankingsInput{MinVotes: "100, erogamescape:50,bangumi:20"})
	require.Nil(t, p)
	require.Equal(t, 100, f.TotalVotes, "a bare floor on the consensus is a total")
	require.Equal(t, map[string]int{"erogamescape": 50, "bangumi": 20}, f.MinVotes)

	f, p = parseRankings(&listRankingsInput{By: "dlsite", MinVotes: "30"})
	require.Nil(t, p)
	require.Equal(t, map[string]int{"dlsite": 30}, f.MinVotes)
	require.Zero(t, f.TotalVotes)

	f, p = parseRankings(&listRankingsInput{MinVotes: "5", MaxVotes: "50,erogamescape:30"})
	require.Nil(t, p)
	require.Equal(t, 5, f.TotalVotes)
	require.Equal(t, 50, f.TotalMax)
	require.Equal(t, map[string]int{"erogamescape": 30}, f.MaxVotes)

	for _, bad := range []string{"0", "x", "steam:3", "vndb:"} {
		_, p = parseRankings(&listRankingsInput{MinVotes: bad})
		require.NotNil(t, p, bad)
	}
}

func TestParseRankingsFilters(t *testing.T) {
	f, p := parseRankings(&listRankingsInput{
		Circulation: "doujin", Platform: "win, swi,win", PlaytimeMin: "60", PlaytimeMax: "600",
		ScoreMin: "55.5", ScoreMax: "80",
	})
	require.Nil(t, p)
	require.Equal(t, "doujin", f.Circulation)
	require.Equal(t, []string{"win", "swi"}, f.Platforms)
	require.Equal(t, 60, f.PlaytimeMin)
	require.Equal(t, 600, f.PlaytimeMax)
	require.Equal(t, 55.5, *f.Min)
	require.Equal(t, 80.0, *f.Max)

	for _, in := range []listRankingsInput{
		{Circulation: "indie"}, {PlaytimeMin: "-1"}, {ScoreMin: "NaN"}, {ScoreMax: "high"},
	} {
		_, p = parseRankings(&in)
		require.NotNil(t, p, "%+v", in)
	}
}

func TestApplyYear(t *testing.T) {
	var f worksFilter
	require.Nil(t, applyYear(&listRankingsInput{Year: "2020"}, &f))
	require.Equal(t, int64(20200101), f.ReleasedAfter)
	require.Equal(t, int64(20201231), f.ReleasedBefore)

	f = worksFilter{}
	require.Nil(t, applyYear(&listRankingsInput{Year: "2015-2018"}, &f))
	require.Equal(t, int64(20150101), f.ReleasedAfter)
	require.Equal(t, int64(20181231), f.ReleasedBefore)

	require.NotNil(t, applyYear(&listRankingsInput{Year: "2018-2015"}, &f))
	require.NotNil(t, applyYear(&listRankingsInput{Year: "1800"}, &f))
	require.NotNil(t, applyYear(&listRankingsInput{Year: "2020", ReleasedAfter: "2020-01-01"}, &f))
}

func TestRankingUnit(t *testing.T) {
	require.Equal(t, "deviation", rankingUnit(catsvc.RankByConsensus, catsvc.RankMetricLower))
	require.Equal(t, "native", rankingUnit("vndb", catsvc.RankMetricMedian))
	require.Equal(t, "percentile", rankingUnit("vndb", catsvc.RankMetricPercentile))
	require.Equal(t, "points", rankingUnit(catsvc.RankByConsensus, catsvc.RankMetricDivergence))
	require.Equal(t, "votes", rankingUnit("bangumi", catsvc.RankMetricVotes))
	require.Equal(t, "count", rankingUnit(catsvc.RankByConsensus, catsvc.RankMetricSources))
}
