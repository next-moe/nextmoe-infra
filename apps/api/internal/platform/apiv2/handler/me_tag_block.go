package handler

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func tagBlockModeKey(m int16) string {
	if m == model.TagBlockModeExact {
		return "exact"
	}
	return "tree"
}

func tagBlockRepr(r catsvc.TagBlockRecord) repr.TagBlock {
	return repr.TagBlock{
		Object: "tag_block", TagID: repr.ID(r.TagID), Mode: tagBlockModeKey(r.Mode),
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Catalog) ListTagBlocks(ctx context.Context, q collect.Query, tagIDs []string) (repr.List[repr.TagBlock], error) {
	if c == nil || c.TagBlocks == nil {
		return repr.List[repr.TagBlock]{}, problem.New(problem.CodeServiceUnavailable, "", "", "tag blocks are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.TagBlock]{}, err
	}
	if len(tagIDs) > 0 {
		ids, perr := parseBatchIDs(tagIDs, "tag_ids")
		if perr != nil {
			return repr.List[repr.TagBlock]{}, perr
		}
		rows, gerr := c.TagBlocks.ListMineFor(ctx, uid, ids)
		if gerr != nil {
			return repr.List[repr.TagBlock]{}, tagBlockErr(gerr)
		}
		byTag := make(map[int64]catsvc.TagBlockRecord, len(rows))
		for _, r := range rows {
			byTag[r.TagID] = r
		}
		items := make([]repr.TagBlock, 0, len(ids))
		var missing []string
		for i, id := range ids {
			r, ok := byTag[id]
			if !ok {
				missing = append(missing, tagIDs[i])
				continue
			}
			items = append(items, tagBlockRepr(r))
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	var before int64
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return repr.List[repr.TagBlock]{}, collectInvalidCursor()
		}
		before = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.TagBlocks.ListMine(ctx, uid, before, limit+1)
	if lerr != nil {
		return repr.List[repr.TagBlock]{}, tagBlockErr(lerr)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items := make([]repr.TagBlock, 0, len(rows))
	for _, r := range rows {
		items = append(items, tagBlockRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.TagBlocks.CountMine(ctx, uid); lerr != nil {
			return repr.List[repr.TagBlock]{}, tagBlockErr(lerr)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetTagBlock(ctx context.Context, tagID int64) (repr.TagBlock, error) {
	if c == nil || c.TagBlocks == nil {
		return repr.TagBlock{}, problem.New(problem.CodeServiceUnavailable, "", "", "tag blocks are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.TagBlock{}, err
	}
	rows, gerr := c.TagBlocks.ListMineFor(ctx, uid, []int64{tagID})
	if gerr != nil {
		return repr.TagBlock{}, tagBlockErr(gerr)
	}
	if len(rows) == 0 {
		return repr.TagBlock{}, problem.New(problem.CodeNotFound, "", "", "This tag is not blocked.")
	}
	return tagBlockRepr(rows[0]), nil
}

func (c *Catalog) PutTagBlock(ctx context.Context, tagID int64, mode string) (repr.TagBlock, error) {
	if c == nil || c.TagBlocks == nil {
		return repr.TagBlock{}, problem.New(problem.CodeServiceUnavailable, "", "", "tag blocks are not bound.")
	}
	uid, clientID, err := requireUser(ctx)
	if err != nil {
		return repr.TagBlock{}, err
	}
	m := model.TagBlockModeTree
	switch mode {
	case "", "tree":
	case "exact":
		m = model.TagBlockModeExact
	default:
		return repr.TagBlock{}, closedParam("mode", "tree, exact")
	}
	rec, gerr := c.TagBlocks.Block(ctx, uid, tagID, m, clientID, siteFrom(ctx))
	if gerr != nil {
		if gerr == catsvc.ErrTagBlockTagUnavailable {
			return repr.TagBlock{}, c.mergedOrNotFound(ctx, model.EntityTypeTag, "tag", tagID)
		}
		return repr.TagBlock{}, tagBlockErr(gerr)
	}
	return tagBlockRepr(*rec), nil
}

func (c *Catalog) DeleteTagBlock(ctx context.Context, tagID int64) error {
	if c == nil || c.TagBlocks == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "tag blocks are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return tagBlockErr(c.TagBlocks.Unblock(ctx, uid, tagID))
}

func tagBlockErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case err == catsvc.ErrTagBlockLimit:
		return problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a user may block at most %d tags.", model.TagBlocksPerUserMax))
	case err == catsvc.ErrTagBlockActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	case err == catsvc.ErrTagBlockTagUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "tag not found.")
	}
	return err
}
