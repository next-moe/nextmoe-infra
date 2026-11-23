package registry

type RatingDimension struct {
	Key    string `json:"key"`
	NameZH string `json:"name_zh"`
	NameEN string `json:"name_en"`
}

const (
	RatingScoreMin = 1
	RatingScoreMax = 10
)

// RatingDimensions are each medium's sub-scores (plan 03 §7, D27); the
// overall score, recommendation, spoiler level and short review are
// media-neutral. game keeps its eight in the legacy columns, written through
// v2, until P4 moves the game module.
var RatingDimensions = map[string][]RatingDimension{
	"novel": {
		{"story", "剧情", "Story"},
		{"character", "角色", "Characters"},
		{"writing", "文笔", "Writing"},
		{"setting", "世界观", "Setting"},
		{"illustration", "插画", "Illustrations"},
		{"pacing", "节奏", "Pacing"},
	},
	"manga": {
		{"story", "剧情", "Story"},
		{"character", "角色", "Characters"},
		{"art", "作画", "Art"},
		{"paneling", "分镜", "Paneling"},
		{"setting", "世界观", "Setting"},
		{"pacing", "节奏", "Pacing"},
	},
}

func HasRatingDimension(medium, key string) bool {
	for _, d := range RatingDimensions[medium] {
		if d.Key == key {
			return true
		}
	}
	return false
}
