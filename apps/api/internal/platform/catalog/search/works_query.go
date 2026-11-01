package search

import (
	"strconv"
	"strings"
)

type WorksResult struct {
	IDs    []int64
	Total  int64
	Facets map[string]map[string]int64
}

type RankingResult struct {
	IDs    []int64
	Values []float64
	Total  int64
	Facets map[string]map[string]int64
}

func WorkDocID(workID int64) string { return "w" + strconv.FormatInt(workID, 10) }

func WorkDocIDToWorkID(docID string) (int64, bool) {
	if !strings.HasPrefix(docID, "w") {
		return 0, false
	}
	id, err := strconv.ParseInt(docID[1:], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
