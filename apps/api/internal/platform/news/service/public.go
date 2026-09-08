package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/news/dto"
	"api/internal/platform/news/model"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

var (
	ErrBadCursor = stderrors.New("news: malformed or mismatched cursor")
	ErrNotFound  = stderrors.New("news: item not found")
	// A withdrawn item is not a 404. Public and credential-less means mirrors
	// exist, and a mirror that only sees the item leave the list never learns
	// the copy it already took was pulled -- which is why the detail face owes
	// a 410 (03-resources.md:142, obligation 3 of D16).
	ErrGone = stderrors.New("news: item withdrawn")
)

// cursorSort names the ordering a cursor was minted under, so a cursor from a
// different sort is rejected instead of silently paging the wrong lane. It is
// NOT the item lane (model.LaneNews / model.LaneColumn) — the two were both
// called "lane" while wave 02 was being written, which is why this one is not.
const cursorSort = "news-date-desc"

type PublicService struct {
	db      *gorm.DB
	cdnBase string
}

func NewPublicService(db *gorm.DB, cdnBase string) *PublicService {
	return &PublicService{db: db, cdnBase: cdnBase}
}

type FeedFilter struct {
	Sources         []string
	Lanes           []string
	WorkID          int64
	PublishedAfter  time.Time
	PublishedBefore time.Time
}

func (f FeedFilter) populationKey() string {
	sources := "all"
	if len(f.Sources) > 0 {
		sources = strings.Join(f.Sources, "+")
	}
	lanes := "all"
	if len(f.Lanes) > 0 {
		lanes = strings.Join(f.Lanes, "+")
	}
	return strings.Join([]string{
		sources,
		lanes,
		strconv.FormatInt(f.WorkID, 10),
		strconv.FormatInt(f.PublishedAfter.Unix(), 10),
		strconv.FormatInt(f.PublishedBefore.Unix(), 10),
	}, "-")
}

// visible is the whole public visibility contract in one place: published, and
// still present upstream. status=3 (we withdrew it) and dead_at (upstream
// deleted it) are separate facts with separate causes; both hide the row, and
// neither may be collapsed into the other.
func (f FeedFilter) where() (where []string, args []any) {
	where = []string{"i.status = ?", "i.dead_at IS NULL"}
	args = append(args, model.StatusPublished)

	if len(f.Sources) > 0 {
		where = append(where, "i.source_key IN ?")
		args = append(args, f.Sources)
	}
	if len(f.Lanes) > 0 {
		where = append(where, "i.lane IN ?")
		args = append(args, f.Lanes)
	}
	if !f.PublishedAfter.IsZero() {
		where = append(where, "i.published_at >= ?")
		args = append(args, f.PublishedAfter)
	}
	if !f.PublishedBefore.IsZero() {
		where = append(where, "i.published_at <= ?")
		args = append(args, f.PublishedBefore)
	}
	if f.WorkID > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM news_item_work iw WHERE iw.item_id = i.id AND iw.work_id = ?)")
		args = append(args, f.WorkID)
	}
	return where, args
}

func (s *PublicService) FeedMeta(ctx context.Context, f FeedFilter) (int64, time.Time, int64, error) {
	where, args := f.where()
	var row struct {
		N            int64     `gorm:"column:n"`
		MaxPublished time.Time `gorm:"column:max_published"`
		MaxID        int64     `gorm:"column:max_id"`
	}
	q := `SELECT count(*) AS n,
			coalesce(max(i.published_at), to_timestamp(0)) AS max_published,
			coalesce(max(i.id), 0) AS max_id
		FROM news_item i WHERE ` + strings.Join(where, " AND ")
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&row).Error; err != nil {
		return 0, time.Time{}, 0, err
	}
	return row.N, row.MaxPublished, row.MaxID, nil
}

func FeedETag(populationKey string, count int64, maxPublished time.Time, maxID int64) string {
	return `W/"newsfeed-` + populationKey + `-` +
		strconv.FormatInt(count, 10) + `-` +
		strconv.FormatInt(maxPublished.Unix(), 10) + `-` +
		strconv.FormatInt(maxID, 10) + `"`
}

func (f FeedFilter) PopulationKey() string { return f.populationKey() }

type itemRow struct {
	ID          int64
	SourceKey   string `gorm:"column:source_key"`
	Lane        string
	Title       string
	Preview     string
	SourceURL   string `gorm:"column:source_url"`
	BannerHash  string `gorm:"column:banner_hash"`
	PublishedAt time.Time
	// Only Item() selects this; Feed already filters on status in SQL.
	Status int16
}

func (s *PublicService) Feed(ctx context.Context, f FeedFilter, cursor string, limit int) (dto.PublicNewsFeedData, error) {
	cur, err := decodeCursor(cursor)
	if err != nil {
		return dto.PublicNewsFeedData{}, err
	}
	where, args := f.where()
	if cur.ID > 0 {
		where = append(where, "(i.published_at < ? OR (i.published_at = ? AND i.id < ?))")
		// Microseconds, not seconds: timestamptz keeps microsecond precision, and
		// a cursor rounded to the second would re-emit or skip rows that share a
		// second.
		pub := time.UnixMicro(cur.Published).UTC()
		args = append(args, pub, pub, cur.ID)
	}
	args = append(args, limit)

	q := `SELECT i.id, i.source_key, i.lane, i.title, i.preview, i.source_url, i.banner_hash, i.published_at
		FROM news_item i WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY i.published_at DESC, i.id DESC LIMIT ?`

	var rows []itemRow
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicNewsFeedData{}, err
	}
	items, err := s.buildItems(ctx, rows)
	if err != nil {
		return dto.PublicNewsFeedData{}, err
	}
	out := dto.PublicNewsFeedData{Items: items}
	if len(rows) == limit && limit > 0 {
		last := rows[len(rows)-1]
		nc := encodeCursor(feedCursor{Sort: cursorSort, ID: last.ID, Published: last.PublishedAt.UnixMicro()})
		out.NextCursor = &nc
	}
	return out, nil
}

func (s *PublicService) Item(ctx context.Context, id int64) (dto.PublicNewsItem, error) {
	var rows []itemRow
	q := `SELECT i.id, i.source_key, i.lane, i.title, i.preview, i.source_url, i.banner_hash, i.published_at, i.status
		FROM news_item i WHERE i.id = ? AND i.dead_at IS NULL`
	if err := s.db.WithContext(ctx).Raw(q, id).Scan(&rows).Error; err != nil {
		return dto.PublicNewsItem{}, err
	}
	if len(rows) == 0 {
		return dto.PublicNewsItem{}, ErrNotFound
	}
	if rows[0].Status == model.StatusWithdrawn {
		return dto.PublicNewsItem{}, ErrGone
	}
	if rows[0].Status != model.StatusPublished {
		return dto.PublicNewsItem{}, ErrNotFound
	}
	items, err := s.buildItems(ctx, rows)
	if err != nil {
		return dto.PublicNewsItem{}, err
	}
	return items[0], nil
}

func (s *PublicService) Sources(ctx context.Context) (dto.PublicNewsSourcesData, error) {
	sources, err := s.loadSources(ctx, nil)
	if err != nil {
		return dto.PublicNewsSourcesData{}, err
	}
	out := dto.PublicNewsSourcesData{Sources: make([]dto.PublicNewsSource, 0, len(sources))}
	for _, key := range sortedKeys(sources) {
		out.Sources = append(out.Sources, sources[key])
	}
	return out, nil
}

func (s *PublicService) buildItems(ctx context.Context, rows []itemRow) ([]dto.PublicNewsItem, error) {
	if len(rows) == 0 {
		return []dto.PublicNewsItem{}, nil
	}
	ids := make([]int64, len(rows))
	keys := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for i, r := range rows {
		ids[i] = r.ID
		if _, dup := seen[r.SourceKey]; !dup {
			seen[r.SourceKey] = struct{}{}
			keys = append(keys, r.SourceKey)
		}
	}
	sources, err := s.loadSources(ctx, keys)
	if err != nil {
		return nil, err
	}
	images, err := s.loadImages(ctx, ids)
	if err != nil {
		return nil, err
	}
	works, err := s.loadWorkIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]dto.PublicNewsItem, len(rows))
	for i, r := range rows {
		out[i] = dto.PublicNewsItem{
			ID:          r.ID,
			Source:      sources[r.SourceKey],
			Lane:        r.Lane,
			SourceURL:   r.SourceURL,
			Title:       r.Title,
			Preview:     r.Preview,
			BannerURL:   s.imageURL(r.BannerHash),
			BannerHash:  r.BannerHash,
			Images:      images[r.ID],
			PublishedAt: r.PublishedAt.UTC(),
			WorkIDs:     works[r.ID],
		}
	}
	return out, nil
}

func (s *PublicService) loadSources(ctx context.Context, keys []string) (map[string]dto.PublicNewsSource, error) {
	q := s.db.WithContext(ctx).Model(&model.NewsSource{})
	if len(keys) > 0 {
		q = q.Where("key IN ?", keys)
	}
	var rows []model.NewsSource
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]dto.PublicNewsSource, len(rows))
	for _, r := range rows {
		out[r.Key] = dto.PublicNewsSource{
			Key: r.Key, DisplayName: r.DisplayName, HomepageURL: r.HomepageURL,
			Attribution: r.Attribution, ColumnURL: r.ColumnURL, PublisherUID: r.PublisherUID,
		}
	}
	return out, nil
}

func (s *PublicService) loadImages(ctx context.Context, ids []int64) (map[int64][]string, error) {
	var rows []model.NewsItemImage
	if err := s.db.WithContext(ctx).Model(&model.NewsItemImage{}).
		Where("item_id IN ?", ids).Order("item_id, position, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]string, len(ids))
	for _, r := range rows {
		if u := s.imageURL(r.ImageHash); u != "" {
			out[r.ItemID] = append(out[r.ItemID], u)
		}
	}
	return out, nil
}

func (s *PublicService) loadWorkIDs(ctx context.Context, ids []int64) (map[int64][]int64, error) {
	var rows []model.NewsItemWork
	if err := s.db.WithContext(ctx).Model(&model.NewsItemWork{}).
		Where("item_id IN ?", ids).Order("item_id, work_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]int64, len(ids))
	for _, r := range rows {
		out[r.ItemID] = append(out[r.ItemID], r.WorkID)
	}
	return out, nil
}

func (s *PublicService) imageURL(hash string) string {
	if hash == "" || s.cdnBase == "" {
		return ""
	}
	return imageclient.MainURL(s.cdnBase, hash, "webp")
}

func sortedKeys(m map[string]dto.PublicNewsSource) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

type feedCursor struct {
	Sort      string `json:"s"`
	ID        int64  `json:"id"`
	Published int64  `json:"p"`
}

func encodeCursor(c feedCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(raw string) (feedCursor, error) {
	if raw == "" {
		return feedCursor{Sort: cursorSort}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return feedCursor{}, ErrBadCursor
	}
	var c feedCursor
	if err := json.Unmarshal(b, &c); err != nil || c.Sort != cursorSort {
		return feedCursor{}, ErrBadCursor
	}
	return c, nil
}
