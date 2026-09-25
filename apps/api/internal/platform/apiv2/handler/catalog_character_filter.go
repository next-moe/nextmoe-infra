package handler

import (
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/apiv2/vocab"
)

type characterFilter struct {
	TraitIDs []int64
	MatchAny bool
	Genders  []int16
}

func parseCharacterFilter(in *listCharactersInput) (characterFilter, *problem.Problem) {
	if in == nil {
		return characterFilter{}, nil
	}
	ids, err := idList(in.TraitID, "trait_id", 10)
	if err != nil {
		return characterFilter{}, err
	}
	ids = uniqueInt64(ids)
	match := strings.TrimSpace(in.TraitMatch)
	matchAny := false
	if match != "" {
		switch match {
		case "all":
		case "any":
			matchAny = true
		default:
			return characterFilter{}, closedParam("trait_match", "all, any")
		}
	}
	tokens, err := closedCSV(in.Gender, "gender", vocab.Tokens("gender"))
	if err != nil {
		return characterFilter{}, err
	}
	var genders []int16
	for _, tok := range tokens {
		code, ok := repr.GenderFromKey(tok)
		if !ok {
			return characterFilter{}, closedParam("gender", strings.Join(vocab.Tokens("gender"), ", "))
		}
		genders = append(genders, code)
	}
	if len(ids) == 0 && len(genders) == 0 {
		return characterFilter{}, nil
	}
	return characterFilter{TraitIDs: ids, MatchAny: matchAny, Genders: genders}, nil
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
