package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
)

const laneWalkthroughs = "walkthroughs"

type WalkthroughsListFilter struct {
	NSFW        bool
	IDs         []int64
	WorkID      *int64
	CharacterID *int64
	WithTotal   bool
}

type walkthroughRow struct {
	ID              int64
	WorkID          int64
	Source          string
	SourceURL       string
	SourceAuthor    string
	SourceUpdatedOn *time.Time
	UpdatedAt       time.Time
	EndingCount     int
	SaveCount       int
	SegmentCount    int
	Characters      string
	Chart           string
}

const walkthroughColumns = `wt.id, wt.work_id, src.key AS source, wt.source_url, wt.source_author, wt.source_updated_on, wt.updated_at,
	jsonb_array_length(wt.chart->'endings') AS ending_count, jsonb_array_length(wt.chart->'saves') AS save_count,
	jsonb_array_length(wt.chart->'segments') AS segment_count, (wt.chart->'characters')::text AS characters`

const walkthroughFrom = ` FROM catalog_walkthrough wt
	JOIN catalog_work w ON w.id = wt.work_id
	JOIN catalog_source src ON src.id = wt.source_id`

func walkthroughVisible(nsfw bool) ([]string, []any) {
	where, args := worksListPopulation(nsfw)
	return append([]string{"wt.status = ?"}, where...), append([]any{model.WalkthroughStatusLive}, args...)
}

func (s *PublicService) WalkthroughsList(ctx context.Context, f WalkthroughsListFilter, cursor string, limit int) (dto.PublicWalkthroughsListData, error) {
	cur, err := decodePublicCursor(cursor, laneWalkthroughs)
	if err != nil {
		return dto.PublicWalkthroughsListData{}, err
	}
	limit = clampBrowseLimit(limit)
	where, args := walkthroughVisible(f.NSFW)
	if len(f.IDs) > 0 {
		where = append(where, "wt.id IN ?")
		args = append(args, f.IDs)
	}
	if f.WorkID != nil {
		where = append(where, "wt.work_id = ?")
		args = append(args, *f.WorkID)
	}
	if f.CharacterID != nil {
		where = append(where, `EXISTS (SELECT 1 FROM catalog_walkthrough_character wc
			WHERE wc.walkthrough_id = wt.id AND wc.character_id = ?)`)
		args = append(args, *f.CharacterID)
	}
	var total int64
	if f.WithTotal {
		if err := s.db.WithContext(ctx).Raw(`SELECT count(*)`+walkthroughFrom+` WHERE `+strings.Join(where, " AND "), args...).
			Scan(&total).Error; err != nil {
			return dto.PublicWalkthroughsListData{}, err
		}
	}
	if cur.ID > 0 {
		where = append(where, "wt.id > ?")
		args = append(args, cur.ID)
	}
	q := `SELECT ` + walkthroughColumns + walkthroughFrom + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY wt.id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, f.IDs)
	var rows []walkthroughRow
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicWalkthroughsListData{}, err
	}
	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	items, err := s.walkthroughItems(ctx, rows)
	if err != nil {
		return dto.PublicWalkthroughsListData{}, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return dto.PublicWalkthroughsListData{Items: items, NextCursor: taxonomyNextCursor(laneWalkthroughs, ids, more), Total: total}, nil
}

func (s *PublicService) WalkthroughDetail(ctx context.Context, id int64, nsfw bool) (dto.PublicWalkthrough, bool, error) {
	where, args := walkthroughVisible(nsfw)
	var rows []walkthroughRow
	if err := s.db.WithContext(ctx).Raw(`SELECT `+walkthroughColumns+`, wt.chart::text AS chart`+walkthroughFrom+
		` WHERE wt.id = ? AND `+strings.Join(where, " AND "), append([]any{id}, args...)...).Scan(&rows).Error; err != nil {
		return dto.PublicWalkthrough{}, false, err
	}
	if len(rows) == 0 {
		return dto.PublicWalkthrough{}, false, nil
	}
	items, err := s.walkthroughItems(ctx, rows)
	if err != nil {
		return dto.PublicWalkthrough{}, false, err
	}
	var c walkthrough.Chart
	if err := json.Unmarshal([]byte(rows[0].Chart), &c); err != nil {
		return dto.PublicWalkthrough{}, false, fmt.Errorf("walkthrough %d chart: %w", id, err)
	}
	items[0].Chart = &c
	return items[0], true, nil
}

func (s *PublicService) walkthroughItems(ctx context.Context, rows []walkthroughRow) ([]dto.PublicWalkthrough, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	type link struct {
		WalkthroughID int64
		ChartKey      string
		CharacterID   int64
		DisplayName   string
	}
	var links []link
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Raw(`SELECT wc.walkthrough_id, wc.chart_key, wc.character_id, c.display_name
			FROM catalog_walkthrough_character wc
			JOIN catalog_character c ON c.id = wc.character_id AND c.deleted_at IS NULL
			WHERE wc.walkthrough_id IN ?`, ids).Scan(&links).Error; err != nil {
			return nil, err
		}
	}
	linked := make(map[int64]map[string]link, len(rows))
	charIDs := make([]int64, 0, len(links))
	for _, l := range links {
		if linked[l.WalkthroughID] == nil {
			linked[l.WalkthroughID] = map[string]link{}
		}
		linked[l.WalkthroughID][l.ChartKey] = l
		charIDs = append(charIDs, l.CharacterID)
	}
	loc, err := s.localizedFor(ctx, characterAliasSource, charIDs)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PublicWalkthrough, 0, len(rows))
	for _, r := range rows {
		var chars []walkthrough.Character
		if err := json.Unmarshal([]byte(r.Characters), &chars); err != nil {
			return nil, fmt.Errorf("walkthrough %d characters: %w", r.ID, err)
		}
		item := dto.PublicWalkthrough{
			ID: r.ID, WorkID: r.WorkID, Source: r.Source, SourceURL: r.SourceURL, SourceAuthor: r.SourceAuthor,
			SourceUpdatedOn: r.SourceUpdatedOn, UpdatedAt: r.UpdatedAt,
			EndingCount: r.EndingCount, SaveCount: r.SaveCount, SegmentCount: r.SegmentCount,
			Characters: make([]dto.PublicWalkthroughCharacter, 0, len(chars)),
		}
		for _, ch := range chars {
			pc := dto.PublicWalkthroughCharacter{Key: ch.Key, Name: ch.Name, Localized: map[string]dto.PublicLocalizedName{}}
			if l, ok := linked[r.ID][ch.Key]; ok {
				id := l.CharacterID
				pc.CharacterID, pc.DisplayName = &id, l.DisplayName
				pc.Localized = locOrEmpty(loc[id])
			}
			item.Characters = append(item.Characters, pc)
		}
		out = append(out, item)
	}
	return out, nil
}
