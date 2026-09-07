package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/pkg/imageclient"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type PublicService struct {
	db          *gorm.DB
	read        *ReadService
	resolve     *ResolveService
	mediums     map[int16]string
	sources     map[int16]string
	cdnBase     string
	imageMeta   ImageMetaFunc
	metaCache   *imageMetaCache
	totals      *totalsCache
	worksSearch *catsearch.Indexer
}

// FlushTotals empties the include_total count cache so the next read recounts.
func (s *PublicService) FlushTotals() {
	s.totals.flush()
}

func NewPublicService(db *gorm.DB, read *ReadService, resolve *ResolveService, cdnBase string) *PublicService {
	s := &PublicService{db: db, read: read, resolve: resolve,
		mediums: map[int16]string{}, sources: map[int16]string{}, cdnBase: cdnBase,
		totals: newTotalsCache()}
	var rows []struct {
		ID  int16
		Key string
	}
	if err := db.Raw(`SELECT id, key FROM catalog_medium`).Scan(&rows).Error; err == nil {
		for _, r := range rows {
			s.mediums[r.ID] = r.Key
		}
	}
	var srcRows []struct {
		ID  int16
		Key string
	}
	if err := db.Raw(`SELECT id, key FROM catalog_source`).Scan(&srcRows).Error; err == nil {
		for _, r := range srcRows {
			s.sources[r.ID] = r.Key
		}
	}
	return s
}

const galgameMediumID int16 = 1

type PublicInclude struct {
	Relations bool
	Credits   bool
	Works     bool
}

func ParsePublicInclude(raw string) PublicInclude {
	var inc PublicInclude
	for _, tok := range strings.Split(raw, ",") {
		switch strings.TrimSpace(tok) {
		case "relations":
			inc.Relations = true
		case "credits":
			inc.Credits = true
		case "works":
			inc.Works = true
		}
	}
	return inc
}

var publicLookupTypes = map[string]int16{
	"work":      model.EntityTypeWork,
	"name":      model.EntityTypeCreditName,
	"character": model.EntityTypeCharacter,
	"label":     model.EntityTypeLabel,
}

func ParsePublicLookupType(raw string) (entityType int16, key string, ok bool) {
	key = strings.TrimSpace(raw)
	if key == "" {
		key = "work"
	}
	entityType, ok = publicLookupTypes[key]
	return entityType, key, ok
}

func (s *PublicService) Lookup(ctx context.Context, source, externalID string, nsfw bool) (dto.PublicLookupData, bool, error) {
	return s.LookupTyped(ctx, source, externalID, model.EntityTypeWork, nsfw)
}

func (s *PublicService) LookupTyped(ctx context.Context, source, externalID string, entityType int16, nsfw bool) (dto.PublicLookupData, bool, error) {
	if entityType == model.EntityTypeWork {
		brief, err := s.lookupBrief(ctx, source, externalID, nsfw)
		if err != nil || brief == nil {
			return dto.PublicLookupData{}, false, err
		}
		return dto.PublicLookupData{Work: brief, ClaimedBy: brief.ClaimedBy}, true, nil
	}

	id, err := s.lookupEntityID(ctx, source, externalID, entityType)
	if err != nil || id == 0 {
		return dto.PublicLookupData{}, false, err
	}
	switch entityType {
	case model.EntityTypeCreditName:
		rec, found, err := s.Name(ctx, id, false, nsfw, 0, 0)
		if err != nil || !found {
			return dto.PublicLookupData{}, false, err
		}
		return dto.PublicLookupData{Name: &rec}, true, nil
	case model.EntityTypeCharacter:
		rec, found, err := s.Character(ctx, id, false, nsfw, 0, 0, 0)
		if err != nil || !found {
			return dto.PublicLookupData{}, false, err
		}
		return dto.PublicLookupData{Character: &rec}, true, nil
	case model.EntityTypeLabel:
		rec, found, err := s.Label(ctx, id, false, nsfw, 0, 0)
		if err != nil || !found {
			return dto.PublicLookupData{}, false, err
		}
		return dto.PublicLookupData{Label: &rec}, true, nil
	default:
		return dto.PublicLookupData{}, false, nil
	}
}

func (s *PublicService) LookupBatch(ctx context.Context, pairs []dto.PublicLookupPair, nsfw bool) ([]dto.PublicLookupBatchItem, error) {
	out := make([]dto.PublicLookupBatchItem, 0, len(pairs))
	for _, p := range pairs {
		entityType, key, ok := ParsePublicLookupType(p.Type)
		item := dto.PublicLookupBatchItem{Source: p.Source, ExternalID: p.ExternalID, Type: key}
		if ok {
			data, found, err := s.LookupTyped(ctx, p.Source, p.ExternalID, entityType, nsfw)
			if err != nil {
				return nil, err
			}
			if found {
				item.Work, item.ClaimedBy = data.Work, data.ClaimedBy
				item.Name, item.Character, item.Label = data.Name, data.Character, data.Label
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *PublicService) LookupEntityID(ctx context.Context, source, externalID string, entityType int16) (int64, error) {
	return s.lookupEntityID(ctx, source, externalID, entityType)
}

func (s *PublicService) lookupEntityID(ctx context.Context, source, externalID string, entityType int16) (int64, error) {
	db := s.db.WithContext(ctx)

	var srcID int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, canonicalSourceKey(source)).Scan(&srcID).Error; err != nil {
		return 0, err
	}
	if srcID == 0 {
		return 0, nil
	}
	ext := strings.TrimSpace(externalID)
	if ext == "" {
		return 0, nil
	}

	var entityID int64
	if err := db.Raw(`SELECT entity_id FROM catalog_external_ref
		WHERE source_id = ? AND external_id = ? AND link_kind = ? AND entity_type = ? LIMIT 1`,
		srcID, ext, model.LinkKindExact, entityType).Scan(&entityID).Error; err != nil {
		return 0, err
	}
	return entityID, nil
}

func (s *PublicService) lookupBrief(ctx context.Context, source, externalID string, nsfw bool) (*dto.PublicWorkBrief, error) {
	db := s.db.WithContext(ctx)

	var srcID int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, canonicalSourceKey(source)).Scan(&srcID).Error; err != nil {
		return nil, err
	}
	if srcID == 0 {
		return nil, nil
	}
	ext := normalizeExternalID(source, externalID)
	if ext == "" {
		return nil, nil
	}

	var ref struct {
		EntityType int16
		EntityID   int64
	}
	if err := db.Raw(`SELECT entity_type, entity_id FROM catalog_external_ref
		WHERE source_id = ? AND external_id = ? AND link_kind = ? AND entity_type IN (?, ?)
		ORDER BY entity_type ASC LIMIT 1`,
		srcID, ext, model.LinkKindExact, model.EntityTypeWork, model.EntityTypeRelease).Scan(&ref).Error; err != nil {
		return nil, err
	}
	if ref.EntityID == 0 {
		return nil, nil
	}
	workID := ref.EntityID
	if ref.EntityType == model.EntityTypeRelease {
		if err := db.Raw(`SELECT work_id FROM catalog_release WHERE id = ?`, ref.EntityID).Scan(&workID).Error; err != nil {
			return nil, err
		}
	}
	briefs, err := s.loadWorkBriefs(ctx, []int64{workID}, nsfw)
	if err != nil {
		return nil, err
	}
	return briefs[workID], nil
}

func (s *PublicService) WorkDetail(ctx context.Context, id int64, inc PublicInclude, nsfw bool, spoilers int16, sel PublicFields) (dto.PublicCatalogWork, bool, error) {
	withRelations := inc.Relations && sel.Wants("relations")
	withCredits := inc.Credits && sel.Wants("credits")
	detail, err := s.read.WorkByIDPublic(ctx, id, spoilers, withRelations, sel)
	if err != nil {
		if stderrors.Is(err, ErrWorkNotFound) {
			return dto.PublicCatalogWork{}, false, nil
		}
		return dto.PublicCatalogWork{}, false, err
	}
	w := detail.Work
	if w.MediumID != galgameMediumID || w.Status != model.WorkStatusLive {
		return dto.PublicCatalogWork{}, false, nil
	}
	if !nsfw && isR18(w.ContentRating) {
		return dto.PublicCatalogWork{}, false, nil
	}

	var subjects []claimSubject
	if sel.Wants("claimed_by", "cover_slots", "series_siblings", "relations") {
		subjects = append(subjects, claimSubject{WorkID: w.ID})
		for _, sb := range detail.SeriesSiblings {
			subjects = append(subjects, claimSubject{WorkID: sb.WorkID})
		}
		if withRelations {
			for _, r := range detail.Relations {
				subjects = append(subjects, claimSubject{WorkID: r.OtherID})
			}
		}
	}
	limits, err := s.read.loadDisplayNSFW(ctx, subjects)
	if err != nil {
		return dto.PublicCatalogWork{}, false, err
	}

	rec := dto.PublicCatalogWork{
		ID:            w.ID,
		Medium:        s.mediumKey(w.MediumID),
		DisplayName:   w.DisplayName,
		OLang:         w.OLang,
		ContentRating: contentRatingKey(w.ContentRating),
		ReleaseDate:   earliestReleaseDate(detail.Releases),
		Titles:        publicTitles(detail.Titles),
		Latin:         workLatin(detail.Titles, w.DisplayName),
		Localized:     workLocalized(detail.Titles),
		Refs:          publicRefs(detail.Refs),
		ClaimedBy:     claimedBy(w.Site, w.ProductWorkID, w.ClaimState, limits[w.ID], w.ContentRating),
		Created:       w.CreatedAt.UTC().Format(time.RFC3339),
		Updated:       w.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if err = s.attachWorkFacets(ctx, &rec, detail, nsfw,
		effectiveDisplayNSFW(w.Site, w.ProductWorkID, limits[w.ID], w.ContentRating), spoilers, sel); err != nil {
		return dto.PublicCatalogWork{}, false, err
	}
	if sel.Wants("releases") {
		if err = s.attachReleaseLabels(ctx, rec.Releases); err != nil {
			return dto.PublicCatalogWork{}, false, err
		}
	}
	if sel.Wants("engines") {
		if rec.Engines, err = s.workEngines(ctx, id); err != nil {
			return dto.PublicCatalogWork{}, false, err
		}
	}
	// Deliberately NOT gated on ?fields=: it is the filler for work_count
	// INSIDE tags[]/labels[]/engines[], and fields= is trim-only — a selected
	// key's bytes must equal the unprojected ones. Its own three queries are
	// already skipped by the empty-id guards when those blocks went unloaded.
	if err = s.attachWorkChipCounts(ctx, &rec, nsfw); err != nil {
		return dto.PublicCatalogWork{}, false, err
	}
	if sel.Wants("links") {
		if rec.Links, err = s.workLinks(ctx, id); err != nil {
			return dto.PublicCatalogWork{}, false, err
		}
	}
	rec.SeriesSiblings = s.publicSeriesSiblings(detail.SeriesSiblings, nsfw, limits)
	if withRelations {
		rec.Relations = s.publicRelations(detail.Relations, nsfw, limits)
	}
	if withCredits {
		groups, err := s.workCredits(ctx, id)
		if err != nil {
			return dto.PublicCatalogWork{}, false, err
		}
		rec.Credits = groups
	}
	if err = s.enrichWorkNameBlocks(ctx, &rec); err != nil {
		return dto.PublicCatalogWork{}, false, err
	}
	return rec, true, nil
}

func (s *PublicService) enrichWorkNameBlocks(ctx context.Context, rec *dto.PublicCatalogWork) error {
	charIDs := make([]int64, 0, len(rec.Characters))
	var nameIDs []int64
	for i := range rec.Characters {
		charIDs = append(charIDs, rec.Characters[i].ID)
		for j := range rec.Characters[i].Voices {
			nameIDs = append(nameIDs, rec.Characters[i].Voices[j].ID)
		}
	}
	for gi := range rec.Credits {
		for ci := range rec.Credits[gi].Credits {
			nameIDs = append(nameIDs, rec.Credits[gi].Credits[ci].ID)
		}
	}
	charLoc, err := s.localizedFor(ctx, characterAliasSource, charIDs)
	if err != nil {
		return err
	}
	nameLoc, err := s.localizedFor(ctx, creditNameAliasSource, nameIDs)
	if err != nil {
		return err
	}
	for i := range rec.Characters {
		rec.Characters[i].Localized = locOrEmpty(charLoc[rec.Characters[i].ID])
		for j := range rec.Characters[i].Voices {
			v := &rec.Characters[i].Voices[j]
			v.Localized = locOrEmpty(nameLoc[v.ID])
		}
	}
	for gi := range rec.Credits {
		for ci := range rec.Credits[gi].Credits {
			c := &rec.Credits[gi].Credits[ci]
			c.Localized = locOrEmpty(nameLoc[c.ID])
		}
	}
	labelBlocks := make([][]dto.PublicWorkLabel, 0, len(rec.Releases)+1)
	labelBlocks = append(labelBlocks, rec.Labels)
	for i := range rec.Releases {
		labelBlocks = append(labelBlocks, rec.Releases[i].Labels)
	}
	if err := s.fillLabelLocalized(ctx, labelBlocks...); err != nil {
		return err
	}
	briefs := make([]*dto.PublicWorkBrief, 0, len(rec.SeriesSiblings)+len(rec.Relations))
	for i := range rec.SeriesSiblings {
		briefs = append(briefs, &rec.SeriesSiblings[i])
	}
	for i := range rec.Relations {
		briefs = append(briefs, &rec.Relations[i].Work)
	}
	return s.fillWorkBriefNames(ctx, briefs...)
}

func (s *PublicService) publicRelations(rels []WorkRelationRow, nsfw bool, limits map[int64]bool) []dto.PublicRelation {
	out := make([]dto.PublicRelation, 0, len(rels))
	for _, r := range rels {
		if !nsfw && isR18(r.ContentRating) {
			continue
		}
		out = append(out, dto.PublicRelation{
			RelationType: r.Key, Phrase: r.Phrase,
			Work: dto.PublicWorkBrief{
				ID: r.OtherID, Medium: s.mediumKey(r.MediumID), DisplayName: r.DisplayName,
				ContentRating: contentRatingKey(r.ContentRating),
				ClaimedBy:     claimedBy(r.Site, r.ProductWorkID, r.ClaimState, limits[r.OtherID], r.ContentRating),
			},
		})
	}
	return out
}

func (s *PublicService) attachWorkFacets(ctx context.Context, rec *dto.PublicCatalogWork, detail *WorkDetail, nsfw, displayNSFW bool, spoilers int16, sel PublicFields) error {
	rec.Releases = s.publicWorkReleases(detail.Releases)
	rec.Popularity = make([]dto.PublicPopularity, 0, len(detail.Popularity))
	for _, p := range detail.Popularity {
		rec.Popularity = append(rec.Popularity, dto.PublicPopularity{
			Source: s.sourceKey(p.SourceID), Metric: popularityMetricKey(p.Metric), Value: p.Value,
		})
	}
	rec.Ratings = s.publicDetailRatings(detail.Ratings)
	rec.Tags = s.publicWorkTags(detail.Tags, spoilers)
	rec.Playtimes = make([]dto.PublicPlaytime, 0, len(detail.Playtimes))
	for _, p := range detail.Playtimes {
		rec.Playtimes = append(rec.Playtimes, dto.PublicPlaytime{
			Source: s.sourceKey(p.SourceID), Minutes: p.Minutes, VoteCount: p.VoteCount,
		})
	}
	series, err := s.publicWorkSeries(ctx, detail.Series, nsfw)
	if err != nil {
		return err
	}
	rec.Series = series
	rec.Platforms = make([]dto.PublicPlatform, 0, len(detail.Platforms))
	for _, p := range detail.Platforms {
		rec.Platforms = append(rec.Platforms, dto.PublicPlatform{Platform: p.Platform, Source: s.sourceKey(p.SourceID)})
	}
	rec.Intros = s.workIntros(detail.Intros)
	imgMeta := s.workMediaMetaFor(ctx, detail.Covers, detail.Screenshots, rosterImageHashes(detail.Characters)...)
	covers, err := s.publicCovers(ctx, detail.Covers, imgMeta)
	if err != nil {
		return err
	}
	rec.Covers = covers
	rec.CoverSlots = s.pickCoverSlots(detail.Covers, imgMeta, nsfw && displayNSFW)
	if sel.Wants("cover_slots") && bannerMissing(rec.CoverSlots) {
		if rec.CoverSlots, err = s.fillDetailBanner(ctx, rec.CoverSlots, detail, imgMeta, nsfw && displayNSFW, sel); err != nil {
			return err
		}
	}
	rec.Screenshots = s.publicScreenshots(detail.Screenshots, imgMeta)
	rec.Characters = s.publicRoster(detail.Characters, imgMeta)
	rec.Labels = publicWorkLabels(detail.Labels)
	if rec.Labels == nil {
		rec.Labels = []dto.PublicWorkLabel{}
	}
	return nil
}

func (s *PublicService) fillDetailBanner(
	ctx context.Context, slots *dto.PublicWorkCoverSlots, detail *WorkDetail,
	imgMeta map[string]ImageMeta, allowSexual bool, sel PublicFields,
) (*dto.PublicWorkCoverSlots, error) {
	if sel.Wants("screenshots") {
		return s.fillBannerFromScreenshots(slots, detail.Screenshots, imgMeta, allowSexual), nil
	}
	byWork, err := s.read.loadWorkScreenshots(ctx, []claimSubject{{WorkID: detail.Work.ID}})
	if err != nil {
		return nil, err
	}
	shots := byWork[detail.Work.ID]
	return s.fillBannerFromScreenshots(slots, shots, s.workMediaMetaFor(ctx, nil, shots), allowSexual), nil
}

func (s *PublicService) imageURL(hash string) string {
	if hash == "" || s.cdnBase == "" {
		return ""
	}
	return imageclient.MainURL(s.cdnBase, hash, "webp")
}

func (s *PublicService) ImageURL(hash string) string {
	return s.imageURL(hash)
}

func (s *PublicService) sourceKey(id int16) string {
	return s.sources[id]
}

func (s *PublicService) resolveSourceIDs(param string) (ids []int16, filter bool) {
	param = strings.TrimSpace(param)
	if param == "" {
		return nil, false
	}
	seen := map[int16]bool{}
	for tok := range strings.SplitSeq(param, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		want := canonicalSourceKey(tok)
		for id, key := range s.sources {
			if key == want && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids, true
}

func popularityMetricKey(m int16) string {
	switch m {
	case model.PopularityMetricDownloads:
		return "downloads"
	case model.PopularityMetricWishlist:
		return "wishlist"
	case model.PopularityMetricReviews:
		return "reviews"
	case model.PopularityMetricBgmWish:
		return "bgm_wish"
	case model.PopularityMetricBgmCollect:
		return "bgm_collect"
	case model.PopularityMetricBgmDoing:
		return "bgm_doing"
	case model.PopularityMetricBgmOnHold:
		return "bgm_on_hold"
	case model.PopularityMetricBgmDropped:
		return "bgm_dropped"
	default:
		return fmt.Sprintf("metric_%d", m)
	}
}

func rosterKindKey(k int16) string {
	switch k {
	case model.WorkCharacterKindMain:
		return "main"
	case model.WorkCharacterKindSecondary:
		return "secondary"
	case model.WorkCharacterKindAppears:
		return "appears"
	default:
		return "unknown"
	}
}

func releaseKindKey(k int16) string {
	switch k {
	case model.ReleaseKindDigital:
		return "digital"
	case model.ReleaseKindPhysical:
		return "physical"
	case model.ReleaseKindTrial:
		return "trial"
	case model.ReleaseKindPatch:
		return "patch"
	default:
		return "default"
	}
}

func releaseDate(r model.CatalogRelease) *string {
	if r.ReleasedY == nil {
		return nil
	}
	d := fmt.Sprintf("%04d", *r.ReleasedY)
	if r.ReleasedM != nil && *r.ReleasedM > 0 {
		d = fmt.Sprintf("%s-%02d", d, *r.ReleasedM)
		if r.ReleasedD != nil && *r.ReleasedD > 0 {
			d = fmt.Sprintf("%s-%02d", d, *r.ReleasedD)
		}
	}
	return &d
}

func publicPlatformsFromExtra(extra datatypes.JSON) []string {
	if len(extra) == 0 {
		return nil
	}
	var e struct {
		Platforms []string `json:"platforms"`
	}
	if err := json.Unmarshal(extra, &e); err != nil {
		return nil
	}
	return e.Platforms
}

func (s *PublicService) workCredits(ctx context.Context, workID int64) ([]dto.PublicCreditGroup, error) {
	rows, err := s.read.WorkCredits(ctx, workID)
	if err != nil {
		return nil, err
	}
	return s.publicCreditGroups(rows), nil
}

func (s *PublicService) Name(ctx context.Context, id int64, withCredits, nsfw bool, limit, offset int) (dto.PublicName, bool, error) {
	res, err := s.read.NameWorks(ctx, id, limit, offset)
	if err != nil {
		return dto.PublicName{}, false, err
	}
	if res.Head == nil {
		return dto.PublicName{}, false, nil
	}
	p := dto.PublicName{
		ID:          res.Head.ID,
		DisplayName: res.Head.Name,
		Lang:        res.Head.Lang,
		Latin:       derefStrPub(res.Head.Latin),
		Siblings:    make([]dto.PublicSiblingName, 0, len(res.Siblings)),
		PhotoHash:   res.Head.PhotoHash,
		Gender:      res.Head.Gender,
		BirthY:      res.Head.BirthY,
		BirthM:      res.Head.BirthM,
		BirthD:      res.Head.BirthD,
		Links:       []dto.PublicPersonLink{},
	}
	p.PhotoMeta = publicImageMeta(s.entityMetaFor(ctx, p.PhotoHash), p.PhotoHash)
	if res.Head.PersonID != nil {
		p.PersonID = *res.Head.PersonID
		if p.Links, err = s.personLinks(ctx, p.PersonID); err != nil {
			return dto.PublicName{}, false, err
		}
	}
	for _, sib := range res.Siblings {
		p.Siblings = append(p.Siblings, dto.PublicSiblingName{
			ID:          sib.ID,
			DisplayName: sib.Name,
			Lang:        sib.Lang,
			Latin:       derefStrPub(sib.Latin),
		})
	}
	sibIDs := make([]int64, len(p.Siblings))
	for i := range p.Siblings {
		sibIDs[i] = p.Siblings[i].ID
	}
	sibLoc, err := s.localizedFor(ctx, creditNameAliasSource, sibIDs)
	if err != nil {
		return dto.PublicName{}, false, err
	}
	for i := range p.Siblings {
		p.Siblings[i].Localized = locOrEmpty(sibLoc[p.Siblings[i].ID])
	}
	if p.Aliases, p.Localized, err = s.nameAliases(ctx, id); err != nil {
		return dto.PublicName{}, false, err
	}
	if p.Intros, err = s.nameIntros(ctx, id, p.PersonID); err != nil {
		return dto.PublicName{}, false, err
	}
	if p.Refs, err = s.entityRefs(ctx, model.EntityTypeCreditName, id); err != nil {
		return dto.PublicName{}, false, err
	}
	if withCredits {
		briefs, err := s.claimEnrich(ctx, res.Works)
		if err != nil {
			return dto.PublicName{}, false, err
		}
		p.Credits = make([]dto.PublicNameCredit, 0, len(res.Works))
		for _, w := range res.Works {
			b := s.briefFromRow(w.Brief, briefs[w.Brief.WorkID], nsfw)
			if b == nil {
				continue
			}
			row := dto.PublicNameCredit{Work: *b, Roles: make([]dto.PublicNameRole, 0, len(w.Roles))}
			for _, r := range w.Roles {
				pr := dto.PublicNameRole{RoleKey: r.RoleKey, RoleName: firstNonEmptyPub(r.RoleNameCN, r.RoleNameJA, r.RoleKey),
					Identity: r.Identity}
				if r.CharacterID != nil {
					pr.CharacterID = *r.CharacterID
				}
				if r.CharacterNM != nil {
					pr.Character = *r.CharacterNM
				}
				row.Roles = append(row.Roles, pr)
			}
			p.Credits = append(p.Credits, row)
		}
		creditBriefs := make([]*dto.PublicWorkBrief, len(p.Credits))
		for i := range p.Credits {
			creditBriefs[i] = &p.Credits[i].Work
		}
		if err := s.fillWorkBriefNames(ctx, creditBriefs...); err != nil {
			return dto.PublicName{}, false, err
		}
		p.NextOffset = nextOffset(len(res.Works), limit, offset)
	}
	return p, true, nil
}

func (s *PublicService) Character(ctx context.Context, id int64, withWorks, nsfw bool, spoilers int16, limit, offset int) (dto.PublicCharacter, bool, error) {
	res, err := s.read.CharacterWorks(ctx, id, limit, offset)
	if err != nil {
		return dto.PublicCharacter{}, false, err
	}
	if res.Head == nil {
		return dto.PublicCharacter{}, false, nil
	}
	ch := dto.PublicCharacter{
		ID:          res.Head.ID,
		DisplayName: res.Head.DisplayName,
		Lang:        res.Head.Lang,
		Latin:       derefStrPub(res.Head.Latin),
	}
	if ch.Aliases, ch.Localized, err = s.characterAliases(ctx, id); err != nil {
		return dto.PublicCharacter{}, false, err
	}
	if ch.Traits, err = s.characterTraits(ctx, id, spoilers, nsfw); err != nil {
		return dto.PublicCharacter{}, false, err
	}
	if ch.Refs, err = s.entityRefs(ctx, model.EntityTypeCharacter, id); err != nil {
		return dto.PublicCharacter{}, false, err
	}
	if ch.Intros, err = s.characterIntros(ctx, id); err != nil {
		return dto.PublicCharacter{}, false, err
	}
	var art struct {
		ImageHash  *string `gorm:"column:image_hash"`
		FigureHash *string `gorm:"column:figure_hash"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT image_hash, figure_hash FROM catalog_character WHERE id = ?`, id).Scan(&art).Error; err != nil {
		return dto.PublicCharacter{}, false, err
	}
	artMeta := s.entityMetaFor(ctx, derefHashes(art.ImageHash, art.FigureHash)...)
	if art.ImageHash != nil {
		if ch.Image = s.imageURL(*art.ImageHash); ch.Image != "" {
			ch.ImageMeta = publicImageMeta(artMeta, *art.ImageHash)
		}
	}
	if art.FigureHash != nil {
		if ch.Figure = s.imageURL(*art.FigureHash); ch.Figure != "" {
			ch.FigureMeta = publicImageMeta(artMeta, *art.FigureHash)
		}
	}
	if withWorks {
		briefs, err := s.claimEnrichCharacter(ctx, res.Works)
		if err != nil {
			return dto.PublicCharacter{}, false, err
		}
		ch.Works = make([]dto.PublicCharacterWork, 0, len(res.Works))
		for _, w := range res.Works {
			b := s.briefFromRow(w.Brief, briefs[w.Brief.WorkID], nsfw)
			if b == nil {
				continue
			}
			row := dto.PublicCharacterWork{
				Work: *b, Kind: rosterKindKey(w.Kind), Spoiler: w.Spoiler, Identity: w.Identity,
				Voices: make([]dto.PublicVoiceName, 0, len(w.Voices)),
			}
			for _, v := range w.Voices {
				row.Voices = append(row.Voices, dto.PublicVoiceName{
					ID: v.CreditNameID, DisplayName: v.Name, Lang: v.Lang, Latin: derefStrPub(v.Latin),
					PersonID: derefI64Pub(v.PersonID),
				})
			}
			ch.Works = append(ch.Works, row)
		}
		workBriefs := make([]*dto.PublicWorkBrief, len(ch.Works))
		var voiceIDs []int64
		for i := range ch.Works {
			workBriefs[i] = &ch.Works[i].Work
			for j := range ch.Works[i].Voices {
				voiceIDs = append(voiceIDs, ch.Works[i].Voices[j].ID)
			}
		}
		if err := s.fillWorkBriefNames(ctx, workBriefs...); err != nil {
			return dto.PublicCharacter{}, false, err
		}
		voiceLoc, err := s.localizedFor(ctx, creditNameAliasSource, voiceIDs)
		if err != nil {
			return dto.PublicCharacter{}, false, err
		}
		for i := range ch.Works {
			for j := range ch.Works[i].Voices {
				v := &ch.Works[i].Voices[j]
				v.Localized = locOrEmpty(voiceLoc[v.ID])
			}
		}
		ch.NextOffset = nextOffset(len(res.Works), limit, offset)
	}
	return ch, true, nil
}

func (s *PublicService) Label(ctx context.Context, id int64, withWorks, nsfw bool, limit, offset int) (dto.PublicLabel, bool, error) {
	head, items, _, err := s.read.LabelWorks(ctx, id, limit, offset)
	if err != nil {
		return dto.PublicLabel{}, false, err
	}
	if head == nil {
		return dto.PublicLabel{}, false, nil
	}
	l := dto.PublicLabel{
		ID: head.ID, DisplayName: head.DisplayName, Latin: head.Latin,
		Kind: labelKindKey(head.Kind), Lang: head.Lang,
		LogoHash: head.LogoHash,
	}
	l.LogoMeta = publicImageMeta(s.entityMetaFor(ctx, l.LogoHash), l.LogoHash)
	counts, err := s.workCountsFor(ctx, labelWorkEdge, []int64{id}, nsfw)
	if err != nil {
		return dto.PublicLabel{}, false, err
	}
	l.WorkCount = counts[id]
	if l.ImprintWorkCount, err = s.imprintWorkCount(ctx, id, nsfw); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if l.Aliases, l.Localized, err = s.labelAliases(ctx, id); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if l.Intros, err = s.labelIntros(ctx, id); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if l.Links, err = s.labelLinks(ctx, id); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if l.Relations, err = s.labelRelations(ctx, id); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if l.Refs, err = s.entityRefs(ctx, model.EntityTypeLabel, id); err != nil {
		return dto.PublicLabel{}, false, err
	}
	if withWorks {
		ids := make([]int64, 0, len(items))
		for _, w := range items {
			ids = append(ids, w.WorkID)
		}
		claims, err := s.claimedByFor(ctx, ids)
		if err != nil {
			return dto.PublicLabel{}, false, err
		}
		l.Works = make([]dto.PublicLabelWork, 0, len(items))
		for _, w := range items {
			if !nsfw && isR18(w.ContentRating) {
				continue
			}
			l.Works = append(l.Works, dto.PublicLabelWork{
				Work: dto.PublicWorkBrief{
					ID: w.WorkID, Medium: s.mediumKey(w.MediumID), DisplayName: w.DisplayName,
					ContentRating: contentRatingKey(w.ContentRating), ClaimedBy: claims[w.WorkID],
				},
				Kind: workLabelKindKey(w.Kind),
			})
		}
		workBriefs := make([]*dto.PublicWorkBrief, len(l.Works))
		for i := range l.Works {
			workBriefs[i] = &l.Works[i].Work
		}
		if err := s.fillWorkBriefNames(ctx, workBriefs...); err != nil {
			return dto.PublicLabel{}, false, err
		}
		l.NextOffset = nextOffset(len(items), limit, offset)
	}
	return l, true, nil
}

func (s *PublicService) labelIntros(ctx context.Context, labelID int64) ([]dto.PublicIntro, error) {
	var rows []struct {
		Lang       string `gorm:"column:lang"`
		Intro      string `gorm:"column:intro"`
		Source     string `gorm:"column:source"`
		Provenance int16  `gorm:"column:provenance"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT i.lang, i.intro, src.key AS source, i.provenance
		FROM catalog_label_intro i JOIN catalog_source src ON src.id = i.source_id
		WHERE i.label_id = ? ORDER BY i.lang, i.provenance, `+
		editspec.HumanLaneFirstSQL("i.source_id", "i.provenance")+
		`, i.source_id`, labelID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicIntro, 0, len(rows))
	seenLang := map[string]bool{}
	for _, r := range rows {
		if seenLang[r.Lang] {
			continue
		}
		seenLang[r.Lang] = true
		out = append(out, dto.PublicIntro{Lang: r.Lang, Intro: r.Intro, Source: r.Source, Machine: r.Provenance == 1})
	}
	return out, nil
}

func (s *PublicService) labelAliases(ctx context.Context, labelID int64) ([]dto.PublicAlias, map[string]dto.PublicLocalizedName, error) {
	rows, err := s.entityAliases(ctx, labelAliasSource, labelID)
	if err != nil {
		return nil, nil, err
	}
	return richAliases(rows), localizedNames(rows), nil
}

func (s *PublicService) characterAliases(ctx context.Context, characterID int64) ([]dto.PublicAlias, map[string]dto.PublicLocalizedName, error) {
	rows, err := s.entityAliases(ctx, characterAliasSource, characterID)
	if err != nil {
		return nil, nil, err
	}
	return richAliases(rows), localizedNames(rows), nil
}

func (s *PublicService) labelLinks(ctx context.Context, labelID int64) ([]dto.PublicLabelLink, error) {
	var rows []struct {
		Source     string `gorm:"column:source"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT src.key AS source, r.external_id
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		ORDER BY r.source_id, r.external_id`,
		model.EntityTypeLabel, labelID, model.LinkKindRelated).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicLabelLink, 0, len(rows))
	for _, r := range rows {
		if url, ok := relatedLinkURL(r.Source, r.ExternalID); ok {
			out = append(out, dto.PublicLabelLink{Source: r.Source, URL: url})
		}
	}
	return out, nil
}

func (s *PublicService) labelRelations(ctx context.Context, labelID int64) ([]dto.PublicLabelRelation, error) {
	var rows []struct {
		ID       int64  `gorm:"column:id"`
		Name     string `gorm:"column:display_name"`
		Relation int16  `gorm:"column:relation"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT other.id, other.display_name, r.relation
		FROM catalog_label_relation r
		JOIN catalog_label other ON other.id = r.other_label_id AND other.deleted_at IS NULL
		WHERE r.label_id = ?
		ORDER BY r.relation, other.display_name, other.id`, labelID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicLabelRelation, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		key, ok := model.LabelRelationKey[r.Relation]
		if !ok {
			continue
		}
		out = append(out, dto.PublicLabelRelation{ID: r.ID, DisplayName: r.Name, Relation: key})
		ids = append(ids, r.ID)
	}
	loc, err := s.localizedFor(ctx, labelAliasSource, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Localized = locOrEmpty(loc[out[i].ID])
	}
	return out, nil
}

func (s *PublicService) personLinks(ctx context.Context, personID int64) ([]dto.PublicPersonLink, error) {
	var rows []struct {
		Source     string `gorm:"column:source"`
		ExternalID string `gorm:"column:external_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT src.key AS source, r.external_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.dead_at IS NULL
		  AND (r.link_kind = ? OR (r.link_kind = ? AND src.key IN ?))
		ORDER BY r.source_id, r.external_id`,
		model.EntityTypePerson, personID, model.LinkKindRelated, model.LinkKindExact,
		personAnchorSources).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicPersonLink, 0, len(rows))
	for _, r := range rows {
		render := relatedLinkURL
		if r.LinkKind == model.LinkKindExact {
			render = personAnchorURL
		}
		if url, ok := render(r.Source, r.ExternalID); ok {
			out = append(out, dto.PublicPersonLink{Source: r.Source, URL: url})
		}
	}
	return out, nil
}

var personAnchorSources = []string{"vndb", "bangumi"}

var (
	vndbStaffID     = regexp.MustCompile(`^s[0-9]+$`)
	bangumiPersonID = regexp.MustCompile(`^[0-9]+$`)
)

// personAnchorURL renders the two identity anchors that are also addressable
// pages. It is PERSON-only on purpose, and it is not the same lane as
// relatedLinkURL: the forum minted vndb URLs out of the refs on a CREDIT NAME
// and shipped a wrong-person page, because a credit-name vndb ref is a staff
// ALIAS id (vndb has no page for one) while the person-level exact anchor is
// the staff id that does. The regexps are the guard that keeps that confusion
// from recurring silently — an id of the wrong shape is skipped, never guessed.
//
// dlsite (creater ids have no public page) and erogamescape (creater.php is
// unverified) hold person exact anchors too and are deliberately absent.
func personAnchorURL(source, externalID string) (string, bool) {
	switch source {
	case "vndb":
		if vndbStaffID.MatchString(externalID) {
			return "https://vndb.org/" + externalID, true
		}
	case "bangumi":
		if bangumiPersonID.MatchString(externalID) {
			return "https://bgm.tv/person/" + externalID, true
		}
	}
	return "", false
}

func (s *PublicService) workEngines(ctx context.Context, workID int64) ([]dto.PublicWorkEngine, error) {
	var rows []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT e.id, e.name FROM catalog_work_engine we
		JOIN catalog_engine e ON e.id = we.engine_id
		WHERE we.work_id = ? ORDER BY e.name, e.id`, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicWorkEngine, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.PublicWorkEngine{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

func (s *PublicService) workLinks(ctx context.Context, workID int64) ([]dto.PublicWorkLink, error) {
	var rows []struct {
		Source     string `gorm:"column:source"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT src.key AS source, r.external_id
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		ORDER BY r.source_id, r.external_id`,
		model.EntityTypeWork, workID, model.LinkKindRelated).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicWorkLink, 0, len(rows))
	for _, r := range rows {
		if url, ok := relatedLinkURL(r.Source, r.ExternalID); ok {
			out = append(out, dto.PublicWorkLink{Source: r.Source, URL: url})
		}
	}
	return out, nil
}

// relatedLinkURL renders a link_kind=related external id to its absolute URL.
// It is the ONE template table of the public face: works, labels and persons
// all store their web presence in the same catalog_external_ref rows under the
// same source keys, so a per-face copy of these templates is not a variation
// point — it is a drift bug waiting to happen. (It was one: the label face
// carried three of these six templates and silently dropped every steam / pixiv
// / web row a label actually had.)
//
// The templates are the exact inverses of the parsers that mint these rows
// (internal/jobs/wikirescue/link.go for the work lane, the E2b org/label
// enrichment for the label lane), which is why they are safe to apply rather
// than guesses:
//
//	web            the external_id IS the full URL (that is the whole point of
//	               the `web` source — a link whose host we do not model)
//	official_site  a host+path with the scheme and trailing slash stripped
//	twitter        a bare lowercase handle
//	cien           the numeric ci-en creator id
//	steam          the numeric app id
//	pixiv          the numeric user id
//
// dlsite and dmm are deliberately ABSENT: their storefront URLs are
// section-dependent (dlsite maniax/home/pro/soft; dmm digital/dlsoft), and the
// registry stores only the bare code, so any single template would be a guess
// that 404s for part of the population. Those two anchors stay reachable as
// data through the source key; the face never invents an address for them. An
// unknown source is skipped for the same reason.
func relatedLinkURL(source, externalID string) (string, bool) {
	switch source {
	case "web":
		return externalID, true
	case "official_site":
		return "https://" + externalID, true
	case "twitter":
		return "https://x.com/" + externalID, true
	case "cien":
		return "https://ci-en.dlsite.com/creator/" + externalID, true
	case "steam":
		return "https://store.steampowered.com/app/" + externalID, true
	case "pixiv":
		return "https://www.pixiv.net/users/" + externalID, true
	default:
		return "", false
	}
}

type workBriefRow struct {
	ID            int64
	MediumID      int16
	DisplayName   string
	ContentRating int16
	Status        int16
	Site          *string
	ProductWorkID *int64
	ClaimState    *int16 `gorm:"column:claim_state"`
	DisplayNSFW   bool   `gorm:"column:display_nsfw"`
}

func (s *PublicService) loadWorkBriefs(ctx context.Context, ids []int64, nsfw bool) (map[int64]*dto.PublicWorkBrief, error) {
	if len(ids) == 0 {
		return map[int64]*dto.PublicWorkBrief{}, nil
	}
	var rows []workBriefRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT w.id, w.medium_id, w.display_name, w.content_rating, w.status, w.site, w.product_work_id, w.claim_state,
		       w.display_nsfw
		FROM catalog_work w
		WHERE w.id IN ? AND w.deleted_at IS NULL AND w.status = ?`, ids, model.WorkStatusLive).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]*dto.PublicWorkBrief, len(rows))
	briefs := make([]*dto.PublicWorkBrief, 0, len(rows))
	for _, r := range rows {
		if !nsfw && isR18(r.ContentRating) {
			continue
		}
		out[r.ID] = &dto.PublicWorkBrief{
			ID: r.ID, Medium: s.mediumKey(r.MediumID), DisplayName: r.DisplayName,
			ContentRating: contentRatingKey(r.ContentRating),
			ClaimedBy:     claimedBy(r.Site, r.ProductWorkID, r.ClaimState, r.DisplayNSFW, r.ContentRating),
		}
		briefs = append(briefs, out[r.ID])
	}
	if err := s.fillWorkBriefNames(ctx, briefs...); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PublicService) claimEnrich(ctx context.Context, works []NameWorkDetail) (map[int64]*dto.PublicClaimedBy, error) {
	ids := make([]int64, 0, len(works))
	for _, w := range works {
		ids = append(ids, w.Brief.WorkID)
	}
	return s.claimedByFor(ctx, ids)
}

func (s *PublicService) claimEnrichCharacter(ctx context.Context, works []CharacterWorkDetail) (map[int64]*dto.PublicClaimedBy, error) {
	ids := make([]int64, 0, len(works))
	for _, w := range works {
		ids = append(ids, w.Brief.WorkID)
	}
	return s.claimedByFor(ctx, ids)
}

func (s *PublicService) briefFromRow(b WorkBriefRow, claim *dto.PublicClaimedBy, nsfw bool) *dto.PublicWorkBrief {
	if !nsfw && isR18(b.ContentRating) {
		return nil
	}
	return &dto.PublicWorkBrief{
		ID: b.WorkID, Medium: s.mediumKey(b.MediumID), DisplayName: b.DisplayName,
		ContentRating: contentRatingKey(b.ContentRating), ClaimedBy: claim,
	}
}

func (s *PublicService) claimedByFor(ctx context.Context, ids []int64) (map[int64]*dto.PublicClaimedBy, error) {
	if len(ids) == 0 {
		return map[int64]*dto.PublicClaimedBy{}, nil
	}
	var rows []struct {
		ID            int64
		Site          string
		ProductWorkID int64
		ClaimState    *int16 `gorm:"column:claim_state"`
		ContentRating int16  `gorm:"column:content_rating"`
		DisplayNSFW   bool   `gorm:"column:display_nsfw"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT w.id, w.site, w.product_work_id, w.claim_state, w.content_rating, w.display_nsfw
		FROM catalog_work w
		WHERE w.id IN ? AND w.site IS NOT NULL AND w.product_work_id IS NOT NULL AND w.deleted_at IS NULL`,
		ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]*dto.PublicClaimedBy, len(rows))
	for _, r := range rows {
		out[r.ID] = &dto.PublicClaimedBy{
			Site: r.Site, WorkID: r.ProductWorkID,
			State:        model.ClaimStateKey(&r.Site, &r.ProductWorkID, r.ClaimState),
			ContentLimit: model.DisplayLimitKey(&r.Site, &r.ProductWorkID, r.DisplayNSFW, r.ContentRating),
		}
	}
	return out, nil
}

func (s *PublicService) mediumKey(id int16) string { return s.mediums[id] }

func isR18(r int16) bool { return r == model.ContentRatingR18 }

func contentRatingKey(r int16) string {
	switch r {
	case model.ContentRatingAllAges:
		return "all_ages"
	case model.ContentRatingSensitive:
		return "sensitive"
	case model.ContentRatingR18:
		return "r18"
	default:
		return "all_ages"
	}
}

func titleKindKey(k int16) (string, bool) {
	switch k {
	case model.WorkTitleKindOfficial:
		return "official", true
	case model.WorkTitleKindAlias:
		return "alias", true
	case model.WorkTitleKindAbbreviation:
		return "abbreviation", true
	default:
		return "", false
	}
}

func labelKindKey(k int16) string {
	switch k {
	case model.LabelKindGameBrand:
		return "game_brand"
	case model.LabelKindBunko:
		return "bunko"
	case model.LabelKindPublisher:
		return "publisher"
	case model.LabelKindAnimeStudio:
		return "anime_studio"
	case model.LabelKindDoujinCircle:
		return "doujin_circle"
	case model.LabelKindGroup:
		return "group"
	default:
		return "other"
	}
}

func workLabelKindKey(k int16) string {
	switch k {
	case model.WorkLabelKindCircle:
		return "circle"
	case model.WorkLabelKindPublisher:
		return "publisher"
	case model.WorkLabelKindDeveloper:
		return "developer"
	case model.WorkLabelKindBrand:
		return "brand"
	default:
		return "other"
	}
}

func publicTitles(titles []WorkTitleRow) []dto.PublicCatalogTitle {
	out := make([]dto.PublicCatalogTitle, 0, len(titles))
	for _, t := range titles {
		kind, ok := titleKindKey(t.Kind)
		if !ok {
			continue
		}
		out = append(out, dto.PublicCatalogTitle{
			Lang: t.Lang, Title: t.Title, Latin: t.Latin, Kind: kind,
			Machine: t.Provenance == model.WorkTitleProvenanceMachine,
		})
	}
	return out
}

// The registry key for ErogameScape was misspelled "erogamespace" until the
// site's real name was checked; the public wire value has always been
// "erogamescape". The pre-rename doc string advertised the misspelling as an
// accepted input, so it stays accepted here — dropping it would 404 any caller
// that took that doc at its word.
func canonicalSourceKey(k string) string {
	if k == "erogamespace" {
		return "erogamescape"
	}
	return k
}

func publicRefs(refs []RefDetail) []dto.PublicCatalogRef {
	out := make([]dto.PublicCatalogRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		key := r.Source + "\x00" + r.ExternalID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, dto.PublicCatalogRef{Source: r.Source, ExternalID: r.ExternalID})
	}
	return out
}

func claimedBy(site *string, productWorkID *int64, claimState *int16, displayNSFW bool, contentRating int16) *dto.PublicClaimedBy {
	state := model.ClaimStateKey(site, productWorkID, claimState)
	if state == model.ClaimStateKeyNone {
		return nil
	}
	return &dto.PublicClaimedBy{
		Site: *site, WorkID: *productWorkID, State: state,
		ContentLimit: model.DisplayLimitKey(site, productWorkID, displayNSFW, contentRating),
	}
}

func earliestReleaseDate(releases []ReleaseDetail) *string {
	var bestY, bestM, bestD int16
	found := false
	for _, rd := range releases {
		r := rd.Release
		if r.ReleasedY == nil {
			continue
		}
		y := *r.ReleasedY
		m := derefI16Pub(r.ReleasedM)
		d := derefI16Pub(r.ReleasedD)
		if !found || y < bestY || (y == bestY && m < bestM) || (y == bestY && m == bestM && d < bestD) {
			bestY, bestM, bestD, found = y, m, d, true
		}
	}
	if !found {
		return nil
	}
	s := fmt.Sprintf("%04d", bestY)
	if bestM > 0 {
		s = fmt.Sprintf("%s-%02d", s, bestM)
		if bestD > 0 {
			s = fmt.Sprintf("%s-%02d", s, bestD)
		}
	}
	return &s
}

func nextOffset(returned, limit, offset int) *int {
	if limit > 0 && returned == limit {
		n := offset + limit
		return &n
	}
	return nil
}

func normalizeExternalID(source, id string) string {
	id = strings.TrimSpace(id)
	if source == "vndb" && id != "" && !strings.HasPrefix(id, "v") {
		return "v" + id
	}
	return id
}

func derefStrPub(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefI16Pub(p *int16) int16 {
	if p == nil {
		return 0
	}
	return *p
}

func derefI64Pub(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func firstNonEmptyPub(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *PublicService) characterTraits(ctx context.Context, characterID int64, spoilers int16, nsfw bool) ([]dto.PublicCharacterTrait, error) {
	var rows []struct {
		ID              int64
		Name            string
		NameZh          string `gorm:"column:name_zh"`
		NameZhProv      int16  `gorm:"column:name_zh_provenance"`
		GroupName       *string
		GroupNameZh     *string `gorm:"column:group_name_zh"`
		GroupNameZhProv *int16  `gorm:"column:group_name_zh_provenance"`
		Sexual          bool
		SpoilerLevel    int16
		Lie             bool
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT t.id, t.name, t.name_zh, t.name_zh_provenance,
			g.name AS group_name, g.name_zh AS group_name_zh,
			g.name_zh_provenance AS group_name_zh_provenance,
			t.sexual, l.spoiler_level, l.lie
		FROM catalog_character_trait_link l
		JOIN catalog_character_trait t ON t.id = l.trait_id
		LEFT JOIN catalog_character_trait g ON g.vndb_tid = t.group_tid
		WHERE l.character_id = ? AND l.spoiler_level <= ? AND (? OR NOT t.sexual)
		ORDER BY t.group_tid, t.gorder, t.name`, characterID, spoilers, nsfw).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicCharacterTrait, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.PublicCharacterTrait{
			ID: r.ID, Name: r.Name, Group: derefStrPub(r.GroupName),
			Localized:      traitLocalized(r.NameZh, r.NameZhProv),
			GroupLocalized: traitLocalized(derefStrPub(r.GroupNameZh), derefI16Pub(r.GroupNameZhProv)),
			Spoiler:        r.SpoilerLevel, Sexual: r.Sexual, Lie: r.Lie,
		})
	}
	return out, nil
}

// The trait vocabulary's Chinese column is Simplified: a census of the 3,327
// filled rows found 738 carrying simplified-only characters and none carrying a
// traditional-only one, so the locale key is zh-Hans rather than a bare zh.
func traitLocalized(nameZh string, provenance int16) map[string]dto.PublicLocalizedName {
	out := map[string]dto.PublicLocalizedName{}
	if nameZh == "" {
		return out
	}
	out["zh-Hans"] = dto.PublicLocalizedName{
		Value:   nameZh,
		Kind:    aliasKindKey(model.AliasKindTranslation),
		Machine: provenance == model.TraitNameZhProvenanceMachine,
	}
	return out
}

// sourceDerived is catalog_source 18 ("derived", first-party extraction).
// Wave-178 panel verdict (refs/proj/178): among machine intro rows a derived
// extraction outranks translated rows; source rows still win via provenance.
const sourceDerived = 18

func (s *PublicService) characterIntros(ctx context.Context, characterID int64) ([]dto.PublicIntro, error) {
	var rows []struct {
		Lang       string
		Intro      string
		SourceID   int16 `gorm:"column:source_id"`
		Provenance int16 `gorm:"column:provenance"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT lang, intro, source_id, provenance
		FROM catalog_character_intro WHERE character_id = ?
		ORDER BY lang, provenance, `+editspec.HumanLaneFirstSQL("source_id", "provenance")+
		`, (provenance = 1 AND source_id = ?) DESC, source_id`,
		characterID, sourceDerived).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicIntro, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Lang] {
			continue
		}
		seen[r.Lang] = true
		out = append(out, dto.PublicIntro{Lang: r.Lang, Intro: r.Intro, Source: s.sourceKey(r.SourceID), Machine: r.Provenance == 1})
	}
	return out, nil
}

func (s *PublicService) nameAliases(ctx context.Context, nameID int64) ([]dto.PublicAlias, map[string]dto.PublicLocalizedName, error) {
	rows, err := s.entityAliases(ctx, creditNameAliasSource, nameID)
	if err != nil {
		return nil, nil, err
	}
	return richAliases(rows), localizedNames(rows), nil
}

func (s *PublicService) nameIntros(ctx context.Context, nameID, personID int64) ([]dto.PublicIntro, error) {
	var personRows []struct {
		Lang       string
		Intro      string
		SourceID   int16 `gorm:"column:source_id"`
		Provenance int16 `gorm:"column:provenance"`
	}
	if personID > 0 {
		if err := s.db.WithContext(ctx).Raw(`SELECT lang, intro, source_id, provenance
			FROM catalog_person_intro WHERE person_id = ?
			ORDER BY lang, provenance, source_id`, personID).Scan(&personRows).Error; err != nil {
			return nil, err
		}
	}

	var bridged []struct {
		Summary  string
		SourceID int16 `gorm:"column:source_id"`
	}
	// The ref side has to be materialised, and the join has to be numeric. Casting
	// person.id to text instead makes person_pkey unusable, and the planner
	// answered this with a Parallel Seq Scan over all 101k rows / 173MB of
	// src_bangumi.person: 133ms and 20,681 buffers to return one row, on a lane the
	// forum's staff page drives. MATERIALIZED is what keeps the bigint cast safe —
	// it pins the ref lookup ahead of the cast so external_id is only ever cast for
	// rows already filtered to bangumi person links; the regexp guard covers the
	// rest, because a cast failure here would be a 500, not a missing intro.
	// Measured on production after the change: 0.33ms, 14 buffers, same row.
	if err := s.db.WithContext(ctx).Raw(`
		WITH ref AS MATERIALIZED (
			SELECT r.external_id, r.source_id
			FROM catalog_external_ref r
			JOIN catalog_source s ON s.id = r.source_id AND s.key = 'bangumi'
			WHERE r.entity_type = 1 AND r.entity_id = ? AND r.link_kind = 0
				AND r.external_id ~ '^[0-9]+$'
		)
		SELECT p.summary, ref.source_id
		FROM ref
		JOIN src_bangumi.person p ON p.id = ref.external_id::bigint
		WHERE coalesce(p.summary, '') <> ''
		ORDER BY ref.external_id LIMIT 1`, nameID).Scan(&bridged).Error; err != nil {
		return nil, err
	}

	out := make([]dto.PublicIntro, 0, len(personRows)+len(bridged))
	seen := map[string]bool{}
	add := func(lang, intro, source string, machine bool) {
		if lang == "" || seen[lang] {
			return
		}
		seen[lang] = true
		out = append(out, dto.PublicIntro{Lang: lang, Intro: intro, Source: source, Machine: machine})
	}
	for _, r := range personRows {
		if r.Provenance == 0 {
			add(r.Lang, r.Intro, s.sourceKey(r.SourceID), false)
		}
	}
	for _, r := range bridged {
		add(introLang(r.Summary), r.Summary, s.sourceKey(r.SourceID), false)
	}
	for _, r := range personRows {
		if r.Provenance == 1 {
			add(r.Lang, r.Intro, s.sourceKey(r.SourceID), true)
		}
	}
	return out, nil
}

func introLang(t string) string {
	for _, r := range t {
		if (r >= 'ぁ' && r <= 'ん') || (r >= 'ァ' && r <= 'ヶ') {
			return "ja"
		}
	}
	return "zh-Hans"
}

func (s *PublicService) publicSeriesSiblings(sibs []SeriesSiblingRow, nsfw bool, limits map[int64]bool) []dto.PublicWorkBrief {
	out := make([]dto.PublicWorkBrief, 0, len(sibs))
	for _, sb := range sibs {
		if !nsfw && isR18(sb.ContentRating) {
			continue
		}
		out = append(out, dto.PublicWorkBrief{
			ID: sb.WorkID, Medium: s.mediumKey(sb.MediumID), DisplayName: sb.DisplayName,
			ContentRating: contentRatingKey(sb.ContentRating),
			ClaimedBy:     claimedBy(sb.Site, sb.ProductWorkID, sb.ClaimState, limits[sb.WorkID], sb.ContentRating),
		})
	}
	return out
}
