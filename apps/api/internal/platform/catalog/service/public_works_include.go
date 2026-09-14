package service

import (
	"context"
	"slices"
	"strings"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const listSpoilerCeiling int16 = 0

type WorksListInclude struct {
	Names   bool
	Intros  bool
	Labels  bool
	Ratings bool
	Covers  bool
	Refs    bool
	Tags    bool
	Credits bool
}

func (i WorksListInclude) any() bool {
	return i.Names || i.Intros || i.Labels || i.Ratings || i.Covers || i.Refs || i.Tags || i.Credits
}

// intersect is the "fields acts AFTER include" rule: a block is loaded only
// when the caller asked for it AND its keys can still reach the wire. names
// carries two keys, so either one keeps it.
func (i WorksListInclude) intersect(sel PublicFields) WorksListInclude {
	return WorksListInclude{
		Names:   i.Names && sel.Wants("latin", "localized"),
		Intros:  i.Intros && sel.Wants("intros"),
		Labels:  i.Labels && sel.Wants("labels"),
		Ratings: i.Ratings && sel.Wants("ratings"),
		Covers:  i.Covers && sel.Wants("covers"),
		Refs:    i.Refs && sel.Wants("refs"),
		Tags:    i.Tags && sel.Wants("tags"),
		Credits: i.Credits && sel.Wants("credits"),
	}
}

func ParseWorksListInclude(raw string) WorksListInclude {
	var inc WorksListInclude
	for _, tok := range strings.Split(raw, ",") {
		switch strings.TrimSpace(tok) {
		case "names":
			inc.Names = true
		case "intros":
			inc.Intros = true
		case "labels":
			inc.Labels = true
		case "ratings":
			inc.Ratings = true
		case "covers":
			inc.Covers = true
		case "refs":
			inc.Refs = true
		case "tags":
			inc.Tags = true
		case "credits":
			inc.Credits = true
		}
	}
	return inc
}

func (s *PublicService) attachWorkListBlocks(
	ctx context.Context, items []dto.PublicWorkListItem, rows []workListSourceRow,
	subjects []claimSubject, covers map[int64][]WorkCoverRow, inc WorksListInclude, nsfw bool,
	displayNSFW map[int64]shelfFacts,
) error {
	if !inc.any() || len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}

	if inc.Names {
		titles, err := s.read.loadWorkTitles(ctx, subjects)
		if err != nil {
			return err
		}
		for i, r := range rows {
			items[i].Latin = workLatin(titles[r.ID], items[i].DisplayName)
			items[i].Localized = workLocalized(titles[r.ID])
		}
	}
	if inc.Intros {
		intros, err := s.read.loadWorkIntros(ctx, subjects)
		if err != nil {
			return err
		}
		for i, r := range rows {
			items[i].Intros = s.workIntros(intros[r.ID])
		}
	}
	if inc.Labels {
		labels, err := s.read.loadWorkLabels(ctx, ids)
		if err != nil {
			return err
		}
		blocks := make([][]dto.PublicWorkLabel, len(rows))
		for i, r := range rows {
			items[i].Labels = publicWorkLabels(labels[r.ID])
			blocks[i] = items[i].Labels
		}
		if err := s.fillWorkLabelCounts(ctx, blocks, nsfw); err != nil {
			return err
		}
		if err := s.fillLabelLocalized(ctx, blocks...); err != nil {
			return err
		}
	}
	if inc.Ratings {
		ratings, err := s.read.loadWorkRatings(ctx, subjects)
		if err != nil {
			return err
		}
		for i, r := range rows {
			items[i].Ratings = s.publicRatings(ratings[r.ID])
		}
	}
	if inc.Covers {
		if err := s.attachWorkListCoverSlots(ctx, items, rows, nsfw, covers, displayNSFW); err != nil {
			return err
		}
	}
	if inc.Refs {
		refs, err := s.workListRefs(ctx, ids)
		if err != nil {
			return err
		}
		for i, r := range rows {
			items[i].Refs = refs[r.ID]
		}
	}
	// The list block is cut at the default spoiler ceiling, the same way the
	// characters list cuts traits: the works list has no spoiler= axis, and the
	// caller who needs a wider ceiling reads works/{id} or works/{id}/tags.
	if inc.Tags {
		tags, err := s.read.loadWorkTags(ctx, subjects, listSpoilerCeiling)
		if err != nil {
			return err
		}
		for i, r := range rows {
			items[i].Tags = s.publicWorkTags(tags[r.ID], listSpoilerCeiling)
		}
	}
	if inc.Credits {
		if err := s.attachWorkListCredits(ctx, items, rows, ids); err != nil {
			return err
		}
	}
	return nil
}

func (s *PublicService) attachWorkListCoverSlots(
	ctx context.Context, items []dto.PublicWorkListItem, rows []workListSourceRow, nsfw bool,
	covers map[int64][]WorkCoverRow, displayNSFW map[int64]shelfFacts,
) error {
	all := make([]WorkCoverRow, 0, len(rows))
	for _, r := range rows {
		all = append(all, covers[r.ID]...)
	}
	meta := s.coverMetaFor(ctx, all)
	allow := make([]bool, len(rows))
	var needShots []claimSubject
	for i, r := range rows {
		allow[i] = nsfw && effectiveDisplayNSFW(r.Site, r.ProductWorkID, displayNSFW[r.ID], r.ContentRating)
		items[i].Covers = s.pickCoverSlots(covers[r.ID], meta, allow[i])
		if bannerMissing(items[i].Covers) {
			needShots = append(needShots, claimSubject{WorkID: r.ID})
		}
	}
	if len(needShots) == 0 {
		return nil
	}
	shots, err := s.read.loadWorkScreenshots(ctx, needShots)
	if err != nil {
		return err
	}
	flat := make([]WorkScreenshotRow, 0, len(needShots))
	for _, sub := range needShots {
		flat = append(flat, shots[sub.WorkID]...)
	}
	shotMeta := s.workMediaMetaFor(ctx, nil, flat)
	for i, r := range rows {
		if bannerMissing(items[i].Covers) {
			items[i].Covers = s.fillBannerFromScreenshots(items[i].Covers, shots[r.ID], shotMeta, allow[i])
		}
	}
	return nil
}

func (s *PublicService) attachWorkListCredits(
	ctx context.Context, items []dto.PublicWorkListItem, rows []workListSourceRow, ids []int64,
) error {
	byWork, err := s.read.workCreditsFor(ctx, ids)
	if err != nil {
		return err
	}
	var nameIDs []int64
	for _, id := range ids {
		for _, r := range byWork[id] {
			nameIDs = append(nameIDs, r.CreditNameID)
		}
	}
	nameLoc, err := s.localizedFor(ctx, creditNameAliasSource, nameIDs)
	if err != nil {
		return err
	}
	for i, r := range rows {
		groups := s.publicCreditGroups(byWork[r.ID])
		for gi := range groups {
			for ci := range groups[gi].Credits {
				c := &groups[gi].Credits[ci]
				c.Localized = locOrEmpty(nameLoc[c.ID])
			}
		}
		items[i].Credits = groups
	}
	return nil
}

func (s *PublicService) workListRefs(ctx context.Context, ids []int64) (map[int64][]dto.PublicCatalogRef, error) {
	out := make(map[int64][]dto.PublicCatalogRef, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID     int64  `gorm:"column:work_id"`
		Source     string `gorm:"column:source"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT r.entity_id AS work_id, src.key AS source, r.external_id
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id IN ? AND r.link_kind = ? AND r.dead_at IS NULL
		UNION ALL
		SELECT rel.work_id, src.key AS source, r.external_id
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND rel.work_id IN ? AND r.link_kind = ? AND r.dead_at IS NULL
		ORDER BY work_id, source, external_id`,
		model.EntityTypeWork, ids, model.LinkKindExact,
		model.EntityTypeRelease, ids, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[[2]any]struct{}, len(rows))
	for _, r := range rows {
		key := [2]any{r.WorkID, r.Source + "\x00" + r.ExternalID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out[r.WorkID] = append(out[r.WorkID], dto.PublicCatalogRef{
			Source: r.Source, ExternalID: r.ExternalID,
		})
	}
	return out, nil
}

func (s *PublicService) workIntros(rows []WorkIntroRow) []dto.PublicIntro {
	out := make([]dto.PublicIntro, 0, len(rows))
	for _, in := range rows {
		out = append(out, dto.PublicIntro{
			Lang: in.Lang, Intro: in.Intro, Source: s.sourceKey(in.SourceID), Machine: in.Machine,
		})
	}
	return out
}

func publicWorkLabels(rows []LabelAttribution) []dto.PublicWorkLabel {
	if len(rows) == 0 {
		return nil
	}
	out := make([]dto.PublicWorkLabel, 0, len(rows))
	at := make(map[int64]int, len(rows))
	primary := make(map[int64]int16, len(rows))
	for _, l := range rows {
		kind := workLabelKindKey(l.Kind)
		if i, ok := at[l.LabelID]; ok {
			out[i].Kinds = append(out[i].Kinds, kind)
			if workLabelKindRank(l.Kind) < workLabelKindRank(primary[l.LabelID]) {
				primary[l.LabelID] = l.Kind
				out[i].Kind = kind
			}
			continue
		}
		at[l.LabelID] = len(out)
		primary[l.LabelID] = l.Kind
		out = append(out, dto.PublicWorkLabel{
			ID: l.LabelID, DisplayName: l.DisplayName,
			LabelKind: labelKindKey(l.LabelKind), Kind: kind, Kinds: []string{kind}, Lang: l.Lang,
			LogoHash: l.LogoHash,
		})
	}
	for i := range out {
		slices.Sort(out[i].Kinds)
		out[i].Kinds = slices.Compact(out[i].Kinds)
	}
	return out
}

func workLabelKindRank(k int16) int {
	switch k {
	case model.WorkLabelKindBrand:
		return 0
	case model.WorkLabelKindCircle:
		return 1
	case model.WorkLabelKindDeveloper:
		return 2
	case model.WorkLabelKindPublisher:
		return 3
	default:
		return 4
	}
}

// The list block deliberately drops distribution/stats that the detail face
// carries: they exist for a per-source modal opened on one work, and a 50-item
// page with ?include=ratings would pay for 50 of them to render none.
func (s *PublicService) publicRatings(rows []WorkRatingRow) []dto.PublicRating {
	if len(rows) == 0 {
		return nil
	}
	out := make([]dto.PublicRating, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.PublicRating{
			Source: s.sourceKey(r.SourceID), Score: r.Score, VoteCount: r.VoteCount, Rank: r.Rank,
		})
	}
	return out
}
