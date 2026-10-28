package docsync

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"api/internal/platform/catalog/editspec"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	syncDebounce = time.Second
	syncTimeout  = time.Minute
)

type Syncer struct {
	db  *gorm.DB
	idx *catalogSearch.Indexer
	emb catalogSearch.Embedder

	mu      sync.Mutex
	pending map[string]map[int64]struct{}
	wake    chan struct{}
}

func NewSyncer(db *gorm.DB, idx *catalogSearch.Indexer) *Syncer {
	return &Syncer{
		db:      db,
		idx:     idx,
		pending: map[string]map[int64]struct{}{},
		wake:    make(chan struct{}, 1),
	}
}

func (s *Syncer) WithEmbedder(emb catalogSearch.Embedder) *Syncer {
	s.emb = emb
	return s
}

var vectorLanes = map[string]struct {
	uid   string
	embed func(context.Context, *gorm.DB, *catalogSearch.Indexer, catalogSearch.Embedder, EmbedOptions) (EmbedStats, error)
}{
	catalogSearch.IndexWorks:      {catalogSearch.IndexWorkVectors, EmbedWorks},
	catalogSearch.IndexCharacters: {catalogSearch.IndexCharacterVectors, EmbedCharacters},
}

func (s *Syncer) syncVectors(ctx context.Context, uid string, ids []int64, gone []string) {
	lane, ok := vectorLanes[uid]
	if !ok || s.emb == nil || len(ids) == 0 {
		return
	}
	if len(gone) > 0 {
		if err := s.idx.DeleteBatch(ctx, lane.uid, gone); err != nil {
			slog.Warn("vector sync: delete departed documents", "index", lane.uid, "ids", len(gone), "err", err)
		}
	}
	opts := EmbedOptions{EmbedBatch: 16, Concurrency: 4, IDs: ids}
	if _, err := lane.embed(ctx, s.db.WithContext(ctx), s.idx, s.emb, opts); err != nil {
		slog.Warn("vector sync failed; the next embed-catalog run will catch up", "index", lane.uid, "ids", len(ids), "err", err)
	}
}

func (s *Syncer) Observe(_ context.Context, entityType string, ev editing.MergeEvent) {
	s.Enqueue(entityType, ev.EntityID)
}

func (s *Syncer) Enqueue(entityType string, ids ...int64) {
	if _, ok := editTargets[entityType]; !ok || len(ids) == 0 {
		return
	}
	s.mu.Lock()
	set := s.pending[entityType]
	if set == nil {
		set = map[int64]struct{}{}
		s.pending[entityType] = set
	}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Syncer) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(syncDebounce):
			}
			s.Flush(ctx)
		}
	}()
}

func (s *Syncer) Flush(ctx context.Context) {
	s.mu.Lock()
	batch := s.pending
	s.pending = map[string]map[int64]struct{}{}
	s.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	targets := map[string][]int64{}
	for entityType, set := range batch {
		t := editTargets[entityType]
		ids, err := t.resolve(ctx, s.db, slices.Collect(maps.Keys(set)))
		if err != nil {
			slog.Warn("search sync: resolve edited entities", "entity_type", entityType, "err", err)
			continue
		}
		targets[t.index] = append(targets[t.index], ids...)
	}
	for uid, ids := range targets {
		gone, err := syncFamily(ctx, s.db.WithContext(ctx), s.idx, uid, ids)
		if err != nil {
			slog.Warn("search sync failed; the daily reindex will catch up", "index", uid, "ids", len(ids), "err", err)
			continue
		}
		s.syncVectors(ctx, uid, ids, gone)
	}
}

type editTarget struct {
	index   string
	resolve func(ctx context.Context, db *gorm.DB, ids []int64) ([]int64, error)
}

func same(_ context.Context, _ *gorm.DB, ids []int64) ([]int64, error) { return ids, nil }

func via(query string) func(context.Context, *gorm.DB, []int64) ([]int64, error) {
	return func(ctx context.Context, db *gorm.DB, ids []int64) ([]int64, error) {
		var out []int64
		err := db.WithContext(ctx).Raw(query, ids).Scan(&out).Error
		return out, err
	}
}

var editTargets = map[string]editTarget{
	editspec.TypeWork:       {catalogSearch.IndexWorks, same},
	editspec.TypeRelease:    {catalogSearch.IndexWorks, via(`SELECT DISTINCT work_id FROM catalog_release WHERE id IN ?`)},
	editspec.TypeCharacter:  {catalogSearch.IndexCharacters, same},
	editspec.TypeCreditName: {catalogSearch.IndexCreditNames, same},
	editspec.TypePerson:     {catalogSearch.IndexCreditNames, via(`SELECT id FROM catalog_credit_name WHERE person_id IN ?`)},
	editspec.TypeLabel:      {catalogSearch.IndexLabels, same},
	editspec.TypeTag:        {catalogSearch.IndexTags, same},
	editspec.TypeEngine:     {catalogSearch.IndexEngines, same},
	editspec.TypeSeries:     {catalogSearch.IndexSeries, same},
	editspec.TypeTrait:      {catalogSearch.IndexTraits, same},
}
