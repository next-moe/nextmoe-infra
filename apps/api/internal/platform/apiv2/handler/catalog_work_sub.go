package handler

import (
	"context"
	"strconv"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
)

func (c *Catalog) subPage(cursor string, limit int) (int, int, error) {
	if c == nil || c.Public == nil {
		return 0, 0, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	offset := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return 0, 0, collectInvalidCursor()
		}
		offset = n
	}
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	if limit > parse.MaxPageLimit {
		limit = parse.MaxPageLimit
	}
	return offset, limit, nil
}

func (c *Catalog) missWork(ctx context.Context, id int64) error {
	return c.missingWork(ctx, id)
}

func (c *Catalog) WorkCovers(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Cover], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Cover]{}, err
	}
	data, found, err := c.Public.WorkCovers(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Cover]{}, err
	}
	if !found {
		return repr.List[repr.Cover]{}, c.missWork(ctx, id)
	}
	return repr.NewList(coversFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkScreenshots(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Screenshot], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Screenshot]{}, err
	}
	data, found, err := c.Public.WorkScreenshots(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Screenshot]{}, err
	}
	if !found {
		return repr.List[repr.Screenshot]{}, c.missWork(ctx, id)
	}
	return repr.NewList(screenshotsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkTags(ctx context.Context, id int64, nsfw bool, spoiler int16, cursor string, limit int) (repr.List[repr.WorkTag], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.WorkTag]{}, err
	}
	data, found, err := c.Public.WorkTags(ctx, id, nsfw, spoiler, limit, offset)
	if err != nil {
		return repr.List[repr.WorkTag]{}, err
	}
	if !found {
		return repr.List[repr.WorkTag]{}, c.missWork(ctx, id)
	}
	return repr.NewList(workTagsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkCharacters(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.WorkCharacter], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.WorkCharacter]{}, err
	}
	data, found, err := c.Public.WorkCharacters(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.WorkCharacter]{}, err
	}
	if !found {
		return repr.List[repr.WorkCharacter]{}, c.missWork(ctx, id)
	}
	return repr.NewList(workCharactersFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkCredits(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.CreditGroup], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.CreditGroup]{}, err
	}
	data, found, err := c.Public.WorkCredits(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.CreditGroup]{}, err
	}
	if !found {
		return repr.List[repr.CreditGroup]{}, c.missWork(ctx, id)
	}
	return repr.NewList(creditGroupsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkReleases(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Release], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Release]{}, err
	}
	data, found, err := c.Public.WorkReleases(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Release]{}, err
	}
	if !found {
		return repr.List[repr.Release]{}, c.missWork(ctx, id)
	}
	return repr.NewList(releasesFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkIntros(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Intro], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Intro]{}, err
	}
	data, found, err := c.Public.WorkIntros(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Intro]{}, err
	}
	if !found {
		return repr.List[repr.Intro]{}, c.missWork(ctx, id)
	}
	return repr.NewList(introsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkRatings(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Rating], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Rating]{}, err
	}
	data, found, err := c.Public.WorkRatings(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Rating]{}, err
	}
	if !found {
		return repr.List[repr.Rating]{}, c.missWork(ctx, id)
	}
	return repr.NewList(ratingsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkRelations(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.Relation], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Relation]{}, err
	}
	data, found, err := c.Public.WorkRelations(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.Relation]{}, err
	}
	if !found {
		return repr.List[repr.Relation]{}, c.missWork(ctx, id)
	}
	return repr.NewList(relationsFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkSeries(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.WorkSeriesRef], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.WorkSeriesRef]{}, err
	}
	data, found, err := c.Public.WorkSeries(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.WorkSeriesRef]{}, err
	}
	if !found {
		return repr.List[repr.WorkSeriesRef]{}, c.missWork(ctx, id)
	}
	return repr.NewList(workSeriesFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkLinks(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.WorkLink], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.WorkLink]{}, err
	}
	data, found, err := c.Public.WorkLinks(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.WorkLink]{}, err
	}
	if !found {
		return repr.List[repr.WorkLink]{}, c.missWork(ctx, id)
	}
	return repr.NewList(linksFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func (c *Catalog) WorkEngines(ctx context.Context, id int64, nsfw bool, cursor string, limit int) (repr.List[repr.WorkEngineRef], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.WorkEngineRef]{}, err
	}
	data, found, err := c.Public.WorkEngines(ctx, id, nsfw, limit, offset)
	if err != nil {
		return repr.List[repr.WorkEngineRef]{}, err
	}
	if !found {
		return repr.List[repr.WorkEngineRef]{}, c.missWork(ctx, id)
	}
	return repr.NewList(workEnginesFrom(data.Items), collect.EncodeOffset(derefInt(data.NextOffset))), nil
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}
