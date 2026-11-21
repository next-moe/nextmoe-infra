package introguard

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RetryAfter is how long a refused source waits before it is offered to the
// model again; a refusal can be the model's mood rather than the source.
const RetryAfter = 30 * 24 * time.Hour

const maxOutputRunes = 2000

type memoKey struct {
	owner int64
	hash  string
}

type Rejections map[memoKey]struct{}

func (r Rejections) Has(owner int64, hash string) bool {
	_, ok := r[memoKey{owner, hash}]
	return ok
}

func LoadRejections(ctx context.Context, db *gorm.DB, introTable string) (Rejections, error) {
	var rows []struct {
		OwnerID int64
		SrcHash string
	}
	if err := db.WithContext(ctx).Model(&model.CatalogIntroMTRejection{}).
		Select("owner_id, src_hash").
		Where("intro_table = ? AND updated_at > ?", introTable, time.Now().Add(-RetryAfter)).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(Rejections, len(rows))
	for _, r := range rows {
		out[memoKey{r.OwnerID, r.SrcHash}] = struct{}{}
	}
	return out, nil
}

func RecordRejection(ctx context.Context, db *gorm.DB, introTable string, owner int64, sourceID int16, hash, reason, mtModel, output string) error {
	if r := []rune(output); len(r) > maxOutputRunes {
		output = string(r[:maxOutputRunes])
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "intro_table"}, {Name: "owner_id"}, {Name: "src_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_id", "reason", "model", "output", "updated_at"}),
	}).Create(&model.CatalogIntroMTRejection{
		IntroTable: introTable, OwnerID: owner, SourceID: sourceID, SrcHash: hash,
		Reason: reason, Model: mtModel, Output: output,
	}).Error
}
