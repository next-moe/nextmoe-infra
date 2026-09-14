package service

import (
	"context"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

// workSubresourceGate must stay the visibility half of WorkDetail, verbatim:
// medium, LIVE status, and the caller's r18 setting — no claim-state predicate,
// which WorkDetail does not apply either. A work works/{id} serves and a
// sub-resource 404s (or the reverse) is the failure this face cannot have.
func (s *PublicService) workSubresourceGate(ctx context.Context, id int64, nsfw bool) (bool, error) {
	var g struct {
		ID            int64 `gorm:"column:id"`
		MediumID      int16 `gorm:"column:medium_id"`
		Status        int16 `gorm:"column:status"`
		ContentRating int16 `gorm:"column:content_rating"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, medium_id, status, content_rating
		FROM catalog_work WHERE id = ? AND deleted_at IS NULL`, id).Scan(&g).Error; err != nil {
		return false, err
	}
	if g.ID == 0 || g.MediumID != galgameMediumID || g.Status != model.WorkStatusLive {
		return false, nil
	}
	return nsfw || !isR18(g.ContentRating), nil
}

func pageOf[T any](rows []T, limit, offset int) ([]T, *int) {
	if offset >= len(rows) {
		return []T{}, nil
	}
	end := min(offset+limit, len(rows))
	if end == len(rows) {
		return rows[offset:end], nil
	}
	return rows[offset:end], &end
}

func (s *PublicService) WorkCovers(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkCoversData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkCoversData{}, false, err
	}
	byWork, err := s.read.loadWorkCovers(ctx, []claimSubject{{WorkID: id}})
	if err != nil {
		return dto.PublicWorkCoversData{}, false, err
	}
	rows := byWork[id]
	renderable := make([]WorkCoverRow, 0, len(rows))
	for _, c := range rows {
		if s.imageURL(c.ImageHash) != "" {
			renderable = append(renderable, c)
		}
	}
	page, next := pageOf(renderable, limit, offset)
	items, err := s.publicCovers(ctx, page, s.coverMetaFor(ctx, page))
	if err != nil {
		return dto.PublicWorkCoversData{}, false, err
	}
	return dto.PublicWorkCoversData{Items: items, NextOffset: next}, true, nil
}

func (s *PublicService) WorkScreenshots(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkScreenshotsData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkScreenshotsData{}, false, err
	}
	byWork, err := s.read.loadWorkScreenshots(ctx, []claimSubject{{WorkID: id}})
	if err != nil {
		return dto.PublicWorkScreenshotsData{}, false, err
	}
	rows := byWork[id]
	renderable := make([]WorkScreenshotRow, 0, len(rows))
	for _, sc := range rows {
		if s.imageURL(sc.ImageHash) != "" {
			renderable = append(renderable, sc)
		}
	}
	page, next := pageOf(renderable, limit, offset)
	return dto.PublicWorkScreenshotsData{
		Items: s.publicScreenshots(page, s.workMediaMetaFor(ctx, nil, page)), NextOffset: next,
	}, true, nil
}

func (s *PublicService) WorkTags(ctx context.Context, id int64, nsfw bool, spoilers int16, limit, offset int) (dto.PublicWorkTagsData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkTagsData{}, false, err
	}
	byWork, err := s.read.loadWorkTags(ctx, []claimSubject{{WorkID: id}}, spoilers)
	if err != nil {
		return dto.PublicWorkTagsData{}, false, err
	}
	page, next := pageOf(s.publicWorkTags(byWork[id], spoilers), limit, offset)
	ids := make([]int64, 0, len(page))
	for _, t := range page {
		if t.CanonicalID != 0 {
			ids = append(ids, t.CanonicalID)
		}
	}
	counts, err := s.workCountsFor(ctx, tagWorkEdge, ids, nsfw)
	if err != nil {
		return dto.PublicWorkTagsData{}, false, err
	}
	for i := range page {
		if page[i].CanonicalID == 0 {
			continue
		}
		n := counts[page[i].CanonicalID]
		page[i].WorkCount = &n
	}
	return dto.PublicWorkTagsData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkCharacters(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkCharactersData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkCharactersData{}, false, err
	}
	rows, err := s.read.loadWorkCharacters(ctx, id)
	if err != nil {
		return dto.PublicWorkCharactersData{}, false, err
	}
	pageRows, next := pageOf(rows, limit, offset)
	page := s.publicRoster(pageRows, s.entityMetaFor(ctx, rosterImageHashes(pageRows)...))
	charIDs := make([]int64, 0, len(page))
	var nameIDs []int64
	for i := range page {
		charIDs = append(charIDs, page[i].ID)
		for j := range page[i].Voices {
			nameIDs = append(nameIDs, page[i].Voices[j].ID)
		}
	}
	charLoc, err := s.localizedFor(ctx, characterAliasSource, charIDs)
	if err != nil {
		return dto.PublicWorkCharactersData{}, false, err
	}
	nameLoc, err := s.localizedFor(ctx, creditNameAliasSource, nameIDs)
	if err != nil {
		return dto.PublicWorkCharactersData{}, false, err
	}
	for i := range page {
		page[i].Localized = locOrEmpty(charLoc[page[i].ID])
		for j := range page[i].Voices {
			v := &page[i].Voices[j]
			v.Localized = locOrEmpty(nameLoc[v.ID])
		}
	}
	return dto.PublicWorkCharactersData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkCredits(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkCreditsData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkCreditsData{}, false, err
	}
	rows, err := s.read.WorkCredits(ctx, id)
	if err != nil {
		return dto.PublicWorkCreditsData{}, false, err
	}
	pageRows, next := pageOf(rows, limit, offset)
	groups := s.publicCreditGroups(pageRows)
	nameIDs := make([]int64, 0, len(pageRows))
	for _, r := range pageRows {
		nameIDs = append(nameIDs, r.CreditNameID)
	}
	nameLoc, err := s.localizedFor(ctx, creditNameAliasSource, nameIDs)
	if err != nil {
		return dto.PublicWorkCreditsData{}, false, err
	}
	for gi := range groups {
		for ci := range groups[gi].Credits {
			c := &groups[gi].Credits[ci]
			c.Localized = locOrEmpty(nameLoc[c.ID])
		}
	}
	return dto.PublicWorkCreditsData{Items: groups, NextOffset: next}, true, nil
}

func (s *PublicService) WorkReleases(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkReleasesData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkReleasesData{}, false, err
	}
	rels, err := s.read.loadWorkReleases(ctx, id, false)
	if err != nil {
		return dto.PublicWorkReleasesData{}, false, err
	}
	page, next := pageOf(s.publicWorkReleases(rels), limit, offset)
	if err = s.attachReleaseLabels(ctx, page); err != nil {
		return dto.PublicWorkReleasesData{}, false, err
	}
	blocks := make([][]dto.PublicWorkLabel, 0, len(page))
	for i := range page {
		blocks = append(blocks, page[i].Labels)
	}
	if err = s.fillLabelLocalized(ctx, blocks...); err != nil {
		return dto.PublicWorkReleasesData{}, false, err
	}
	if err = s.fillWorkLabelCounts(ctx, blocks, nsfw); err != nil {
		return dto.PublicWorkReleasesData{}, false, err
	}
	return dto.PublicWorkReleasesData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkIntros(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkIntrosData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkIntrosData{}, false, err
	}
	byWork, err := s.read.loadWorkIntros(ctx, []claimSubject{{WorkID: id}})
	if err != nil {
		return dto.PublicWorkIntrosData{}, false, err
	}
	page, next := pageOf(s.workIntros(byWork[id]), limit, offset)
	return dto.PublicWorkIntrosData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkRatings(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkRatingsData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkRatingsData{}, false, err
	}
	byWork, err := s.read.loadWorkRatings(ctx, []claimSubject{{WorkID: id}})
	if err != nil {
		return dto.PublicWorkRatingsData{}, false, err
	}
	page, next := pageOf(s.publicDetailRatings(byWork[id]), limit, offset)
	return dto.PublicWorkRatingsData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkRelations(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkRelationsData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkRelationsData{}, false, err
	}
	rows, err := s.read.loadWorkRelations(ctx, id)
	if err != nil {
		return dto.PublicWorkRelationsData{}, false, err
	}
	visible := make([]WorkRelationRow, 0, len(rows))
	for _, r := range rows {
		if nsfw || !isR18(r.ContentRating) {
			visible = append(visible, r)
		}
	}
	pageRows, next := pageOf(visible, limit, offset)
	subjects := make([]claimSubject, 0, len(pageRows))
	for _, r := range pageRows {
		subjects = append(subjects, claimSubject{WorkID: r.OtherID})
	}
	limits, err := s.read.loadShelfFacts(ctx, subjects)
	if err != nil {
		return dto.PublicWorkRelationsData{}, false, err
	}
	items := s.publicRelations(pageRows, nsfw, limits)
	briefs := make([]*dto.PublicWorkBrief, len(items))
	for i := range items {
		briefs[i] = &items[i].Work
	}
	if err = s.fillWorkBriefNames(ctx, briefs...); err != nil {
		return dto.PublicWorkRelationsData{}, false, err
	}
	return dto.PublicWorkRelationsData{Items: items, NextOffset: next}, true, nil
}

func (s *PublicService) WorkSeries(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkSeriesData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkSeriesData{}, false, err
	}
	byWork, err := s.read.loadWorkSeries(ctx, []claimSubject{{WorkID: id}})
	if err != nil {
		return dto.PublicWorkSeriesData{}, false, err
	}
	items, err := s.publicWorkSeries(ctx, byWork[id], nsfw)
	if err != nil {
		return dto.PublicWorkSeriesData{}, false, err
	}
	page, next := pageOf(items, limit, offset)
	return dto.PublicWorkSeriesData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkLinks(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkLinksData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkLinksData{}, false, err
	}
	rows, err := s.workLinks(ctx, id)
	if err != nil {
		return dto.PublicWorkLinksData{}, false, err
	}
	page, next := pageOf(rows, limit, offset)
	return dto.PublicWorkLinksData{Items: page, NextOffset: next}, true, nil
}

func (s *PublicService) WorkEngines(ctx context.Context, id int64, nsfw bool, limit, offset int) (dto.PublicWorkEnginesData, bool, error) {
	ok, err := s.workSubresourceGate(ctx, id, nsfw)
	if err != nil || !ok {
		return dto.PublicWorkEnginesData{}, false, err
	}
	rows, err := s.workEngines(ctx, id)
	if err != nil {
		return dto.PublicWorkEnginesData{}, false, err
	}
	page, next := pageOf(rows, limit, offset)
	ids := make([]int64, 0, len(page))
	for _, e := range page {
		ids = append(ids, e.ID)
	}
	counts, err := s.workCountsFor(ctx, engineWorkEdge, ids, nsfw)
	if err != nil {
		return dto.PublicWorkEnginesData{}, false, err
	}
	for i := range page {
		page[i].WorkCount = counts[page[i].ID]
	}
	return dto.PublicWorkEnginesData{Items: page, NextOffset: next}, true, nil
}
