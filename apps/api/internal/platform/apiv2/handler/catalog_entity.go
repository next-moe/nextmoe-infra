package handler

import (
	"context"
	"slices"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) GetCreditName(ctx context.Context, id int64, nsfw bool, include []string) (repr.CreditName, error) {
	if c == nil || c.Public == nil {
		return repr.CreditName{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Name(ctx, id, false, nsfw, 0, 0)
	if err != nil {
		return repr.CreditName{}, err
	}
	if !found {
		return repr.CreditName{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeCreditName, "credit_name", id)
	}
	return creditNameFromDetail(rec, include, c.Public.ImageURL(rec.PhotoHash)), nil
}

func (c *Catalog) GetCharacter(ctx context.Context, id int64, nsfw bool, spoiler int16, include []string) (repr.Character, error) {
	if c == nil || c.Public == nil {
		return repr.Character{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Character(ctx, id, false, nsfw, spoiler, 0, 0)
	if err != nil {
		return repr.Character{}, err
	}
	if !found {
		return repr.Character{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeCharacter, "character", id)
	}
	out := repr.Character{
		Object: "character", ID: repr.ID(rec.ID), DisplayName: rec.DisplayName,
		Latin: optString(rec.Latin), Lang: optString(rec.Lang), Localized: localizedFrom(rec.Localized),
	}
	if characterWantsAttrs(include) {
		attrs, ok, aerr := c.Public.CharacterAttributes(ctx, id)
		if aerr != nil {
			return repr.Character{}, aerr
		}
		if ok {
			attachCharacterAttrs(&out, attrs)
		}
	}
	attachCharacterBlocks(&out, rec, include)
	if characterWantsWorkCount(include) {
		counts, cerr := c.Public.CharacterWorkCounts(ctx, []int64{id}, nsfw)
		if cerr != nil {
			return repr.Character{}, cerr
		}
		n := counts[id]
		out.WorkCount = &n
	}
	return out, nil
}

func (c *Catalog) GetPerson(ctx context.Context, id int64) (repr.Person, error) {
	if c == nil || c.Public == nil {
		return repr.Person{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Person(ctx, id)
	if err != nil {
		return repr.Person{}, err
	}
	if !found {
		return repr.Person{}, c.mergedOrNotFound(ctx, catmodel.EntityTypePerson, "person", id)
	}
	var primary *string
	if rec.PrimaryCreditNameID != nil && *rec.PrimaryCreditNameID > 0 {
		s := repr.ID(*rec.PrimaryCreditNameID)
		primary = &s
	}
	g, _ := repr.Gender(rec.Gender)
	return repr.Person{
		Object: "person", ID: repr.ID(rec.ID), DisplayName: rec.DisplayName,
		PrimaryCreditNameID: primary, Gender: g,
	}, nil
}

func (c *Catalog) GetTrait(ctx context.Context, id int64, nsfw bool, include []string) (repr.Trait, error) {
	if c == nil || c.Public == nil {
		return repr.Trait{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Trait(ctx, id, nsfw, include)
	if err != nil {
		return repr.Trait{}, err
	}
	if !found {
		return repr.Trait{}, problem.New(problem.CodeNotFound, "", "", "No trait with this id.")
	}
	out := traitFromRow(rec, include)
	if slices.Contains(include, "character_count") {
		counts, cerr := characterTraitCounts(c, ctx, nsfw)
		if cerr != nil {
			return repr.Trait{}, traitCountUnavailable(cerr)
		}
		n := 0
		if counts != nil {
			n = int(counts[id])
		}
		out.CharacterCount = &n
	}
	return out, nil
}

func (c *Catalog) GetCompany(ctx context.Context, id int64, nsfw bool, include []string) (repr.Company, error) {
	if c == nil || c.Public == nil {
		return repr.Company{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Label(ctx, id, false, nsfw, 0, 0)
	if err != nil {
		return repr.Company{}, err
	}
	if !found {
		return repr.Company{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeLabel, "company", id)
	}
	return companyFromDetail(rec, include, c.Public.ImageURL(rec.LogoHash)), nil
}

func (c *Catalog) GetTag(ctx context.Context, id int64, nsfw bool, include []string) (repr.Tag, error) {
	if c == nil || c.Public == nil {
		return repr.Tag{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.TagDetail(ctx, id, false, nsfw, 0, 0)
	if err != nil {
		return repr.Tag{}, err
	}
	if !found {
		return repr.Tag{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeTag, "tag", id)
	}
	return tagFromDetail(rec, include), nil
}

func (c *Catalog) GetEngine(ctx context.Context, id int64, nsfw bool) (repr.Engine, error) {
	if c == nil || c.Public == nil {
		return repr.Engine{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.EngineDetail(ctx, id, nsfw)
	if err != nil {
		return repr.Engine{}, err
	}
	if !found {
		return repr.Engine{}, problem.New(problem.CodeNotFound, "", "", "engine not found.")
	}
	aliases := rec.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return repr.Engine{
		Object: "engine", ID: repr.ID(rec.ID), DisplayName: rec.Name, WorkCount: rec.WorkCount,
		Description: rec.Description, Aliases: aliases,
	}, nil
}

func (c *Catalog) GetRole(ctx context.Context, id int64) (repr.Role, error) {
	if c == nil || c.Public == nil {
		return repr.Role{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.RoleDetail(ctx, id)
	if err != nil {
		return repr.Role{}, err
	}
	if !found {
		return repr.Role{}, problem.New(problem.CodeNotFound, "", "", "role not found.")
	}
	return roleFromListItem(rec), nil
}

func (c *Catalog) GetSeries(ctx context.Context, id int64, nsfw bool, include []string) (repr.Series, error) {
	if c == nil || c.Public == nil {
		return repr.Series{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.SeriesDetail(ctx, id, false, nsfw, 0, 0)
	if err != nil {
		return repr.Series{}, err
	}
	if !found {
		return repr.Series{}, problem.New(problem.CodeNotFound, "", "", "series not found.")
	}
	return seriesFromDetail(rec, include), nil
}

func (c *Catalog) GetRelease(ctx context.Context, id int64, nsfw bool) (repr.Release, error) {
	if c == nil || c.Public == nil {
		return repr.Release{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	f := catsvc.ReleaseFeedFilter{
		NSFW: nsfw, IDs: []int64{id}, Kinds: releaseFeedKinds(),
		OLang: catsvc.PublicOLang{All: true},
	}
	data, err := c.Public.ReleaseFeed(ctx, f, "", 1)
	if err != nil {
		return repr.Release{}, err
	}
	if len(data.Items) == 0 {
		return repr.Release{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeRelease, "release", id)
	}
	return releaseFromFeed(data.Items[0]), nil
}
