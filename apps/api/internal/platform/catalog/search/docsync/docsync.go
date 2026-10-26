package docsync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	catalogSearch "api/internal/platform/catalog/search"

	"gorm.io/gorm"
)

type scope []int64

func (s scope) and(col string) string {
	if s == nil {
		return ""
	}
	return " AND " + col + " IN ?"
}

func (s scope) args(args ...any) []any {
	if s == nil {
		return args
	}
	return append(args, []int64(s))
}

func (s scope) pageArgs(lastID int64, batch int) []any {
	return append(s.args(lastID), batch)
}

type family struct {
	docPrefix string
	run       func(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int, s scope) ([]int64, error)
}

var families = map[string]family{
	catalogSearch.IndexCreditNames: {"n", reindexCreditNames},
	catalogSearch.IndexCharacters:  {"c", reindexCharacters},
	catalogSearch.IndexLabels:      {"b", reindexLabels},
	catalogSearch.IndexWorks:       {"w", reindexWorks},
	catalogSearch.IndexTags:        {"t", reindexTags},
	catalogSearch.IndexSeries:      {"s", reindexSeries},
	catalogSearch.IndexEngines:     {"e", reindexEngines},
	catalogSearch.IndexTraits:      {"f", reindexTraits},
}

func Known(uid string) bool {
	_, ok := families[uid]
	return ok
}

func Reindex(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, uid string, batch int) error {
	f, ok := families[uid]
	if !ok {
		return fmt.Errorf("docsync: unknown index %q", uid)
	}
	indexed, err := f.run(ctx, db, idx, batch, nil)
	if err != nil {
		return err
	}
	slog.Info("reindexed", "index", uid, "docs", len(indexed))
	return nil
}

func Sync(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, uid string, ids []int64) error {
	f, ok := families[uid]
	if !ok {
		return fmt.Errorf("docsync: unknown index %q", uid)
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 {
		return nil
	}
	indexed, err := f.run(ctx, db, idx, len(ids), scope(ids))
	if err != nil {
		return err
	}
	var gone []string
	for _, id := range ids {
		if _, found := slices.BinarySearch(indexed, id); !found {
			gone = append(gone, f.docPrefix+fmt.Sprint(id))
		}
	}
	if len(gone) > 0 {
		return idx.DeleteBatch(ctx, uid, gone)
	}
	return nil
}
