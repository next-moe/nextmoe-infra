package service

import (
	"context"
	"strings"

	"api/internal/platform/catalog/model"
)

var DisplayLimitTokens = []string{model.DisplayLimitKeySFW, model.DisplayLimitKeyNSFW}

func IsDisplayLimit(tok string) bool {
	for _, v := range DisplayLimitTokens {
		if tok == v {
			return true
		}
	}
	return false
}

// displayLimitNSFWSQL is the SQL twin of model.WorkShelf.NSFW and the only SQL
// copy of the axis: the sfw branch below is written as its negation rather than
// as a second expression, because two hand-maintained complementary branches
// are exactly where a partition quietly stops partitioning. It takes one
// argument, the r18 content rating.
const displayLimitNSFWSQL = `(w.cover_art_all_explicit
	OR (` + claimedSQL + ` AND w.display_nsfw)
	OR (NOT ` + claimedSQL + ` AND coalesce(w.title_nsfw, w.content_rating = ?)))`

func displayLimitWhere(limits []string) (string, []any) {
	if len(limits) == 0 {
		return "", nil
	}
	ors := make([]string, 0, len(limits))
	args := make([]any, 0, len(limits))
	for _, lim := range limits {
		switch lim {
		case model.DisplayLimitKeyNSFW:
			ors = append(ors, displayLimitNSFWSQL)
			args = append(args, model.ContentRatingR18)
		case model.DisplayLimitKeySFW:
			ors = append(ors, "NOT "+displayLimitNSFWSQL)
			args = append(args, model.ContentRatingR18)
		default:
			ors = append(ors, "false")
		}
	}
	return "((" + strings.Join(ors, ") OR (") + "))", args
}

// shelfFacts are the two catalog_work columns a list row does not already
// carry. cover_art_all_explicit is maintained by a database trigger; see the
// catalog migrate package.
type shelfFacts struct {
	DisplayNSFW         bool
	CoverArtAllExplicit bool
	TitleNSFW           *bool
}

// The cover gates must classify a work exactly as claimed_by.content_limit
// does: gating on the raw column left unclaimed r18 works (display_nsfw is
// never edited for them) hiding their covers even from nsfw viewers.
func effectiveDisplayNSFW(site *string, productWorkID *int64, f shelfFacts, contentRating int16) bool {
	return model.WorkShelf{
		Site: site, ProductWorkID: productWorkID, DisplayNSFW: f.DisplayNSFW,
		ContentRating: contentRating, CoverArtAllExplicit: f.CoverArtAllExplicit, TitleNSFW: f.TitleNSFW,
	}.NSFW()
}

func shelfLimitKey(site *string, productWorkID *int64, f shelfFacts, contentRating int16) string {
	return model.DisplayLimitKey(model.WorkShelf{
		Site: site, ProductWorkID: productWorkID, DisplayNSFW: f.DisplayNSFW,
		ContentRating: contentRating, CoverArtAllExplicit: f.CoverArtAllExplicit, TitleNSFW: f.TitleNSFW,
	})
}

func (s *ReadService) loadShelfFacts(ctx context.Context, subjects []claimSubject) (map[int64]shelfFacts, error) {
	out := make(map[int64]shelfFacts, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	var rows []struct {
		ID                  int64 `gorm:"column:id"`
		DisplayNSFW         bool  `gorm:"column:display_nsfw"`
		CoverArtAllExplicit bool  `gorm:"column:cover_art_all_explicit"`
		TitleNSFW           *bool `gorm:"column:title_nsfw"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, display_nsfw, cover_art_all_explicit, title_nsfw FROM catalog_work WHERE id IN ?`,
		workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = shelfFacts{DisplayNSFW: r.DisplayNSFW, CoverArtAllExplicit: r.CoverArtAllExplicit, TitleNSFW: r.TitleNSFW}
	}
	return out, nil
}

func (s *PublicService) workDisplayNSFW(ctx context.Context, id int64) (bool, error) {
	var w struct {
		Site                *string `gorm:"column:site"`
		ProductWorkID       *int64  `gorm:"column:product_work_id"`
		ContentRating       int16   `gorm:"column:content_rating"`
		DisplayNSFW         bool    `gorm:"column:display_nsfw"`
		CoverArtAllExplicit bool    `gorm:"column:cover_art_all_explicit"`
		TitleNSFW           *bool   `gorm:"column:title_nsfw"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT site, product_work_id, content_rating, display_nsfw,
		cover_art_all_explicit, title_nsfw FROM catalog_work WHERE id = ?`, id).Scan(&w).Error; err != nil {
		return false, err
	}
	return effectiveDisplayNSFW(w.Site, w.ProductWorkID, shelfFacts{
		DisplayNSFW: w.DisplayNSFW, CoverArtAllExplicit: w.CoverArtAllExplicit, TitleNSFW: w.TitleNSFW,
	}, w.ContentRating), nil
}
