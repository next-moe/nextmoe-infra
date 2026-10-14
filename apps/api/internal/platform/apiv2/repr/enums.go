package repr

import "api/internal/platform/catalog/model"

const C5 = "Must not be used as a discriminant."

func ptr(s string) *string { return &s }

func Medium(v int16) (string, bool) {
	switch v {
	case 1:
		return "galgame", true
	case 2:
		return "manga", true
	case 3:
		return "novel", true
	case 4:
		return "anime", true
	case 5:
		return "asmr", true
	case 6:
		return "doujin_game", true
	case 7:
		return "music", true
	default:
		return "", false
	}
}

func MediumFromKey(key string) (string, bool) {
	switch key {
	case "galgame", "manga", "novel", "anime", "asmr", "doujin_game", "music":
		return key, true
	default:
		return "", false
	}
}

func ContentRating(v int16) (string, bool) {
	switch v {
	case model.ContentRatingAllAges:
		return "all_ages", true
	case model.ContentRatingSensitive:
		return "sensitive", true
	case model.ContentRatingR18:
		return "r18", true
	default:
		return "", false
	}
}

func TitleKindFromKey(key string) (string, bool) {
	switch key {
	case "official", "alias", "abbreviation":
		return key, true
	default:
		return "", false
	}
}

func TitleKind(v int16) (string, bool) {
	switch v {
	case model.WorkTitleKindOfficial:
		return "official", true
	case model.WorkTitleKindAlias:
		return "alias", true
	case model.WorkTitleKindAbbreviation:
		return "abbreviation", true
	default:
		return "", false
	}
}

func RosterRoleFromKey(key string) (string, bool) {
	switch key {
	case "unknown", "main", "secondary", "appears":
		return key, true
	default:
		return "", false
	}
}

func AttributionRoleFromKey(key string) (string, bool) {
	switch key {
	case "circle", "publisher", "developer", "brand":
		return key, true
	default:
		return "", false
	}
}

func CompanyKind(v int16) (string, bool) {
	switch v {
	case model.LabelKindGameBrand:
		return "game_brand", true
	case model.LabelKindBunko:
		return "bunko", true
	case model.LabelKindPublisher:
		return "publisher", true
	case model.LabelKindAnimeStudio:
		return "anime_studio", true
	case model.LabelKindDoujinCircle:
		return "doujin_circle", true
	case model.LabelKindGroup:
		return "group", true
	default:
		return "", false
	}
}

func CompanyKindFromKey(key string) (string, bool) {
	switch key {
	case "game_brand", "bunko", "publisher", "anime_studio", "doujin_circle", "group":
		return key, true
	default:
		return "", false
	}
}

func TagTier(v int16) (string, bool) {
	switch v {
	case model.TagTierCore:
		return "core", true
	case model.TagTierLongtail:
		return "longtail", true
	case model.TagTierHidden:
		return "hidden", true
	default:
		return "", false
	}
}

func TagKind(v int16) (string, bool) {
	switch v {
	case model.TagKindContent:
		return "content", true
	case model.TagKindMeta:
		return "meta", true
	default:
		return "", false
	}
}

func ReleaseKindFromKey(key string) (string, bool) {
	switch key {
	case "default", "digital", "physical", "trial", "patch":
		return key, true
	default:
		return "", false
	}
}

func Gender(v *int16) (*string, bool) {
	if v == nil {
		return nil, true
	}
	switch *v {
	case model.GenderMale:
		return ptr("male"), true
	case model.GenderFemale:
		return ptr("female"), true
	case model.GenderOther:
		return ptr("other"), true
	default:
		return nil, false
	}
}

func GenderFromKey(key string) (int16, bool) {
	switch key {
	case "male":
		return model.GenderMale, true
	case "female":
		return model.GenderFemale, true
	case "other":
		return model.GenderOther, true
	default:
		return 0, false
	}
}

func BloodType(v *int16) (*string, bool) {
	if v == nil {
		return nil, true
	}
	switch *v {
	case model.BloodTypeA:
		return ptr("a"), true
	case model.BloodTypeB:
		return ptr("b"), true
	case model.BloodTypeAB:
		return ptr("ab"), true
	case model.BloodTypeO:
		return ptr("o"), true
	default:
		return nil, false
	}
}

func Sexual(v *int16) (*string, bool) {
	if v == nil {
		return nil, true
	}
	switch *v {
	case 0:
		return ptr("safe"), true
	case 1:
		return ptr("suggestive"), true
	case 2:
		return ptr("explicit"), true
	default:
		return nil, false
	}
}

func Violence(v *int16) (*string, bool) {
	if v == nil {
		return nil, true
	}
	switch *v {
	case 0:
		return ptr("tame"), true
	case 1:
		return ptr("violent"), true
	case 2:
		return ptr("brutal"), true
	default:
		return nil, false
	}
}

func Spoiler(v int16) (string, bool) {
	switch v {
	case 0:
		return "none", true
	case 1:
		return "minor", true
	case 2:
		return "major", true
	default:
		return "", false
	}
}

func SpoilerFromKey(key string) (int16, bool) {
	switch key {
	case "none":
		return 0, true
	case "minor":
		return 1, true
	case "major":
		return 2, true
	default:
		return 0, false
	}
}
