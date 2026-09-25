package search

import (
	"strconv"
	"strings"
)

type CharactersResult struct {
	IDs   []int64
	Total int64
}

func CharacterDocID(id int64) string { return "c" + strconv.FormatInt(id, 10) }

func CharacterDocIDToID(docID string) (int64, bool) {
	if !strings.HasPrefix(docID, "c") {
		return 0, false
	}
	id, err := strconv.ParseInt(docID[1:], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
