package service

import (
	"context"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func (s *PublicService) SeriesDetail(ctx context.Context, id int64, withWorks, nsfw bool, limit, offset int) (dto.PublicSeriesDetail, bool, error) {
	var head struct {
		ID          int64  `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
		SourceID    int16  `gorm:"column:source_id"`
		ExternalID  string `gorm:"column:external_id"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, display_name, source_id, external_id FROM catalog_series WHERE id = ?`, id).
		Scan(&head).Error; err != nil {
		return dto.PublicSeriesDetail{}, false, err
	}
	if head.ID == 0 {
		return dto.PublicSeriesDetail{}, false, nil
	}
	rec := dto.PublicSeriesDetail{ID: head.ID, DisplayName: head.DisplayName}
	rec.Refs = []dto.PublicCatalogRef{{Source: s.sourceKey(head.SourceID), ExternalID: head.ExternalID}}
	intros, err := s.seriesIntros(ctx, id)
	if err != nil {
		return dto.PublicSeriesDetail{}, false, err
	}
	rec.Intros = intros
	counts, nsfwWorks, err := s.workCountsWithNSFW(ctx, seriesWorkEdge, []int64{id}, nsfw)
	if err != nil {
		return dto.PublicSeriesDetail{}, false, err
	}
	rec.WorkCount = counts[id]
	rec.HasNSFW = nsfwWorks[id] > 0
	if withWorks {
		var wrows []struct {
			WorkID   int64 `gorm:"column:work_id"`
			Position int16 `gorm:"column:position"`
			Kind     int16 `gorm:"column:kind"`
		}
		if err := s.db.WithContext(ctx).Raw(`
			SELECT m.work_id, m.position, m.kind FROM catalog_series_member m
			JOIN catalog_work w ON w.id = m.work_id AND w.deleted_at IS NULL AND w.status = ? AND w.medium_id = ?
			WHERE m.series_id = ?
			ORDER BY (m.position = 0), m.position, m.work_id
			LIMIT ? OFFSET ?`,
			model.WorkStatusLive, galgameMediumID, id, limit, offset).Scan(&wrows).Error; err != nil {
			return dto.PublicSeriesDetail{}, false, err
		}
		ids := make([]int64, len(wrows))
		for i, r := range wrows {
			ids[i] = r.WorkID
		}
		briefs, err := s.loadWorkBriefs(ctx, ids, nsfw)
		if err != nil {
			return dto.PublicSeriesDetail{}, false, err
		}
		rec.Works = make([]dto.PublicWorkBrief, 0, len(ids))
		rec.Members = make([]dto.PublicSeriesMember, 0, len(ids))
		for _, r := range wrows {
			b := briefs[r.WorkID]
			if b == nil {
				continue
			}
			rec.Works = append(rec.Works, *b)
			rec.Members = append(rec.Members, dto.PublicSeriesMember{
				WorkID: r.WorkID, Position: r.Position, Kind: seriesMemberKindKey(r.Kind),
			})
		}
		rec.NextOffset = nextOffset(len(wrows), limit, offset)
	}
	return rec, true, nil
}

func seriesMemberKindKey(kind int16) string {
	switch kind {
	case model.SeriesMemberKindMain:
		return "main"
	case model.SeriesMemberKindFandisc:
		return "fandisc"
	case model.SeriesMemberKindSideStory:
		return "side_story"
	case model.SeriesMemberKindCollection:
		return "collection"
	default:
		return "unknown"
	}
}

func (s *PublicService) seriesIntros(ctx context.Context, seriesID int64) ([]dto.PublicSeriesIntro, error) {
	var rows []struct {
		Lang     string `gorm:"column:lang"`
		Intro    string `gorm:"column:intro"`
		SourceID int16  `gorm:"column:source_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT lang, intro, source_id FROM catalog_series_intro
		WHERE series_id = ? ORDER BY lang, `+
		editspec.HumanLaneFirstNoProvenanceSQL("source_id")+
		`, source_id`, seriesID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicSeriesIntro, 0, len(rows))
	seenLang := map[string]bool{}
	for _, r := range rows {
		if seenLang[r.Lang] {
			continue
		}
		seenLang[r.Lang] = true
		out = append(out, dto.PublicSeriesIntro{Lang: r.Lang, Intro: r.Intro, Source: s.sourceKey(r.SourceID)})
	}
	return out, nil
}

func (s *PublicService) SeriesList(ctx context.Context, nsfw bool, cursor, source string, limit int) (dto.PublicSeriesListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneSeries)
	if err != nil {
		return dto.PublicSeriesListData{}, err
	}
	limit = clampBrowseLimit(limit)

	sourceIDs, filterSource := s.resolveSourceIDs(source)
	if filterSource && len(sourceIDs) == 0 {
		return dto.PublicSeriesListData{Items: []dto.PublicSeriesListItem{}}, nil
	}

	var where []string
	var args []any
	if cur.ID > 0 {
		where = append(where, "s.id > ?")
		args = append(args, cur.ID)
	}
	if filterSource {
		where = append(where, "s.source_id IN ?")
		args = append(args, sourceIDs)
	}
	args = append(args, limit+taxonomyOverFetch)

	var rows []struct {
		ID          int64  `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
		Source      string `gorm:"column:source"`
	}
	q := `SELECT s.id, s.display_name, coalesce(src.key, '') AS source
		FROM catalog_series s
		LEFT JOIN catalog_source src ON src.id = s.source_id ` +
		whereClause(where) + ` ORDER BY s.id ASC LIMIT ?`
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicSeriesListData{}, err
	}

	rows, more := taxonomyTrim(rows, limit)
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	counts, nsfwWorks, err := s.workCountsWithNSFW(ctx, seriesWorkEdge, ids, nsfw)
	if err != nil {
		return dto.PublicSeriesListData{}, err
	}
	out := dto.PublicSeriesListData{Items: make([]dto.PublicSeriesListItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = dto.PublicSeriesListItem{
			ID: r.ID, DisplayName: r.DisplayName, Source: r.Source,
			WorkCount: counts[r.ID], HasNSFW: nsfwWorks[r.ID] > 0,
		}
	}
	var totalWhere []string
	var totalArgs []any
	if filterSource {
		totalWhere = append(totalWhere, "source_id IN ?")
		totalArgs = append(totalArgs, sourceIDs)
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_series", totalWhere, totalArgs); err != nil {
		return dto.PublicSeriesListData{}, err
	}
	out.NextCursor = taxonomyNextCursor(taxonomyLaneSeries, ids, more)
	return out, nil
}
