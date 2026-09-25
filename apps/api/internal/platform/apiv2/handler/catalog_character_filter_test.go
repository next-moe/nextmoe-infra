package handler

import (
	"slices"
	"testing"

	"api/internal/platform/apiv2/problem"
)

func TestParseCharacterFilter(t *testing.T) {
	tests := []struct {
		name    string
		in      *listCharactersInput
		want    characterFilter
		code    string
		param   string
		allowed []string
	}{
		{name: "empty", in: &listCharactersInput{}},
		{
			name: "deduped all",
			in:   &listCharactersInput{TraitID: "1,2,2"},
			want: characterFilter{TraitIDs: []int64{1, 2}},
		},
		{
			name: "match any",
			in:   &listCharactersInput{TraitID: "1", TraitMatch: "any"},
			want: characterFilter{TraitIDs: []int64{1}, MatchAny: true},
		},
		{
			name: "gender list",
			in:   &listCharactersInput{Gender: "female,male,female"},
			want: characterFilter{Genders: []int16{2, 1}},
		},
		{
			name:    "gender bogus",
			in:      &listCharactersInput{Gender: "bogus"},
			code:    problem.CodeUnknownEnumValue,
			param:   "gender",
			allowed: []string{"male", "female", "other"},
		},
		{
			name:    "bogus match",
			in:      &listCharactersInput{TraitMatch: "bogus"},
			code:    problem.CodeUnknownEnumValue,
			param:   "trait_match",
			allowed: []string{"all", "any"},
		},
		{
			name:  "eleven ids",
			in:    &listCharactersInput{TraitID: "1,2,3,4,5,6,7,8,9,10,11"},
			code:  problem.CodeInvalidParameter,
			param: "trait_id",
		},
		{
			name:  "non-numeric",
			in:    &listCharactersInput{TraitID: "abc"},
			code:  problem.CodeInvalidParameter,
			param: "trait_id",
		},
		{name: "match without id", in: &listCharactersInput{TraitMatch: "any"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCharacterFilter(tc.in)
			if tc.code != "" {
				if err == nil || err.Code != tc.code {
					t.Fatalf("code: %v", err)
				}
				if len(err.Errors) == 0 || err.Errors[0].Parameter != tc.param {
					t.Fatalf("param: %+v", err)
				}
				if tc.allowed != nil {
					if err.Errors[0].Params == nil || err.Errors[0].Params.Allowed == nil {
						t.Fatalf("allowed: %+v", err)
					}
					if !slices.Equal(*err.Errors[0].Params.Allowed, tc.allowed) {
						t.Fatalf("allowed %v", *err.Errors[0].Params.Allowed)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.TraitIDs, tc.want.TraitIDs) || got.MatchAny != tc.want.MatchAny || !slices.Equal(got.Genders, tc.want.Genders) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
