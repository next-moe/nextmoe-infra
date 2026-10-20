package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

const maxNameKeys = 500

type listNamesInput struct {
	Keys string `query:"keys" maxLength:"16384" doc:"Comma-separated family:id, max 500, e.g. tag:12,company:3. Families: work, tag, company, engine, series, character, credit_name, person, role, trait, release. Duplicates collapse."`
	NSFW string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false. Passed to each family's ids= batch lane unchanged."`
}

type listNamesOutput struct {
	Body repr.List[repr.RefName]
}

type refLabel struct {
	name, zh string
}

type nameLookup func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error)

func registerCatalogNames(api huma.API, works WorksFunc, cat *Catalog) {
	lookups := nameLookups(works, cat)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogNames",
		Method:      http.MethodGet,
		Path:        "/v2/catalog/names",
		Summary:     "Display names across families",
		Description: "Resolves display names for entities of several families in one call: each family's names come from that family's ids= batch lane under the same nsfw gate, so this face answers exactly what those lanes would. " +
			"Items follow the order of keys=. Keys that name no visible entity land in missing[]. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listCatalogNames(lookups))
}

func listCatalogNames(lookups map[string]nameLookup) func(context.Context, *listNamesInput) (*listNamesOutput, error) {
	return func(ctx context.Context, in *listNamesInput) (*listNamesOutput, error) {
		if in == nil {
			in = &listNamesInput{}
		}
		if in.NSFW != "" {
			if _, perr := parse.Bool(in.NSFW, "nsfw"); perr != nil {
				return nil, withIdent(ctx, perr)
			}
		}
		keys, perr := splitNameKeys(in.Keys, lookups)
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		byFamily := map[string][]string{}
		for _, k := range keys {
			byFamily[k.family] = append(byFamily[k.family], k.id)
		}
		families := make([]string, 0, len(byFamily))
		for f := range byFamily {
			families = append(families, f)
		}
		sort.Strings(families)
		names := map[string]refLabel{}
		for _, family := range families {
			ids := byFamily[family]
			for start := 0; start < len(ids); start += collect.MaxBatchItems {
				end := min(start+collect.MaxBatchItems, len(ids))
				found, err := lookups[family](ctx, strings.Join(ids[start:end], ","), in.NSFW)
				if err != nil {
					return nil, err
				}
				for id, name := range found {
					names[family+":"+id] = name
				}
			}
		}
		items := make([]repr.RefName, 0, len(keys))
		missing := []string{}
		for _, k := range keys {
			label, ok := names[k.key]
			if !ok {
				missing = append(missing, k.key)
				continue
			}
			item := repr.RefName{
				Object: "ref_name", Key: k.key, Family: k.family, ID: k.id, DisplayName: label.name,
			}
			if label.zh != "" {
				item.NameZh = &label.zh
			}
			items = append(items, item)
		}
		return &listNamesOutput{Body: repr.List[repr.RefName]{Object: "list", Items: items, Missing: &missing}}, nil
	}
}

type nameKey struct {
	key, family, id string
}

func splitNameKeys(raw string, lookups map[string]nameLookup) ([]nameKey, *problem.Problem) {
	seen := map[string]bool{}
	out := []nameKey{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" || seen[token] {
			continue
		}
		family, id, ok := strings.Cut(token, ":")
		if _, known := lookups[family]; !ok || !known || !isDecimalID(id) {
			p := problem.New(problem.CodeInvalidParameter, "", "", "keys is invalid.")
			p.Errors = []problem.FieldError{{
				Parameter: "keys",
				Reason:    problem.ReasonInvalidFormat,
				Detail:    "expected family:id with a known family and a decimal id, got " + token,
			}}
			return nil, p
		}
		seen[token] = true
		out = append(out, nameKey{key: token, family: family, id: id})
	}
	if len(out) > maxNameKeys {
		p := problem.New(problem.CodeTooManyIDs, "", "", "keys= accepts at most 500 values.")
		p.Errors = []problem.FieldError{{
			Parameter: "keys",
			Reason:    problem.ReasonTooManyItems,
			Detail:    "maximum 500",
			Params:    &problem.FieldParams{MaxItems: problem.Ptr(maxNameKeys)},
		}}
		return nil, p
	}
	return out, nil
}

func isDecimalID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func namesOf[T any](items []T, pick func(T) (string, refLabel)) map[string]refLabel {
	out := make(map[string]refLabel, len(items))
	for _, item := range items {
		id, label := pick(item)
		out[id] = label
	}
	return out
}

func zhOf(localized map[string]repr.LocalizedText) string {
	for _, lang := range []string{"zh-Hans", "zh-Hant", "zh"} {
		if v, ok := localized[lang]; ok && v.Value != "" {
			return v.Value
		}
	}
	return ""
}

func nameLookups(works WorksFunc, cat *Catalog) map[string]nameLookup {
	batch := func(ids, nsfw string) CollectionInput { return CollectionInput{IDs: ids, NSFW: nsfw} }
	return map[string]nameLookup{
		"work": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listWorks(works, cat)(ctx, &listWorksInput{IDs: ids, NSFW: nsfw})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(w repr.Work) (string, refLabel) {
				return w.ID, refLabel{name: w.DisplayName, zh: zhOf(w.Localized)}
			}), nil
		},
		"tag": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogTags(cat)(ctx, &listTagsInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Tag) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName}
			}), nil
		},
		"company": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogCompanies(cat)(ctx, &listCompaniesInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Company) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName, zh: zhOf(v.Localized)}
			}), nil
		},
		"engine": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			in := batch(ids, nsfw)
			out, err := listCatalogEngines(cat)(ctx, &in)
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Engine) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName}
			}), nil
		},
		"series": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			in := batch(ids, nsfw)
			out, err := listCatalogSeries(cat)(ctx, &in)
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Series) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName}
			}), nil
		},
		"character": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogCharacters(cat)(ctx, &listCharactersInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Character) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName, zh: zhOf(v.Localized)}
			}), nil
		},
		"credit_name": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogCreditNames(cat)(ctx, &listCreditNamesInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.CreditName) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName, zh: zhOf(v.Localized)}
			}), nil
		},
		"person": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogPersons(cat)(ctx, &listPersonsInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Person) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName}
			}), nil
		},
		"role": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			in := batch(ids, nsfw)
			out, err := listCatalogRoles(cat)(ctx, &in)
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Role) (string, refLabel) {
				return v.ID, refLabel{name: v.DisplayName, zh: zhOf(v.Localized)}
			}), nil
		},
		"trait": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogTraits(cat)(ctx, &listTraitsInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Trait) (string, refLabel) {
				zh := v.NameZh
				if zh == "" {
					zh = zhOf(v.Localized)
				}
				return v.ID, refLabel{name: v.DisplayName, zh: zh}
			}), nil
		},
		"release": func(ctx context.Context, ids, nsfw string) (map[string]refLabel, error) {
			out, err := listCatalogReleases(cat)(ctx, &listReleasesInput{CollectionInput: batch(ids, nsfw)})
			if err != nil {
				return nil, err
			}
			return namesOf(out.Body.Items, func(v repr.Release) (string, refLabel) {
				if v.Title == nil {
					return v.ID, refLabel{}
				}
				return v.ID, refLabel{name: *v.Title}
			}), nil
		},
	}
}
