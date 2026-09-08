package collect

var WorkInclude = []string{
	"titles", "refs", "relations", "credits", "releases", "popularity",
	"ratings", "tags", "playtimes", "series", "platforms", "intros",
	"covers", "screenshots", "characters", "companies", "engines", "links",
}

var WorkFullSet = []string{
	"titles", "refs", "intros", "covers", "companies", "tags",
	"releases", "ratings", "relations", "engines", "series", "links",
}

// WorkListInclude is the works LIST vocabulary, and it is deliberately not
// WorkInclude. The list and the detail shared one Spec, so the list declared
// all eighteen detail tokens, validated them, and then answered four blocks:
// include=tags,credits was a 200 with no tags and no credits, ten more tokens
// did nothing at all, and view=full unioned twelve tokens to emit four. A
// consumer probed for a 400 first, got none, and shipped a per-work detail read
// on its hottest page to recover what the list had accepted an ask for.
//
// titles and covers are narrower here than on the detail face, and that is the
// contract rather than a gap: titles elects latin/localized, covers elects the
// two cover slots that carry the sexual grading of the base cover. Both are
// load-bearing for consumers in exactly that shape — emitting the detail face's
// full titles[] and covers[] arrays on a 100-work batch would be a payload
// regression, not a fix. The unbounded per-work galleries stay on
// works/{id}/covers and the other sub-resources.
var WorkListInclude = []string{
	"titles", "refs", "intros", "covers", "companies", "ratings", "tags", "credits",
}

// credits is out of the list FullSet for the reason it is out of WorkFullSet:
// it is an explicit ask, not part of "everything". Every other list-capable
// token rides view=full.
var WorkListFullSet = []string{
	"titles", "refs", "intros", "covers", "companies", "ratings", "tags",
}

var WorkBasicFields = []string{
	"object", "id", "medium", "display_name", "latin", "localized", "olang",
	"content_rating", "release_date", "release_date_precision", "release_status",
	"cover", "banner", "claim", "created_at", "updated_at",
}

var WorkSort = []string{"id", "updated", "relevance", "released_desc", "released_asc", "popularity"}

var WorkFacets = []string{
	"tag_id", "company_id", "olang", "content_rating", "medium", "platform",
}

func SearchSort(sort string) bool {
	switch sort {
	case "relevance", "released_desc", "released_asc", "popularity":
		return true
	default:
		return false
	}
}

func WorkSpec() Spec {
	fields := append([]string{}, WorkBasicFields...)
	fields = append(fields, WorkInclude...)
	return Spec{
		Sort:    WorkSort,
		Include: WorkInclude,
		FullSet: WorkFullSet,
		Fields:  fields,
		Facets:  WorkFacets,
	}
}

func WorkListSpec() Spec {
	fields := append([]string{}, WorkBasicFields...)
	fields = append(fields, WorkListInclude...)
	return Spec{
		Sort:    WorkSort,
		Include: WorkListInclude,
		FullSet: WorkListFullSet,
		Fields:  fields,
		Facets:  WorkFacets,
	}
}

func VocabSpec() Spec {
	return Spec{
		Sort:    []string{"name"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "name", "closed", "values"},
		Facets:  []string{},
	}
}

func ProblemSpec() Spec {
	return Spec{
		Sort:    []string{"code"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "code", "domain", "status", "type", "title", "description"},
		Facets:  []string{},
	}
}

// NewsSpec is NoBatch for the same reason the three below are, and it was the
// one deviation 82 missed: /v2/news declares ids= and refs=, parses them, and
// has no hydration lane, so q.Batch zeroed the limit and
// GET /v2/news?ids=<an id that exists> answered 200 with an empty items[], no
// missing[] and no cursor.
func NewsSpec() Spec {
	return Spec{
		Sort:    []string{"published"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "title", "summary", "source", "source_url", "banner", "published_at"},
		Facets:  []string{},
		NoBatch: true,
	}
}

func NewsSubmissionSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields: []string{
			"object", "id", "source", "lane", "status", "title", "summary",
			"source_url", "banner_hash", "published_at", "work_ids",
		},
		NoBatch: true,
	}
}

var companyInclude = []string{"aliases", "logo", "intros", "links"}

func CompanySpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: companyInclude,
		FullSet: companyInclude,
		Fields: []string{
			"object", "id", "display_name", "latin", "lang", "localized", "company_kind", "work_count",
			"aliases", "logo", "intros", "links",
		},
	}
}

var creditNameInclude = []string{"aliases", "photo", "siblings", "intros", "links", "refs"}

func CreditNameSpec() Spec {
	fields := []string{
		"object", "id", "display_name", "latin", "lang", "localized", "person_id",
		"gender", "birth_year", "birth_month", "birth_day",
	}
	fields = append(fields, creditNameInclude...)
	return Spec{
		Sort:    []string{"id"},
		Include: creditNameInclude,
		FullSet: creditNameInclude,
		Fields:  fields,
	}
}

func CharacterSpec() Spec {
	full := []string{
		"gender", "birthday", "height_cm", "weight_kg", "measurements", "blood_type", "instance_of_id",
		"image", "figure", "traits", "aliases", "intros", "refs",
	}
	fields := []string{"object", "id", "display_name", "latin", "lang", "localized"}
	fields = append(fields, full...)
	return Spec{
		Sort:    []string{"id"},
		Include: append([]string{}, full...),
		FullSet: append([]string{}, full...),
		Fields:  fields,
	}
}

func PersonSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "display_name", "primary_credit_name_id", "gender"},
	}
}

func TraitSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "display_name", "name_zh", "vndb_tid", "is_sexual"},
	}
}

func PlaytimeSpec() Spec {
	return Spec{
		Sort:    []string{"updated"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "work_id", "minutes"},
		NoBatch: true,
	}
}

func FolderSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "owner_uid", "name", "description", "visibility", "is_default", "item_count", "created_at", "updated_at"},
		NoBatch: true,
	}
}

func FolderItemSpec() Spec {
	return Spec{
		Sort:    []string{"updated"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "folder_id", "work_id", "created_at", "updated_at"},
		NoBatch: true,
	}
}

func CoverVoteSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "cover_id", "work_id", "vote"},
	}
}

func ClaimSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields: []string{
			"object", "id", "state", "display_name", "site", "product_work_id",
			"last_event", "first_acted_at", "acted_count",
		},
		NoBatch: true,
	}
}

func NameCreditSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "work", "roles"},
	}
}

var tagInclude = []string{"intros"}

func TagSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: tagInclude,
		FullSet: tagInclude,
		Fields: []string{
			"object", "id", "display_name", "tier", "tag_kind", "work_count", "is_sexual", "intros",
		},
	}
}

var seriesInclude = []string{"intros", "refs"}

func SeriesSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: seriesInclude,
		FullSet: seriesInclude,
		Fields: []string{
			"object", "id", "display_name", "work_count", "has_nsfw", "intros", "refs",
		},
	}
}

func EngineSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "display_name", "work_count", "description", "aliases"},
	}
}

func RoleSpec() Spec {
	return Spec{
		Sort:    []string{"id"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "key", "category", "display_name", "localized", "deprecated"},
	}
}

func WorkSubSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id"},
	}
}

func ReleaseSpec() Spec {
	return Spec{
		Sort:    []string{"date_desc", "date_asc"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "id", "work_id", "release_kind", "date", "title", "lang", "platform", "platforms", "refs"},
	}
}

func ChangesSpec() Spec {
	return Spec{
		Sort:    []string{"updated"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "target_object", "id", "updated_at"},
	}
}

func RedirectsSpec() Spec {
	return Spec{
		Sort:    []string{"merged_at"},
		Include: []string{},
		FullSet: []string{},
		Fields:  []string{"object", "target_object", "old_id", "current_id", "merged_at"},
	}
}

func SearchSpec() Spec {
	return Spec{
		Include: []string{},
		FullSet: []string{},
		Fields: []string{
			"object", "target_object", "id", "display_name", "latin", "localized",
			"sources", "content_rating", "tier", "tag_kind", "is_sexual",
		},
	}
}

func CalendarSpec() Spec {
	fields := append([]string{}, WorkBasicFields...)
	fields = append(fields, WorkListInclude...)
	return Spec{
		Sort:    []string{"id"},
		Include: WorkListInclude,
		FullSet: WorkListFullSet,
		Fields:  fields,
	}
}

// ObjectListSpec answers the vocabulary of the object's COLLECTION face, which
// is the one a caller building a list request needs. Only work has a narrower
// list face; for every other object the list and the detail share a Spec.
func ObjectListSpec(object string) (Spec, bool) {
	if object == "work" {
		return WorkListSpec(), true
	}
	return ObjectSpec(object)
}

func ObjectSpec(object string) (Spec, bool) {
	switch object {
	case "work":
		return WorkSpec(), true
	case "company":
		return CompanySpec(), true
	case "character":
		return CharacterSpec(), true
	case "release":
		return ReleaseSpec(), true
	case "tag":
		return TagSpec(), true
	case "engine":
		return EngineSpec(), true
	case "series":
		return SeriesSpec(), true
	case "person":
		return PersonSpec(), true
	case "trait":
		return TraitSpec(), true
	default:
		return Spec{}, false
	}
}
