package service

import (
	"context"
	"encoding/json"
	"strings"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
)

const (
	taxonomyLaneLabels  = "labels"
	taxonomyLaneTags    = "tags"
	taxonomyLaneEngines = "engines"
	taxonomyLaneSeries  = "series"
	taxonomyLaneRoles   = "roles"
)

type LabelsListFilter struct {
	Kind     *int16
	NSFW     bool
	HasWorks bool
	IDs      []int64
}

type TagsListFilter struct {
	Tier     *int16
	Kind     *int16
	NSFW     bool
	HasWorks bool
	IDs      []int64
}

type EnginesListFilter struct {
	NSFW bool
	IDs  []int64
}

type RolesListFilter struct {
	IDs []int64
}

func (s *PublicService) LabelsList(ctx context.Context, f LabelsListFilter, cursor string, limit int) (dto.PublicLabelsListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneLabels)
	if err != nil {
		return dto.PublicLabelsListData{}, err
	}
	limit = clampBrowseLimit(limit)

	where := []string{"deleted_at IS NULL"}
	var args []any
	if f.Kind != nil {
		where = append(where, "kind = ?")
		args = append(args, *f.Kind)
	}
	if f.HasWorks {
		pred, pargs := workExistsClause(labelWorkEdge, "catalog_label.id", f.NSFW)
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if len(f.IDs) > 0 {
		where = append(where, "id IN ?")
		args = append(args, f.IDs)
	}
	filterWhere, filterArgs := append([]string(nil), where...), append([]any(nil), args...)
	if cur.ID > 0 {
		where = append(where, "id > ?")
		args = append(args, cur.ID)
	}

	var rows []struct {
		ID          int64
		DisplayName string
		Latin       string
		Lang        string
		Kind        int16
		LogoHash    string
	}
	q := `SELECT id, display_name, latin, lang, kind, logo_hash FROM catalog_label WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, f.IDs)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicLabelsListData{}, err
	}

	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	counts, err := s.workCountsFor(ctx, labelWorkEdge, ids, f.NSFW)
	if err != nil {
		return dto.PublicLabelsListData{}, err
	}
	related, err := s.labelsWithRelations(ctx, ids)
	if err != nil {
		return dto.PublicLabelsListData{}, err
	}
	aliases, err := s.entityAliasesBatch(ctx, labelAliasSource, ids)
	if err != nil {
		return dto.PublicLabelsListData{}, err
	}
	logoHashes := make([]string, 0, len(rows))
	for _, r := range rows {
		logoHashes = append(logoHashes, r.LogoHash)
	}
	logoMeta := s.entityMetaFor(ctx, logoHashes...)

	out := dto.PublicLabelsListData{Items: make([]dto.PublicLabelListItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = dto.PublicLabelListItem{
			ID: r.ID, DisplayName: r.DisplayName, Latin: r.Latin, Lang: r.Lang, Localized: localizedNames(aliases[r.ID]),
			Aliases: richAliases(aliases[r.ID]),
			Kind:    labelKindKey(r.Kind), WorkCount: counts[r.ID],
			LogoHash: r.LogoHash, LogoMeta: publicImageMeta(logoMeta, r.LogoHash),
			HasRelations: related[r.ID],
		}
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_label", filterWhere, filterArgs); err != nil {
		return dto.PublicLabelsListData{}, err
	}
	out.NextCursor = taxonomyNextCursor(taxonomyLaneLabels, ids, more)
	return out, nil
}

func (s *PublicService) labelsWithRelations(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		LabelID int64 `gorm:"column:label_id"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT DISTINCT label_id FROM catalog_label_relation WHERE label_id IN ?`, ids,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.LabelID] = true
	}
	return out, nil
}

func (s *PublicService) TagsList(ctx context.Context, f TagsListFilter, cursor string, limit int) (dto.PublicTagsListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneTags)
	if err != nil {
		return dto.PublicTagsListData{}, err
	}
	limit = clampBrowseLimit(limit)

	var where []string
	var args []any
	if f.Tier != nil {
		where = append(where, "tier = ?")
		args = append(args, *f.Tier)
	}
	if f.Kind != nil {
		where = append(where, "kind = ?")
		args = append(args, *f.Kind)
	}
	if f.HasWorks {
		pred, pargs := workExistsClause(tagWorkEdge, "catalog_tag.id", f.NSFW)
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if len(f.IDs) > 0 {
		where = append(where, "id IN ?")
		args = append(args, f.IDs)
	}
	filterWhere, filterArgs := append([]string(nil), where...), append([]any(nil), args...)
	if cur.ID > 0 {
		where = append(where, "id > ?")
		args = append(args, cur.ID)
	}

	var rows []struct {
		ID   int64
		Name string
		Tier int16
		Kind int16
	}
	q := `SELECT id, name, tier, kind FROM catalog_tag ` + whereClause(where) + ` ORDER BY id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, f.IDs)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicTagsListData{}, err
	}

	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	counts, err := s.workCountsFor(ctx, tagWorkEdge, ids, f.NSFW)
	if err != nil {
		return dto.PublicTagsListData{}, err
	}
	sexual, err := s.tagSexualFor(ctx, ids)
	if err != nil {
		return dto.PublicTagsListData{}, err
	}
	out := dto.PublicTagsListData{Items: make([]dto.PublicTagListItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = dto.PublicTagListItem{
			ID: r.ID, Name: r.Name, Tier: tagTierKey(r.Tier), Kind: tagKindKey(r.Kind),
			WorkCount: counts[r.ID], Sexual: sexual[r.ID],
		}
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_tag", filterWhere, filterArgs); err != nil {
		return dto.PublicTagsListData{}, err
	}
	out.NextCursor = taxonomyNextCursor(taxonomyLaneTags, ids, more)
	return out, nil
}

func (s *PublicService) EnginesList(ctx context.Context, f EnginesListFilter, cursor string, limit int) (dto.PublicEnginesListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneEngines)
	if err != nil {
		return dto.PublicEnginesListData{}, err
	}
	limit = clampBrowseLimit(limit)

	var where []string
	var args []any
	if len(f.IDs) > 0 {
		where = append(where, "id IN ?")
		args = append(args, f.IDs)
	}
	filterWhere, filterArgs := append([]string(nil), where...), append([]any(nil), args...)
	if cur.ID > 0 {
		where = append(where, "id > ?")
		args = append(args, cur.ID)
	}

	var rows []struct {
		ID          int64
		Name        string
		Description string
		Aliases     datatypes.JSON
	}
	q := `SELECT id, name, description, aliases FROM catalog_engine ` + whereClause(where) + ` ORDER BY id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, f.IDs)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicEnginesListData{}, err
	}

	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	counts, err := s.workCountsFor(ctx, engineWorkEdge, ids, f.NSFW)
	if err != nil {
		return dto.PublicEnginesListData{}, err
	}
	out := dto.PublicEnginesListData{Items: make([]dto.PublicEngineListItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = dto.PublicEngineListItem{
			ID: r.ID, Name: r.Name, WorkCount: counts[r.ID],
			Description: r.Description, Aliases: engineAliases(r.Aliases),
		}
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_engine", filterWhere, filterArgs); err != nil {
		return dto.PublicEnginesListData{}, err
	}
	out.NextCursor = taxonomyNextCursor(taxonomyLaneEngines, ids, more)
	return out, nil
}

func (s *PublicService) EngineDetail(ctx context.Context, id int64, nsfw bool) (dto.PublicEngine, bool, error) {
	var head struct {
		ID          int64
		Name        string
		Description string
		Aliases     datatypes.JSON
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, name, description, aliases FROM catalog_engine WHERE id = ?`, id).Scan(&head).Error; err != nil {
		return dto.PublicEngine{}, false, err
	}
	if head.ID == 0 {
		return dto.PublicEngine{}, false, nil
	}
	counts, err := s.workCountsFor(ctx, engineWorkEdge, []int64{id}, nsfw)
	if err != nil {
		return dto.PublicEngine{}, false, err
	}
	rec := dto.PublicEngine{
		ID: head.ID, Name: head.Name, WorkCount: counts[id],
		Description: head.Description, Aliases: engineAliases(head.Aliases),
	}
	if rec.Refs, err = s.entityRefs(ctx, model.EntityTypeEngine, id); err != nil {
		return dto.PublicEngine{}, false, err
	}
	return rec, true, nil
}

func (s *PublicService) RolesList(ctx context.Context, f RolesListFilter, cursor string, limit int) (dto.PublicRolesListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneRoles)
	if err != nil {
		return dto.PublicRolesListData{}, err
	}
	limit = clampBrowseLimit(limit)

	var where []string
	var args []any
	if len(f.IDs) > 0 {
		where = append(where, "id IN ?")
		args = append(args, f.IDs)
	}
	filterWhere, filterArgs := append([]string(nil), where...), append([]any(nil), args...)
	if cur.ID > 0 {
		where = append(where, "id > ?")
		args = append(args, cur.ID)
	}

	var rows []roleRow
	q := `SELECT id, key, category, name_cn, name_ja, name_en, is_deprecated FROM catalog_role ` +
		whereClause(where) + ` ORDER BY id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, f.IDs)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicRolesListData{}, err
	}

	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	ids := make([]int64, len(rows))
	out := dto.PublicRolesListData{Items: make([]dto.PublicRoleListItem, len(rows))}
	for i, r := range rows {
		ids[i] = r.ID
		out.Items[i] = r.item()
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_role", filterWhere, filterArgs); err != nil {
		return dto.PublicRolesListData{}, err
	}
	out.NextCursor = taxonomyNextCursor(taxonomyLaneRoles, ids, more)
	return out, nil
}

func (s *PublicService) RoleDetail(ctx context.Context, id int64) (dto.PublicRoleListItem, bool, error) {
	var head roleRow
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, key, category, name_cn, name_ja, name_en, is_deprecated FROM catalog_role WHERE id = ?`,
		id).Scan(&head).Error; err != nil {
		return dto.PublicRoleListItem{}, false, err
	}
	if head.ID == 0 {
		return dto.PublicRoleListItem{}, false, nil
	}
	return head.item(), true, nil
}

type roleRow struct {
	ID           int64  `gorm:"column:id"`
	Key          string `gorm:"column:key"`
	Category     string `gorm:"column:category"`
	NameCN       string `gorm:"column:name_cn"`
	NameJA       string `gorm:"column:name_ja"`
	NameEN       string `gorm:"column:name_en"`
	IsDeprecated bool   `gorm:"column:is_deprecated"`
}

func (r roleRow) item() dto.PublicRoleListItem {
	return dto.PublicRoleListItem{
		ID: r.ID, Key: r.Key, Category: r.Category,
		NameCN: r.NameCN, NameJA: r.NameJA, NameEN: r.NameEN,
		Deprecated: r.IsDeprecated,
	}
}

var taxonomyLiveClaim = []string{model.ClaimStateKeyLive}

type taxonomyEdge struct {
	sql    string
	rollup string
}

var (
	labelWorkEdge  = taxonomyEdge{sql: `(SELECT label_id AS key_id, work_id FROM catalog_work_label) e`}
	engineWorkEdge = taxonomyEdge{sql: `(SELECT engine_id AS key_id, work_id FROM ` + model.WorkEnginesSQL + ` x) e`}
	seriesWorkEdge = taxonomyEdge{sql: `(SELECT series_id AS key_id, work_id FROM catalog_series_member) e`}
	tagWorkEdge    = taxonomyEdge{
		sql: `(SELECT m.tag_id AS key_id, wt.work_id
		FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name) e`,
		rollup: "catalog_tag_work_count",
	}
)

func (s *PublicService) workCountsFor(ctx context.Context, edge taxonomyEdge, ids []int64, nsfw bool) (map[int64]int, error) {
	counts, _, err := s.workCountsWithNSFW(ctx, edge, ids, nsfw)
	return counts, err
}

func (s *PublicService) workCountsWithNSFW(ctx context.Context, edge taxonomyEdge, ids []int64, nsfw bool) (counts, nsfwWorks map[int64]int, err error) {
	if edge.rollup != "" {
		return s.workCountsFromRollup(ctx, edge, ids, nsfw)
	}
	return s.workCountsLive(ctx, edge, ids, nsfw)
}

func (s *PublicService) workCountsLive(ctx context.Context, edge taxonomyEdge, ids []int64, nsfw bool) (counts, nsfwWorks map[int64]int, err error) {
	counts, nsfwWorks = make(map[int64]int, len(ids)), make(map[int64]int, len(ids))
	if ids != nil && len(ids) == 0 {
		return counts, nsfwWorks, nil
	}
	visible := "TRUE"
	var args []any
	if !nsfw {
		visible = "w.content_rating <> ?"
		args = append(args, model.ContentRatingR18)
	}
	nsfwPred, nsfwArgs := displayLimitWhere([]string{model.DisplayLimitKeyNSFW})
	args = append(args, nsfwArgs...)

	where := []string{"w.deleted_at IS NULL", "w.status = ?", "w.medium_id = ?"}
	if ids != nil {
		where = append([]string{"e.key_id IN ?"}, where...)
		args = append(args, ids)
	}
	args = append(args, model.WorkStatusLive, galgameMediumID)
	pred, pargs := claimStateWhere(taxonomyLiveClaim)
	where = append(where, pred)
	args = append(args, pargs...)

	var rows []struct {
		KeyID int64 `gorm:"column:key_id"`
		N     int   `gorm:"column:n"`
		NNSFW int   `gorm:"column:n_nsfw"`
	}
	q := `SELECT e.key_id,
			count(DISTINCT e.work_id) FILTER (WHERE ` + visible + `) AS n,
			count(DISTINCT e.work_id) FILTER (WHERE ` + nsfwPred + `) AS n_nsfw
		FROM ` + edge.sql + ` JOIN catalog_work w ON w.id = e.work_id
		WHERE ` + strings.Join(where, " AND ") + ` GROUP BY e.key_id`
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		if r.N > 0 {
			counts[r.KeyID] = r.N
		}
		if r.NNSFW > 0 {
			nsfwWorks[r.KeyID] = r.NNSFW
		}
	}
	return counts, nsfwWorks, nil
}

func workExistsClause(edge taxonomyEdge, outerID string, nsfw bool) (string, []any) {
	where := []string{"e.key_id = " + outerID, "w.deleted_at IS NULL", "w.status = ?", "w.medium_id = ?"}
	args := []any{model.WorkStatusLive, galgameMediumID}
	if !nsfw {
		where = append(where, "w.content_rating <> ?")
		args = append(args, model.ContentRatingR18)
	}
	pred, pargs := claimStateWhere(taxonomyLiveClaim)
	where = append(where, pred)
	args = append(args, pargs...)
	return `EXISTS (SELECT 1 FROM ` + edge.sql + ` JOIN catalog_work w ON w.id = e.work_id WHERE ` +
		strings.Join(where, " AND ") + `)`, args
}

func (s *PublicService) tagSexualFor(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		TagID int64 `gorm:"column:id"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_tag WHERE id IN ? AND sexual`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TagID] = true
	}
	return out, nil
}

func (s *PublicService) taxonomyTotal(ctx context.Context, table string, where []string, args []any) (int64, error) {
	var key string
	cacheable := false
	if raw, err := json.Marshal(args); err == nil {
		key = table + "\x00" + strings.Join(where, "\x00") + "\x00" + string(raw)
		cacheable = true
		if v, ok := s.totals.get(key); ok {
			return v, nil
		}
	}

	var total int64
	q := `SELECT count(*) FROM ` + table + ` ` + whereClause(where)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&total).Error; err != nil {
		return 0, err
	}
	if cacheable {
		s.totals.put(key, total)
	}
	return total, nil
}

func engineAliases(raw datatypes.JSON) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		return out
	}
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func clampBrowseLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func applyBrowseLimit(q string, args []any, bound int, ids []int64) (string, []any, bool) {
	if len(ids) > 0 {
		return q, args, false
	}
	return q + " LIMIT ?", append(args, bound), true
}

const taxonomyOverFetch = 1

func taxonomyTrim[T any](rows []T, limit int) (page []T, more bool) {
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

func taxonomyNextCursor(lane string, ids []int64, more bool) *string {
	if !more || len(ids) == 0 {
		return nil
	}
	c := encodePublicCursor(publicCursor{Sort: lane, ID: ids[len(ids)-1]})
	return &c
}

func whereClause(where []string) string {
	if len(where) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(where, " AND ")
}
