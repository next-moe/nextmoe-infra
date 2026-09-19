package releasemeta

import (
	"context"

	"api/internal/platform/catalog/sourcedate"

	"gorm.io/gorm"
)

const mirrorBatch = 1000

func loadVndbReleased(ctx context.Context, db *gorm.DB, ids []string) (map[string]int64, error) {
	out := make(map[string]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID       string `gorm:"column:id"`
		Released int64  `gorm:"column:released"`
	}
	for start := 0; start < len(ids); start += mirrorBatch {
		end := min(start+mirrorBatch, len(ids))
		var batch []row
		if err := db.WithContext(ctx).Raw(
			`SELECT id, released FROM src_vndb.releases WHERE id IN ?`, ids[start:end],
		).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			out[r.ID] = r.Released
		}
	}
	return out, nil
}

func loadDlsiteYMD(ctx context.Context, dlDB *gorm.DB, worknos []string) (map[string]string, error) {
	out := make(map[string]string, len(worknos))
	if len(worknos) == 0 {
		return out, nil
	}
	type row struct {
		Workno string `gorm:"column:workno"`
		YMD    string `gorm:"column:ymd"`
	}
	for start := 0; start < len(worknos); start += mirrorBatch {
		end := min(start+mirrorBatch, len(worknos))
		var batch []row
		if err := dlDB.WithContext(ctx).Table("works").
			Select("workno, "+sourcedate.DLsiteDaySQL+" AS ymd").
			Where("workno IN ?", worknos[start:end]).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			out[r.Workno] = r.YMD
		}
	}
	return out, nil
}

func loadGetchuDates(ctx context.Context, gcDB *gorm.DB, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		GetchuID    string `gorm:"column:getchu_id"`
		ReleaseDate string `gorm:"column:release_date"`
	}
	for start := 0; start < len(ids); start += mirrorBatch {
		end := min(start+mirrorBatch, len(ids))
		var batch []row
		if err := gcDB.WithContext(ctx).Raw(
			`SELECT getchu_id, coalesce(release_date, '') AS release_date FROM items WHERE getchu_id IN ?`,
			ids[start:end],
		).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			out[r.GetchuID] = r.ReleaseDate
		}
	}
	return out, nil
}

func loadBgmDates(ctx context.Context, db *gorm.DB, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID   int64  `gorm:"column:id"`
		Date string `gorm:"column:date"`
	}
	for start := 0; start < len(ids); start += mirrorBatch {
		end := min(start+mirrorBatch, len(ids))
		var batch []row
		if err := db.WithContext(ctx).Raw(
			`SELECT id, coalesce(date, '') AS date FROM src_bangumi.subject WHERE id IN ?`,
			ids[start:end],
		).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			out[r.ID] = r.Date
		}
	}
	return out, nil
}

func loadDlsiteAges(ctx context.Context, dlDB *gorm.DB, worknos []string) (map[string]string, error) {
	out := make(map[string]string, len(worknos))
	type row struct {
		Workno      string  `gorm:"column:workno"`
		AgeCategory *string `gorm:"column:age_category"`
	}
	for start := 0; start < len(worknos); start += mirrorBatch {
		end := min(start+mirrorBatch, len(worknos))
		var batch []row
		if err := dlDB.WithContext(ctx).Table("works").
			Select("workno, age_category").
			Where("workno IN ?", worknos[start:end]).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			age := ""
			if r.AgeCategory != nil {
				age = *r.AgeCategory
			}
			out[r.Workno] = age
		}
	}
	return out, nil
}

func loadEgSelldays(ctx context.Context, egDB *gorm.DB, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID      int64   `gorm:"column:id"`
		Sellday *string `gorm:"column:sellday"`
	}
	for start := 0; start < len(ids); start += mirrorBatch {
		end := min(start+mirrorBatch, len(ids))
		var batch []row
		if err := egDB.WithContext(ctx).Table("games").
			Select("id, sellday").
			Where("id IN ?", ids[start:end]).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			s := ""
			if r.Sellday != nil {
				s = *r.Sellday
			}
			out[r.ID] = s
		}
	}
	return out, nil
}
