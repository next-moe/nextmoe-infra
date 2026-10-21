package editspec

import (
	"context"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

func mirrorRefs(member string) func(before, after any) []int64 {
	return func(before, after any) []int64 {
		var ids []int64
		for _, v := range []any{before, after} {
			arr, _ := v.([]any)
			for i, el := range arr {
				obj, ok := el.(map[string]any)
				if !ok {
					continue
				}
				if id, err := objInt(obj, member, i, true); err == nil && id > 0 {
					ids = append(ids, id)
				}
			}
		}
		return uniqueIDs(ids)
	}
}

func mirrorOf(member string, load func(context.Context, *gorm.DB, int64) ([]any, error)) *editing.MirrorSpec {
	return &editing.MirrorSpec{
		Others: mirrorRefs(member),
		Load: func(ctx context.Context, db *gorm.DB, entityID int64) (any, error) {
			return load(ctx, db, entityID)
		},
	}
}
