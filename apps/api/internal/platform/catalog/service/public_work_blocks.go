package service

import (
	"context"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

// The mappers below are the single rendering of each work-detail block. They
// are shared by works/{id} and by the works/{id}/<block> sub-resources: a
// second copy would drift, and the sub-resources exist precisely to serve the
// same bytes as the block they page.

func (s *PublicService) publicWorkReleases(rows []ReleaseDetail) []dto.PublicRelease {
	out := make([]dto.PublicRelease, 0, len(rows))
	for _, rd := range rows {
		r := rd.Release
		pr := dto.PublicRelease{
			ID: r.ID, Kind: releaseKindKey(r.Kind), Date: releaseDate(r),
			Title: derefStrPub(r.Title), Lang: derefStrPub(r.Lang),
			Platform: derefStrPub(r.Platform), Platforms: publicPlatformsFromExtra(r.Extra),
			Refs: make([]dto.PublicCatalogRef, 0, len(rd.Anchors)),
		}
		if r.EngineID != nil {
			pr.Engine = &dto.PublicEngineRef{ID: *r.EngineID, Name: rd.EngineName}
		}
		for _, a := range rd.Anchors {
			if a.LinkKind != model.LinkKindExact {
				continue
			}
			pr.Refs = append(pr.Refs, dto.PublicCatalogRef{Source: a.Source, ExternalID: a.ExternalID})
		}
		out = append(out, pr)
	}
	return out
}

// publicDetailRatings is NOT publicRatings: the work-detail block carries the
// vote histogram and the spread, which the works-list block deliberately drops.
func (s *PublicService) publicDetailRatings(rows []WorkRatingRow) []dto.PublicRating {
	out := make([]dto.PublicRating, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.PublicRating{
			Source: s.sourceKey(r.SourceID), Score: r.Score, VoteCount: r.VoteCount, Rank: r.Rank,
			Distribution: r.Distribution, Stats: r.Stats,
		})
	}
	return out
}

func (s *PublicService) publicWorkTags(rows []WorkTagRow, spoilers int16) []dto.PublicTag {
	out := make([]dto.PublicTag, 0, len(rows))
	for _, t := range rows {
		if t.Spoiler > spoilers {
			continue
		}
		pt := dto.PublicTag{
			Name: t.Name, Count: t.Count, Source: s.sourceKey(t.SourceID),
			Spoiler: t.Spoiler, Sexual: t.Sexual,
		}
		if t.CanonicalID != nil {
			pt.CanonicalID = *t.CanonicalID
		}
		if t.Tier != nil {
			pt.Tier = tagTierKey(*t.Tier)
		}
		if t.Kind != nil {
			pt.Kind = tagKindKey(*t.Kind)
		}
		out = append(out, pt)
	}
	return out
}

func (s *PublicService) publicWorkSeries(ctx context.Context, rows []WorkSeriesRow, nsfw bool) ([]dto.PublicSeries, error) {
	ids := make([]int64, 0, len(rows))
	for _, se := range rows {
		ids = append(ids, se.ID)
	}
	counts, err := s.workCountsFor(ctx, seriesWorkEdge, ids, nsfw)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PublicSeries, 0, len(rows))
	for _, se := range rows {
		out = append(out, dto.PublicSeries{
			ID: se.ID, Name: se.Name, Source: s.sourceKey(se.SourceID), MemberCount: counts[se.ID],
		})
	}
	return out, nil
}

// vote_count has been in the Cover schema since wave 3 and no lane ever read
// catalog_cover_vote for it: ReadService.CoverVotes existed with no caller and
// every cover published 0, so the vote a reader had just cast never came back.
func (s *PublicService) publicCovers(ctx context.Context, rows []WorkCoverRow, meta map[string]ImageMeta) ([]dto.PublicCover, error) {
	out := make([]dto.PublicCover, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, c := range rows {
		if c.ID > 0 {
			ids = append(ids, c.ID)
		}
	}
	votes, err := s.read.CoverVotes(ctx, ids, 0)
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		url := s.imageURL(c.ImageHash)
		if url == "" {
			continue
		}
		pc := dto.PublicCover{
			ID: c.ID, URL: url, Kind: c.Kind, PortraitPinned: c.PortraitPinned,
			Sexual: c.Sexual, Violence: c.Violence, Source: s.sourceKey(c.SourceID),
			VoteCount: votes[c.ID].Count,
		}
		if m, ok := meta[c.ImageHash]; ok {
			pc.Width, pc.Height, pc.Thumbhash = m.Width, m.Height, m.Thumbhash
		}
		out = append(out, pc)
	}
	return out, nil
}

func (s *PublicService) publicScreenshots(rows []WorkScreenshotRow, meta map[string]ImageMeta) []dto.PublicScreenshot {
	out := make([]dto.PublicScreenshot, 0, len(rows))
	for _, sc := range rows {
		url := s.imageURL(sc.ImageHash)
		if url == "" {
			continue
		}
		ps := dto.PublicScreenshot{
			URL: url, Caption: sc.Caption, Sexual: sc.Sexual, Violence: sc.Violence,
			Source: s.sourceKey(sc.SourceID),
		}
		if m, ok := meta[sc.ImageHash]; ok {
			ps.Width, ps.Height, ps.Thumbhash = m.Width, m.Height, m.Thumbhash
		}
		out = append(out, ps)
	}
	return out
}

func (s *PublicService) publicRoster(rows []WorkCharacterRow, meta map[string]ImageMeta) []dto.PublicRosterCharacter {
	out := make([]dto.PublicRosterCharacter, 0, len(rows))
	for _, ch := range rows {
		pc := dto.PublicRosterCharacter{
			ID: ch.CharacterID, DisplayName: ch.DisplayName, Latin: derefStrPub(ch.Latin),
			Kind: rosterKindKey(ch.Kind), Spoiler: ch.Spoiler, Identity: ch.Identity,
			Voices: make([]dto.PublicRosterVoice, 0, len(ch.Va)),
		}
		if ch.ImageHash != nil {
			if pc.Image = s.imageURL(*ch.ImageHash); pc.Image != "" {
				pc.ImageMeta = publicImageMeta(meta, *ch.ImageHash)
			}
		}
		if ch.FigureHash != nil {
			if pc.Figure = s.imageURL(*ch.FigureHash); pc.Figure != "" {
				pc.FigureMeta = publicImageMeta(meta, *ch.FigureHash)
			}
		}
		for _, v := range ch.Va {
			pc.Voices = append(pc.Voices, dto.PublicRosterVoice{
				ID: v.CreditNameID, DisplayName: v.Name, Lang: v.Lang, Latin: derefStrPub(v.Latin),
				PersonID: derefI64Pub(v.PersonID),
			})
		}
		out = append(out, pc)
	}
	return out
}

func (s *PublicService) publicCreditGroups(rows []CreditRow) []dto.PublicCreditGroup {
	groups := make([]dto.PublicCreditGroup, 0)
	var cur *dto.PublicCreditGroup
	for _, r := range rows {
		if cur == nil || cur.RoleKey != r.RoleKey {
			groups = append(groups, dto.PublicCreditGroup{
				RoleKey:  r.RoleKey,
				RoleName: firstNonEmptyPub(r.RoleNameCN, r.RoleNameJA, r.RoleKey),
			})
			cur = &groups[len(groups)-1]
		}
		item := dto.PublicCreditItem{ID: r.CreditNameID, DisplayName: r.Name, Lang: r.Lang, Identity: r.Identity}
		if r.Latin != nil {
			item.Latin = *r.Latin
		}
		if r.CharacterID != nil {
			item.CharacterID = *r.CharacterID
		}
		if r.CharacterNM != nil {
			item.Character = *r.CharacterNM
		}
		if r.LabelID != nil {
			item.LabelID = *r.LabelID
		}
		if r.LabelNM != nil {
			item.Label = *r.LabelNM
		}
		if r.SourceKey != nil {
			item.Source = *r.SourceKey
		}
		cur.Credits = append(cur.Credits, item)
	}
	return groups
}
