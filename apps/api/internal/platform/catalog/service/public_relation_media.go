package service

import (
	"context"

	"api/internal/platform/catalog/dto"
)

func (s *PublicService) attachRelationMedia(ctx context.Context, rels []dto.PublicRelation, rows []WorkRelationRow, nsfw bool, limits map[int64]shelfFacts) error {
	if len(rels) == 0 {
		return nil
	}
	allow := make(map[int64]bool, len(rows))
	for _, r := range rows {
		allow[r.OtherID] = nsfw && effectiveDisplayNSFW(r.Site, r.ProductWorkID, limits[r.OtherID], r.ContentRating)
	}
	seen := make(map[int64]struct{}, len(rels))
	ids := make([]int64, 0, len(rels))
	subjects := make([]claimSubject, 0, len(rels))
	for _, r := range rels {
		if _, dup := seen[r.Work.ID]; dup {
			continue
		}
		seen[r.Work.ID] = struct{}{}
		ids = append(ids, r.Work.ID)
		subjects = append(subjects, claimSubject{WorkID: r.Work.ID})
	}
	dates, err := s.earliestReleaseDatesFor(ctx, ids)
	if err != nil {
		return err
	}
	covers, err := s.read.loadWorkCovers(ctx, subjects)
	if err != nil {
		return err
	}
	all := make([]WorkCoverRow, 0, len(covers))
	for _, id := range ids {
		all = append(all, covers[id]...)
	}
	meta := s.coverMetaFor(ctx, all)
	for i := range rels {
		w := &rels[i].Work
		w.ReleaseDate = dates[w.ID]
		if slots := s.pickCoverSlots(covers[w.ID], meta, allow[w.ID]); slots != nil {
			w.Cover = slots.Portrait
		}
	}
	return nil
}
