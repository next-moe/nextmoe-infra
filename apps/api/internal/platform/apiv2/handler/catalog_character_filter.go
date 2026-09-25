package handler

import (
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/apiv2/vocab"
)

type characterFilter struct {
	Q        string
	TraitIDs []int64
	MatchAny bool
	Genders  []int16
}

func parseCharacterFilter(in *listCharactersInput) (characterFilter, *problem.Problem) {
	if in == nil {
		return characterFilter{}, nil
	}
	f := characterFilter{Q: strings.TrimSpace(in.Q)}
	ids, err := idList(in.TraitID, "trait_id", 10)
	if err != nil {
		return characterFilter{}, err
	}
	f.TraitIDs = uniqueInt64(ids)
	match := strings.TrimSpace(in.TraitMatch)
	if match != "" {
		switch match {
		case "all":
		case "any":
			f.MatchAny = true
		default:
			return characterFilter{}, closedParam("trait_match", "all, any")
		}
	}
	tokens, err := closedCSV(in.Gender, "gender", vocab.Tokens("gender"))
	if err != nil {
		return characterFilter{}, err
	}
	for _, tok := range tokens {
		code, ok := repr.GenderFromKey(tok)
		if !ok {
			return characterFilter{}, closedParam("gender", strings.Join(vocab.Tokens("gender"), ", "))
		}
		f.Genders = append(f.Genders, code)
	}
	return f, nil
}

func characterSearchSort(sort string) bool {
	switch sort {
	case "popularity", "relevance", "newest":
		return true
	default:
		return false
	}
}

func characterIndexLane(q collect.Query, nameQ string) bool {
	return strings.TrimSpace(nameQ) != "" || q.Page > 0 || characterSearchSort(q.Sort)
}

func uniqueInt64(ids []int64) []int64 {
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
