package ratingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const populationSQL = `JOIN catalog_work w ON w.id = r.work_id AND w.deleted_at IS NULL AND w.status = 0
	AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')`

type ratingRow struct {
	WorkID       int64   `gorm:"column:work_id"`
	Source       string  `gorm:"column:source"`
	Score        float64 `gorm:"column:score"`
	VoteCount    int     `gorm:"column:vote_count"`
	Distribution []byte  `gorm:"column:distribution"`
	Stats        []byte  `gorm:"column:stats"`
}

func (r ratingRow) observation() Observation {
	o := Observation{WorkID: r.WorkID, Source: r.Source, Score: r.Score, Votes: r.VoteCount}
	if len(r.Distribution) > 0 {
		_ = json.Unmarshal(r.Distribution, &o.Hist)
	}
	if len(r.Stats) > 0 {
		var st struct {
			Average *float64 `json:"average"`
			Stdev   *float64 `json:"stdev"`
		}
		if json.Unmarshal(r.Stats, &st) == nil {
			o.Average, o.Stdev = st.Average, st.Stdev
		}
	}
	return o
}

const ratingSelect = `SELECT r.work_id, s.key AS source, r.score::float8 AS score, r.vote_count, r.distribution, r.stats
	FROM catalog_work_rating r JOIN catalog_source s ON s.id = r.source_id `

func LoadAll(ctx context.Context, db *gorm.DB) ([]Observation, error) {
	var rows []ratingRow
	if err := db.WithContext(ctx).Raw(ratingSelect+populationSQL+` WHERE s.key IN ? AND r.vote_count > 0`,
		SourceKeys()).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load ratings: %w", err)
	}
	out := make([]Observation, len(rows))
	for i, r := range rows {
		out[i] = r.observation()
	}
	return out, nil
}

func LoadWorks(ctx context.Context, db *gorm.DB, ids []int64) (map[int64][]Observation, error) {
	out := map[int64][]Observation{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []ratingRow
	if err := db.WithContext(ctx).Raw(ratingSelect+`WHERE r.work_id IN ? AND s.key IN ? AND r.vote_count > 0`,
		ids, SourceKeys()).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load ratings: %w", err)
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], r.observation())
	}
	return out, nil
}

type Fitted struct {
	ID       int64
	FittedAt time.Time
	Params   *Params
}

const keepFits = 14

func Save(ctx context.Context, db *gorm.DB, p *Params, rep Report) (Fitted, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Fitted{}, err
	}
	report, err := json.Marshal(rep)
	if err != nil {
		return Fitted{}, err
	}
	row := model.CatalogRatingModel{Version: p.Version, Params: body, Report: report, Works: rep.Works, FitWorks: rep.FitWorks}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM catalog_rating_model WHERE id NOT IN
			(SELECT id FROM catalog_rating_model ORDER BY id DESC LIMIT ?)`, keepFits).Error
	})
	if err != nil {
		return Fitted{}, fmt.Errorf("save rating model: %w", err)
	}
	return Fitted{ID: row.ID, FittedAt: row.CreatedAt, Params: p}, nil
}

var ErrNoModel = errors.New("ratingmodel: no fitted model")

func Latest(ctx context.Context, db *gorm.DB) (Fitted, error) {
	var row model.CatalogRatingModel
	err := db.WithContext(ctx).Where("version = ?", Version).Order("id DESC").Limit(1).Find(&row).Error
	if err != nil {
		return Fitted{}, fmt.Errorf("load rating model: %w", err)
	}
	if row.ID == 0 {
		return Fitted{}, ErrNoModel
	}
	var p Params
	if err := json.Unmarshal(row.Params, &p); err != nil {
		return Fitted{}, fmt.Errorf("decode rating model %d: %w", row.ID, err)
	}
	return Fitted{ID: row.ID, FittedAt: row.CreatedAt, Params: &p}, nil
}

func FitAndSave(ctx context.Context, db *gorm.DB) (Fitted, Report, error) {
	obs, err := LoadAll(ctx, db)
	if err != nil {
		return Fitted{}, Report{}, err
	}
	p, rep, err := Fit(obs)
	if err != nil {
		return Fitted{}, Report{}, err
	}
	f, err := Save(ctx, db, p, rep)
	return f, rep, err
}

const cacheTTL = 10 * time.Minute

type Cache struct {
	db      *gorm.DB
	mu      sync.Mutex
	fitted  Fitted
	checked time.Time
}

func NewCache(db *gorm.DB) *Cache { return &Cache{db: db} }

func (c *Cache) Get(ctx context.Context) (Fitted, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fitted.Params != nil && time.Since(c.checked) < cacheTTL {
		return c.fitted, nil
	}
	f, err := Latest(ctx, c.db)
	if err != nil {
		if c.fitted.Params != nil && !errors.Is(err, ErrNoModel) {
			return c.fitted, nil
		}
		return Fitted{}, err
	}
	c.fitted, c.checked = f, time.Now()
	return f, nil
}
