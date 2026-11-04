package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const (
	engineRatersTTL         = time.Hour
	engineRatedWorkFloor    = 10
	engineSortPopularity    = "popularity"
	taxonomyLaneEnginesHeat = "engines_popularity"
)

var engineRaterSources = []int16{model.SourceVNDB, model.SourceBangumi, model.SourceErogameScape}

type engineRaters struct {
	Raters     int64
	RatedWorks int
}

type engineRatersCache struct {
	mu   sync.Mutex
	sf   singleflight.Group
	snap map[bool]engineRatersSnap
}

type engineRatersSnap struct {
	at time.Time
	m  map[int64]engineRaters
}

func (c *engineRatersCache) get(nsfw bool) map[int64]engineRaters {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, ok := c.snap[nsfw]
	if !ok || time.Since(snap.at) >= engineRatersTTL {
		return nil
	}
	return snap.m
}

func (c *engineRatersCache) put(nsfw bool, m map[int64]engineRaters) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil {
		c.snap = map[bool]engineRatersSnap{}
	}
	c.snap[nsfw] = engineRatersSnap{at: time.Now(), m: m}
}

func (s *PublicService) engineRatersFor(ctx context.Context, nsfw bool) (map[int64]engineRaters, error) {
	if cached := s.engineRaters.get(nsfw); cached != nil {
		return cached, nil
	}
	key := "sfw"
	if nsfw {
		key = "nsfw"
	}
	v, err, _ := s.engineRaters.sf.Do(key, func() (any, error) {
		if cached := s.engineRaters.get(nsfw); cached != nil {
			return cached, nil
		}
		m, err := s.loadEngineRaters(ctx, nsfw)
		if err != nil {
			return nil, err
		}
		s.engineRaters.put(nsfw, m)
		return m, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(map[int64]engineRaters), nil
}

func (s *PublicService) loadEngineRaters(ctx context.Context, nsfw bool) (map[int64]engineRaters, error) {
	where, popArgs := worksListPopulation(nsfw)
	args := append([]any{engineRatedWorkFloor, engineRaterSources}, popArgs...)
	var rows []struct {
		KeyID      int64 `gorm:"column:key_id"`
		Raters     int64 `gorm:"column:raters"`
		RatedWorks int   `gorm:"column:rated_works"`
	}
	q := `SELECT e.key_id,
			coalesce(sum(r.v), 0) AS raters,
			count(*) FILTER (WHERE r.v >= ?) AS rated_works
		FROM ` + engineWorkEdge.sql + ` JOIN catalog_work w ON w.id = e.work_id
		LEFT JOIN (SELECT work_id, max(vote_count) AS v FROM catalog_work_rating
			WHERE source_id IN ? GROUP BY work_id) r ON r.work_id = e.work_id
		WHERE ` + strings.Join(where, " AND ") + ` GROUP BY e.key_id`
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]engineRaters, len(rows))
	for _, r := range rows {
		m[r.KeyID] = engineRaters{Raters: r.Raters, RatedWorks: r.RatedWorks}
	}
	return m, nil
}

func (s *PublicService) enginesByRaters(ctx context.Context, f EnginesListFilter, cursor string, limit int) (dto.PublicEnginesListData, error) {
	cur, err := decodePublicCursor(cursor, taxonomyLaneEnginesHeat)
	if err != nil {
		return dto.PublicEnginesListData{}, err
	}
	limit = clampBrowseLimit(limit)
	raters, err := s.engineRatersFor(ctx, f.NSFW)
	if err != nil {
		return dto.PublicEnginesListData{}, err
	}

	var where []string
	var args []any
	if len(f.IDs) > 0 {
		where = append(where, "id IN ?")
		args = append(args, f.IDs)
	}
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(`SELECT id FROM catalog_engine `+whereClause(where), args...).
		Scan(&ids).Error; err != nil {
		return dto.PublicEnginesListData{}, err
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := raters[ids[i]].Raters, raters[ids[j]].Raters
		if a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	if cursor != "" {
		start := sort.Search(len(ids), func(i int) bool {
			r := raters[ids[i]].Raters
			return r < cur.Ord || (r == cur.Ord && ids[i] > cur.ID)
		})
		ids = ids[start:]
	}
	var more bool
	if len(f.IDs) == 0 {
		ids, more = taxonomyTrim(ids, limit)
	}

	var rows []engineRow
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Raw(
			`SELECT id, name, description, aliases FROM catalog_engine WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
			return dto.PublicEnginesListData{}, err
		}
	}
	pos := make(map[int64]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	sort.Slice(rows, func(i, j int) bool { return pos[rows[i].ID] < pos[rows[j].ID] })

	out, err := s.engineListItems(ctx, rows, f.NSFW, raters)
	if err != nil {
		return dto.PublicEnginesListData{}, err
	}
	if out.Total, err = s.taxonomyTotal(ctx, "catalog_engine", where, args); err != nil {
		return dto.PublicEnginesListData{}, err
	}
	if more && len(ids) > 0 {
		last := ids[len(ids)-1]
		c := encodePublicCursor(publicCursor{Sort: taxonomyLaneEnginesHeat, ID: last, Ord: raters[last].Raters})
		out.NextCursor = &c
	}
	return out, nil
}
