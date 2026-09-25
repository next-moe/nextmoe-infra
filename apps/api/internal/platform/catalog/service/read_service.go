package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sort"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var charAttrColumns = []string{
	"birthday_month", "birthday_day", "blood_type", "height_cm", "weight_kg",
	"bust_cm", "waist_cm", "hip_cm", "cup", "gender",
}

func decodeCharExtra(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

func attrSources(raw datatypes.JSON) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var doc map[string][]struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := map[string]string{}
	for _, col := range charAttrColumns {
		if entries := doc[col]; len(entries) > 0 && entries[0].Source != "" {
			out[col] = entries[0].Source
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const sourceKeyDlsite = "dlsite"

const sourceKeyCurated = "curated"

// curatedSourceKeys is that source's key resolved DUAL-READ: both spellings,
// whichever the row currently carries.
//
// The rename lands through the seed, which runs as its own migration job before
// this service starts. That ordering is not something this code can rely on: a
// redeploy that does not re-pull an image runs yesterday's binary against
// today's registry (and a rollback runs it deliberately). A single-spelling
// lookup in that window resolves to id 0 — and id 0 is not an error here, it is
// a silently wrong source attribution on every bridged row. Accepting both
// spellings makes the two orderings indistinguishable.
//
// The stale spelling can be dropped once the window has closed; it matches
// nothing by itself, so keeping it costs one array element.
var curatedSourceKeys = []string{sourceKeyCurated, "galgame_wiki"}

func curatedSourceID(byKey map[string]int16) int16 {
	for _, k := range curatedSourceKeys {
		if id, ok := byKey[k]; ok {
			return id
		}
	}
	return 0
}

const siteGalgameWiki = "galgame_wiki"

type ReadService struct{ db *gorm.DB }

func NewReadService(db *gorm.DB) *ReadService { return &ReadService{db: db} }

var ErrWorkNotFound = stderrors.New("catalog: no work for anchor")

type WorkDetail struct {
	Work           model.CatalogWork
	Titles         []WorkTitleRow
	Releases       []ReleaseDetail
	Labels         []LabelAttribution
	Refs           []RefDetail
	Characters     []WorkCharacterRow
	Intros         []WorkIntroRow
	Covers         []WorkCoverRow
	Screenshots    []WorkScreenshotRow
	Ratings        []WorkRatingRow
	Tags           []WorkTagRow
	Popularity     []WorkPopularityRow
	Playtimes      []WorkPlaytimeRow
	Series         []WorkSeriesRow
	Platforms      []WorkPlatformRow
	Relations      []WorkRelationRow
	SeriesSiblings []SeriesSiblingRow
}

type WorkIntroRow struct {
	Lang     string
	Intro    string
	SourceID int16
	Machine  bool
}

type WorkCoverRow struct {
	ID             int64
	ImageHash      string
	Kind           string
	PortraitPinned bool
	SortOrder      int
	Sexual         int16
	Violence       int16
	SourceID       int16
}

type WorkScreenshotRow struct {
	ImageHash string
	Caption   string
	SortOrder int
	Sexual    int16
	Violence  int16
	SourceID  int16
}

type WorkCharacterRow struct {
	CharacterID int64
	DisplayName string
	Latin       *string
	Gender      *int16
	Kind        int16
	Spoiler     int16
	ImageHash   *string
	FigureHash  *string
	Identity    string
	Va          []WorkCharacterVARow
}

type WorkCharacterVARow struct {
	CreditNameID int64
	Name         string
	Lang         string
	Latin        *string
	PersonID     *int64
}

type RefDetail struct {
	Source     string
	ExternalID string
	EntityType int16
	ReleaseID  int64
}

type ReleaseDetail struct {
	Release model.CatalogRelease
	Anchors []AnchorDetail
}

type AnchorDetail struct {
	Source     string
	ExternalID string
	LinkKind   int16
	MatchedBy  string
}

type LabelAttribution struct {
	LabelID     int64
	DisplayName string
	LabelKind   int16
	Kind        int16
	Lang        string
	LogoHash    string
}

func (s *ReadService) WorkByAnchor(ctx context.Context, sourceKey, externalID string) (*WorkDetail, error) {
	db := s.db.WithContext(ctx)

	var srcID int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, sourceKey).Scan(&srcID).Error; err != nil {
		return nil, err
	}
	if srcID == 0 {
		return nil, ErrWorkNotFound
	}

	var ref struct {
		EntityType int16
		EntityID   int64
	}
	if err := db.Raw(`SELECT entity_type, entity_id FROM catalog_external_ref
		WHERE source_id = ? AND external_id = ? AND entity_type IN (?, ?)
		ORDER BY link_kind ASC, entity_type ASC LIMIT 1`,
		srcID, externalID, model.EntityTypeWork, model.EntityTypeRelease).Scan(&ref).Error; err != nil {
		return nil, err
	}
	if ref.EntityID == 0 {
		return nil, ErrWorkNotFound
	}

	workID := ref.EntityID
	if ref.EntityType == model.EntityTypeRelease {
		if err := db.Raw(`SELECT work_id FROM catalog_release WHERE id = ?`, ref.EntityID).Scan(&workID).Error; err != nil {
			return nil, err
		}
	}
	return s.loadWorkDetail(ctx, workID, workDetailOpts{withRelations: true})
}

func (s *ReadService) WorkByID(ctx context.Context, workID int64, spoilers int16) (*WorkDetail, error) {
	return s.loadWorkDetail(ctx, workID, workDetailOpts{spoilers: spoilers, withRelations: true})
}

func (s *ReadService) WorkByIDIncludeHidden(ctx context.Context, workID int64, spoilers int16) (*WorkDetail, error) {
	return s.loadWorkDetail(ctx, workID, workDetailOpts{spoilers: spoilers, includeHidden: true, withRelations: true})
}

// WorkByIDPublic is the only entry point that may leave Relations unloaded: the
// public face is the only caller whose response omits the block unless the
// caller spent an ?include= token on it. Every other face renders relations
// unconditionally and must keep passing withRelations.
func (s *ReadService) WorkByIDPublic(ctx context.Context, workID int64, spoilers int16, withRelations bool, fields PublicFields) (*WorkDetail, error) {
	return s.loadWorkDetail(ctx, workID, workDetailOpts{spoilers: spoilers, withRelations: withRelations, fields: fields})
}

type workDetailOpts struct {
	spoilers      int16
	includeHidden bool
	withRelations bool
	// The zero PublicFields is inactive, so every face that does not offer
	// ?fields= keeps loading all fifteen blocks.
	fields PublicFields
}

func (s *ReadService) loadWorkDetail(ctx context.Context, workID int64, opts workDetailOpts) (*WorkDetail, error) {
	db := s.db.WithContext(ctx)
	var work model.CatalogWork
	if err := db.First(&work, workID).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorkNotFound
		}
		return nil, err
	}

	detail := &WorkDetail{Work: work}
	subj := claimSubject{WorkID: work.ID}
	want := opts.fields.Wants

	if want("titles", "latin", "localized") {
		titles, err := s.loadWorkDetailTitles(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Titles = titles[work.ID]
	}

	// release_date and the release-level half of refs are both derived from
	// the release rows, so either of them alone still pays for this query.
	if want("releases", "refs", "release_date") {
		rels, err := s.loadWorkReleases(ctx, workID, opts.includeHidden)
		if err != nil {
			return nil, err
		}
		detail.Releases = rels
	}

	if want("labels") {
		if err := db.Raw(`SELECT wl.label_id, l.display_name, l.kind AS label_kind, wl.kind AS kind, l.lang, l.logo_hash
			FROM catalog_work_label wl JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
			WHERE wl.work_id = ? ORDER BY wl.kind, l.display_name`, workID).Scan(&detail.Labels).Error; err != nil {
			return nil, err
		}
	}

	if want("refs") {
		var workRefs []struct {
			Source     string
			ExternalID string
		}
		if err := db.Raw(`SELECT s.key AS source, r.external_id
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
			ORDER BY s.key`, model.EntityTypeWork, workID, model.LinkKindExact).Scan(&workRefs).Error; err != nil {
			return nil, err
		}
		for _, wr := range workRefs {
			detail.Refs = append(detail.Refs, RefDetail{Source: wr.Source, ExternalID: wr.ExternalID, EntityType: model.EntityTypeWork})
		}
		for _, rd := range detail.Releases {
			for _, a := range rd.Anchors {
				if a.LinkKind == model.LinkKindExact {
					detail.Refs = append(detail.Refs, RefDetail{
						Source: a.Source, ExternalID: a.ExternalID,
						EntityType: model.EntityTypeRelease, ReleaseID: rd.Release.ID,
					})
				}
			}
		}
	}

	if want("characters") {
		chars, err := s.loadWorkCharacters(ctx, workID)
		if err != nil {
			return nil, err
		}
		detail.Characters = chars
	}

	if want("intros") {
		intros, err := s.loadWorkIntros(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Intros = intros[work.ID]
	}

	if want("covers", "cover_slots") {
		covers, err := s.loadWorkCovers(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Covers = covers[work.ID]
	}

	if want("screenshots") {
		shots, err := s.loadWorkScreenshots(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Screenshots = shots[work.ID]
	}

	if want("ratings") {
		ratings, err := s.loadWorkRatings(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Ratings = ratings[work.ID]
	}

	if want("tags") {
		tags, err := s.loadWorkTags(ctx, []claimSubject{subj}, opts.spoilers)
		if err != nil {
			return nil, err
		}
		detail.Tags = tags[work.ID]
	}

	if want("popularity") {
		popularity, err := s.loadWorkPopularity(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Popularity = popularity[work.ID]
	}

	if want("playtimes") {
		playtimes, err := s.loadWorkPlaytimes(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Playtimes = playtimes[work.ID]
	}

	if want("series") {
		series, err := s.loadWorkSeries(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Series = series[work.ID]
	}

	if want("platforms") {
		platforms, err := s.loadWorkPlatforms(ctx, []claimSubject{subj})
		if err != nil {
			return nil, err
		}
		detail.Platforms = platforms[work.ID]
	}

	if opts.withRelations {
		relations, err := s.loadWorkRelations(ctx, workID)
		if err != nil {
			return nil, err
		}
		detail.Relations = relations
	}

	if want("series_siblings") {
		siblings, err := s.loadSeriesSiblings(ctx, workID)
		if err != nil {
			return nil, err
		}
		detail.SeriesSiblings = siblings
	}
	return detail, nil
}

type claimSubject struct {
	WorkID int64
}

func (s *ReadService) loadWorkReleases(ctx context.Context, workID int64, includeHidden bool) ([]ReleaseDetail, error) {
	db := s.db.WithContext(ctx)
	var releases []model.CatalogRelease
	relQ := db.Where("work_id = ?", workID).Order("id")
	if includeHidden {
		relQ = relQ.Unscoped()
	}
	if err := relQ.Find(&releases).Error; err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, nil
	}
	relIDs := make([]int64, len(releases))
	for i, r := range releases {
		relIDs[i] = r.ID
	}
	var arows []struct {
		EntityID   int64
		Source     string
		ExternalID string
		LinkKind   int16
		MatchedBy  string
	}
	// Dead anchors are dropped here rather than downstream: this loader feeds
	// the release anchor rows of every read face at once, and the previous
	// shape filtered them only where release anchors fold into work-level
	// refs[] — so releases[].refs[] on the public face kept rendering an
	// anchor whose upstream record no longer exists.
	if err := db.Raw(`SELECT r.entity_id, s.key AS source, r.external_id, r.link_kind, r.matched_by
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id IN ? AND r.dead_at IS NULL
		ORDER BY r.link_kind, s.key`, model.EntityTypeRelease, relIDs).Scan(&arows).Error; err != nil {
		return nil, err
	}
	anchorsByRelease := make(map[int64][]AnchorDetail, len(releases))
	for _, a := range arows {
		anchorsByRelease[a.EntityID] = append(anchorsByRelease[a.EntityID],
			AnchorDetail{Source: a.Source, ExternalID: a.ExternalID, LinkKind: a.LinkKind, MatchedBy: a.MatchedBy})
	}
	out := make([]ReleaseDetail, 0, len(releases))
	for _, r := range releases {
		out = append(out, ReleaseDetail{Release: r, Anchors: anchorsByRelease[r.ID]})
	}
	return out, nil
}

func (s *ReadService) loadWorkIntros(ctx context.Context, subjects []claimSubject) (map[int64][]WorkIntroRow, error) {
	out := make(map[int64][]WorkIntroRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	return out, s.nativeWorkIntros(ctx, workIDs, out)
}

func (s *ReadService) nativeWorkIntros(ctx context.Context, workIDs []int64, out map[int64][]WorkIntroRow) error {
	db := s.db.WithContext(ctx)
	var rows []struct {
		WorkID     int64  `gorm:"column:work_id"`
		Lang       string `gorm:"column:lang"`
		Intro      string `gorm:"column:intro"`
		SourceID   int16  `gorm:"column:source_id"`
		Provenance int16  `gorm:"column:provenance"`
	}
	if err := db.Raw(`SELECT work_id, lang, intro, source_id, provenance FROM catalog_work_intro
		WHERE work_id IN ? ORDER BY work_id, lang, provenance, `+
		editspec.HumanLaneFirstSQL("source_id", "provenance")+`, source_id`, workIDs).Scan(&rows).Error; err != nil {
		return err
	}
	seen := make(map[int64]map[string]bool)
	for _, r := range rows {
		langs := seen[r.WorkID]
		if langs == nil {
			langs = map[string]bool{}
			seen[r.WorkID] = langs
		}
		if langs[r.Lang] {
			continue
		}
		langs[r.Lang] = true
		out[r.WorkID] = append(out[r.WorkID], WorkIntroRow{
			Lang: r.Lang, Intro: r.Intro, SourceID: r.SourceID, Machine: r.Provenance == 1,
		})
	}
	for id := range seen {
		sortIntros(out[id])
	}
	return nil
}

func sortIntros(intros []WorkIntroRow) {
	sort.Slice(intros, func(i, j int) bool { return intros[i].Lang < intros[j].Lang })
}

func (s *ReadService) loadWorkCovers(ctx context.Context, subjects []claimSubject) (map[int64][]WorkCoverRow, error) {
	out := make(map[int64][]WorkCoverRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	if err := s.nativeWorkCovers(ctx, workIDs, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ReadService) nativeWorkCovers(ctx context.Context, workIDs []int64, out map[int64][]WorkCoverRow) error {
	db := s.db.WithContext(ctx)
	var rows []struct {
		ID             int64  `gorm:"column:id"`
		WorkID         int64  `gorm:"column:work_id"`
		ImageHash      string `gorm:"column:image_hash"`
		SortOrder      int    `gorm:"column:sort_order"`
		Kind           string `gorm:"column:kind"`
		PortraitPinned bool   `gorm:"column:portrait_pinned"`
		Sexual         int16  `gorm:"column:sexual"`
		Violence       int16  `gorm:"column:violence"`
		SourceID       int16  `gorm:"column:source_id"`
	}
	if err := db.Raw(`SELECT id, work_id, image_hash, sort_order, kind, portrait_pinned, sexual, violence, source_id
		FROM catalog_work_cover WHERE work_id IN ?
		ORDER BY work_id, sort_order, image_hash`, workIDs).Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], WorkCoverRow{
			ID: r.ID, ImageHash: r.ImageHash, Kind: r.Kind, PortraitPinned: r.PortraitPinned,
			SortOrder: r.SortOrder, Sexual: r.Sexual, Violence: r.Violence, SourceID: r.SourceID,
		})
	}
	return nil
}

func (s *ReadService) loadWorkScreenshots(ctx context.Context, subjects []claimSubject) (map[int64][]WorkScreenshotRow, error) {
	out := make(map[int64][]WorkScreenshotRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	if err := s.nativeWorkScreenshots(ctx, workIDs, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ReadService) nativeWorkScreenshots(ctx context.Context, workIDs []int64, out map[int64][]WorkScreenshotRow) error {
	db := s.db.WithContext(ctx)
	var rows []struct {
		WorkID    int64  `gorm:"column:work_id"`
		ImageHash string `gorm:"column:image_hash"`
		SortOrder int    `gorm:"column:sort_order"`
		Caption   string `gorm:"column:caption"`
		Sexual    int16  `gorm:"column:sexual"`
		Violence  int16  `gorm:"column:violence"`
		SourceID  int16  `gorm:"column:source_id"`
	}
	if err := db.Raw(`SELECT work_id, image_hash, sort_order, caption, sexual, violence, source_id
		FROM catalog_work_screenshot WHERE work_id IN ?
		ORDER BY work_id, source_id, sort_order, image_hash`, workIDs).Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], WorkScreenshotRow{
			ImageHash: r.ImageHash, Caption: r.Caption,
			SortOrder: r.SortOrder, Sexual: r.Sexual, Violence: r.Violence, SourceID: r.SourceID,
		})
	}
	return nil
}

func (s *ReadService) loadWorkCharacters(ctx context.Context, workID int64) ([]WorkCharacterRow, error) {
	db := s.db.WithContext(ctx)

	var edges []struct {
		CharacterID int64   `gorm:"column:character_id"`
		DisplayName string  `gorm:"column:display_name"`
		Latin       *string `gorm:"column:latin"`
		Gender      *int16  `gorm:"column:gender"`
		ImageHash   *string `gorm:"column:image_hash"`
		FigureHash  *string `gorm:"column:figure_hash"`
		Kind        int16   `gorm:"column:kind"`
		Spoiler     int16   `gorm:"column:spoiler"`
		Identity    string  `gorm:"column:identity"`
	}
	if err := db.Raw(`SELECT wc.character_id, ch.display_name, ch.latin, ch.gender, ch.image_hash, ch.figure_hash, wc.kind, wc.spoiler,
		`+editspec.RosterIdentitySQL("wc")+` AS identity
		FROM catalog_work_character wc JOIN catalog_character ch ON ch.id = wc.character_id
		WHERE wc.work_id = ? AND ch.deleted_at IS NULL
		  AND `+editspec.NotSuppressedRosterSQL("wc"), workID).Scan(&edges).Error; err != nil {
		return nil, err
	}

	var creds []struct {
		CharacterID  int64   `gorm:"column:character_id"`
		DisplayName  string  `gorm:"column:display_name"`
		Latin        *string `gorm:"column:latin"`
		Gender       *int16  `gorm:"column:gender"`
		ImageHash    *string `gorm:"column:image_hash"`
		FigureHash   *string `gorm:"column:figure_hash"`
		CreditNameID int64   `gorm:"column:credit_name_id"`
		Name         string  `gorm:"column:name"`
		NameLang     string  `gorm:"column:name_lang"`
		NameLatin    *string `gorm:"column:name_latin"`
		PersonID     *int64  `gorm:"column:person_id"`
	}
	// The person link follows link_visibility here exactly as the credit-name
	// detail face applies it: a hidden link publishes no person_id.
	if err := db.Raw(`SELECT DISTINCT c.character_id, ch.display_name, ch.latin, ch.gender, ch.image_hash, ch.figure_hash,
		cn.id AS credit_name_id, cn.name, cn.lang AS name_lang, cn.latin AS name_latin,
		CASE WHEN cn.link_visibility = 0 THEN cn.person_id END AS person_id
		FROM catalog_credit c
		JOIN catalog_character ch ON ch.id = c.character_id
		JOIN catalog_credit_name cn ON cn.id = c.credit_name_id
		WHERE c.work_id = ? AND c.character_id IS NOT NULL AND ch.deleted_at IS NULL
		  AND `+editspec.NotSuppressedCreditSQL("c"), workID).Scan(&creds).Error; err != nil {
		return nil, err
	}

	byID := make(map[int64]*WorkCharacterRow, len(edges)+len(creds))
	for _, e := range edges {
		byID[e.CharacterID] = &WorkCharacterRow{
			CharacterID: e.CharacterID, DisplayName: e.DisplayName, Latin: e.Latin,
			Gender: e.Gender, Kind: e.Kind, Spoiler: e.Spoiler, ImageHash: e.ImageHash,
			FigureHash: e.FigureHash, Identity: e.Identity,
		}
	}
	for _, c := range creds {
		row, ok := byID[c.CharacterID]
		if !ok {
			row = &WorkCharacterRow{
				CharacterID: c.CharacterID, DisplayName: c.DisplayName, Latin: c.Latin,
				Gender: c.Gender, Kind: model.WorkCharacterKindUnknown, ImageHash: c.ImageHash,
				FigureHash: c.FigureHash,
			}
			byID[c.CharacterID] = row
		}
		row.Va = append(row.Va, WorkCharacterVARow{
			CreditNameID: c.CreditNameID, Name: c.Name, Lang: c.NameLang,
			Latin: c.NameLatin, PersonID: c.PersonID,
		})
	}

	out := make([]WorkCharacterRow, 0, len(byID))
	for _, r := range byID {
		va := r.Va
		sort.Slice(va, func(i, j int) bool {
			if va[i].Name != va[j].Name {
				return va[i].Name < va[j].Name
			}
			return va[i].CreditNameID < va[j].CreditNameID
		})
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := kindRank(out[i].Kind), kindRank(out[j].Kind)
		if ri != rj {
			return ri < rj
		}
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].CharacterID < out[j].CharacterID
	})
	return out, nil
}

func kindRank(k int16) int16 {
	if k == model.WorkCharacterKindUnknown {
		return 99
	}
	return k
}

type LabelWork struct {
	WorkID        int64  `gorm:"column:work_id"`
	DisplayName   string `gorm:"column:display_name"`
	MediumID      int16  `gorm:"column:medium_id"`
	ContentRating int16  `gorm:"column:content_rating"`
	Status        int16  `gorm:"column:status"`
	Kind          int16  `gorm:"column:kind"`
}

type LabelHead struct {
	ID          int64  `gorm:"column:id"`
	DisplayName string `gorm:"column:display_name"`
	Latin       string `gorm:"column:latin"`
	Kind        int16  `gorm:"column:kind"`
	Lang        string `gorm:"column:lang"`
	LogoHash    string `gorm:"column:logo_hash"`
}

func (s *ReadService) LabelWorks(ctx context.Context, labelID int64, limit, offset int) (head *LabelHead, items []LabelWork, total int64, err error) {
	db := s.db.WithContext(ctx)
	var h LabelHead
	if err = db.Raw(`SELECT id, display_name, latin, kind, lang, logo_hash FROM catalog_label
		WHERE id = ? AND deleted_at IS NULL`, labelID).Scan(&h).Error; err != nil {
		return nil, nil, 0, err
	}
	if h.ID != 0 {
		head = &h
	}
	if err = db.Raw(`SELECT count(*) FROM catalog_work_label wl JOIN catalog_work w ON w.id = wl.work_id
		WHERE wl.label_id = ? AND w.deleted_at IS NULL AND w.status = ?`, labelID, model.WorkStatusLive).Scan(&total).Error; err != nil {
		return nil, nil, 0, err
	}
	err = db.Raw(`SELECT w.id AS work_id, w.display_name, w.medium_id, w.content_rating, w.status, wl.kind
		FROM catalog_work_label wl JOIN catalog_work w ON w.id = wl.work_id
		WHERE wl.label_id = ? AND w.deleted_at IS NULL AND w.status = ? ORDER BY w.id LIMIT ? OFFSET ?`,
		labelID, model.WorkStatusLive, limit, offset).Scan(&items).Error
	return head, items, total, err
}

type WorkSearchHit struct {
	WorkID        int64  `gorm:"column:work_id"`
	DisplayName   string `gorm:"column:display_name"`
	MediumID      int16  `gorm:"column:medium_id"`
	ContentRating int16  `gorm:"column:content_rating"`
	Status        int16  `gorm:"column:status"`
	Site          string `gorm:"column:site"`
	DlsiteID      string `gorm:"-"`
}

func (s *ReadService) SearchWorks(ctx context.Context, q string, mediumID int16, limit int, claimStates []string, site string) ([]WorkSearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	db := s.db.WithContext(ctx)

	claimGate, claimArgs := claimStateWhere(claimStates)
	if claimGate != "" {
		claimGate = " AND " + claimGate
	}
	siteGate, siteArgs := "", []any(nil)
	if site != "" {
		siteGate, siteArgs = " AND w.site = ?", []any{site}
	}

	var hits []WorkSearchHit
	args := []any{model.WorkStatusLive, mediumID, mediumID}
	args = append(args, claimArgs...)
	args = append(args, siteArgs...)
	args = append(args, q, limit)
	if err := db.Raw(`
		SELECT w.id AS work_id, w.display_name, w.medium_id, w.content_rating,
		       w.status, COALESCE(w.site, '') AS site
		FROM catalog_work w
		WHERE w.deleted_at IS NULL
		  AND w.status = ?
		  AND (? <= 0 OR w.medium_id = ?)`+claimGate+siteGate+`
		  AND EXISTS (
		      SELECT 1 FROM catalog_work_title t
		      WHERE t.work_id = w.id
		        AND t.title_norm LIKE '%' || lower(normalize(?, NFKC)) || '%'
		        AND `+editspec.NotSuppressedWorkTitleSQL("t")+`
		  )
		ORDER BY w.id
		LIMIT ?`, args...).Scan(&hits).Error; err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return hits, nil
	}

	workIDs := make([]int64, len(hits))
	for i := range hits {
		workIDs[i] = hits[i].WorkID
	}
	var refs []struct {
		WorkID     int64  `gorm:"column:work_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.Raw(`
		SELECT x.work_id, x.external_id FROM (
			SELECT r.entity_id AS work_id, r.external_id, r.link_kind
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			WHERE s.key = ? AND r.entity_type = ? AND r.entity_id IN ? AND r.dead_at IS NULL
			UNION ALL
			SELECT rel.work_id, r.external_id, r.link_kind
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			  JOIN catalog_release rel ON rel.id = r.entity_id
			WHERE s.key = ? AND r.entity_type = ? AND rel.work_id IN ? AND r.dead_at IS NULL
		) x ORDER BY x.work_id, x.link_kind`,
		sourceKeyDlsite, model.EntityTypeWork, workIDs,
		sourceKeyDlsite, model.EntityTypeRelease, workIDs).Scan(&refs).Error; err != nil {
		return nil, err
	}
	firstDlsite := make(map[int64]string, len(refs))
	for _, r := range refs {
		if _, ok := firstDlsite[r.WorkID]; !ok {
			firstDlsite[r.WorkID] = r.ExternalID
		}
	}
	for i := range hits {
		hits[i].DlsiteID = firstDlsite[hits[i].WorkID]
	}
	return hits, nil
}

type WorkBriefRow struct {
	WorkID        int64  `gorm:"column:work_id"`
	DisplayName   string `gorm:"column:display_name"`
	MediumID      int16  `gorm:"column:medium_id"`
	ContentRating int16  `gorm:"column:content_rating"`
	Status        int16  `gorm:"column:status"`
	Site          string `gorm:"column:site"`
}

func (s *ReadService) workBriefs(ctx context.Context, ids []int64) (map[int64]WorkBriefRow, error) {
	var rows []WorkBriefRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id AS work_id, display_name, medium_id, content_rating, status, COALESCE(site, '') AS site
		FROM catalog_work WHERE id IN ? AND deleted_at IS NULL AND status = ?`, ids, model.WorkStatusLive).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]WorkBriefRow, len(rows))
	for _, r := range rows {
		m[r.WorkID] = r
	}
	return m, nil
}

type NameHeadRow struct {
	ID             int64  `gorm:"column:id"`
	Name           string `gorm:"column:name"`
	Lang           string `gorm:"column:lang"`
	Latin          *string
	PersonID       *int64 `gorm:"column:person_id"`
	LinkVisibility int16  `gorm:"column:link_visibility"`
	PhotoHash      string `gorm:"column:photo_hash"`
	Gender         *int16 `gorm:"column:gender"`
	BirthY         *int16 `gorm:"column:birth_y"`
	BirthM         *int16 `gorm:"column:birth_m"`
	BirthD         *int16 `gorm:"column:birth_d"`
}

type SiblingNameRow struct {
	ID    int64  `gorm:"column:id"`
	Name  string `gorm:"column:name"`
	Lang  string `gorm:"column:lang"`
	Latin *string
}

// nameWorkScope is shared by NameWorks' total and its page: a total of 40 that
// pages out 38 is a defect on its own.
var nameWorkScope = `FROM catalog_credit c WHERE c.credit_name_id = ? AND ` +
	editspec.NotSuppressedCreditSQL("c") + ` AND ` + editspec.LiveWorkSQL("c.work_id")

type NameWorkRoleRow struct {
	WorkID      int64   `gorm:"column:work_id"`
	RoleID      int64   `gorm:"column:role_id"`
	RoleKey     string  `gorm:"column:role_key"`
	RoleNameCN  string  `gorm:"column:role_name_cn"`
	RoleNameJA  string  `gorm:"column:role_name_ja"`
	CharacterID *int64  `gorm:"column:character_id"`
	CharacterNM *string `gorm:"column:character_nm"`
	Identity    string  `gorm:"column:identity"`
}

type NameWorkDetail struct {
	Brief WorkBriefRow
	Roles []NameWorkRoleRow
}

type NameWorksResult struct {
	Head     *NameHeadRow
	Siblings []SiblingNameRow
	Works    []NameWorkDetail
	Total    int64
}

func (s *ReadService) NameWorks(ctx context.Context, nameID int64, limit, offset int) (*NameWorksResult, error) {
	db := s.db.WithContext(ctx)

	var head NameHeadRow
	if err := db.Raw(`SELECT cn.id, cn.name, cn.lang, cn.latin, cn.person_id, cn.link_visibility,
		COALESCE(p.photo_hash, '') AS photo_hash, p.gender, p.birth_y, p.birth_m, p.birth_d
		FROM catalog_credit_name cn
		LEFT JOIN catalog_person p ON p.id = cn.person_id AND p.deleted_at IS NULL
		WHERE cn.id = ?`, nameID).Scan(&head).Error; err != nil {
		return nil, err
	}
	if head.ID == 0 {
		return &NameWorksResult{}, nil
	}
	res := &NameWorksResult{Head: &head}

	if head.LinkVisibility != model.LinkVisibilityPublic {
		head.PersonID = nil
		head.PhotoHash = ""
		head.Gender, head.BirthY, head.BirthM, head.BirthD = nil, nil, nil, nil
	} else if head.PersonID != nil {
		if err := db.Raw(`SELECT id, name, lang, latin FROM catalog_credit_name
			WHERE person_id = ? AND id <> ? AND link_visibility = ?
			ORDER BY id`, *head.PersonID, nameID, model.LinkVisibilityPublic).Scan(&res.Siblings).Error; err != nil {
			return nil, err
		}
	}

	if err := db.Raw(`SELECT count(DISTINCT c.work_id) `+nameWorkScope,
		nameID).Scan(&res.Total).Error; err != nil {
		return nil, err
	}
	var workIDs []int64
	if err := db.Raw(`SELECT DISTINCT c.work_id `+nameWorkScope+`
		ORDER BY c.work_id LIMIT ? OFFSET ?`, nameID, limit, offset).Scan(&workIDs).Error; err != nil {
		return nil, err
	}
	if len(workIDs) == 0 {
		return res, nil
	}
	briefs, err := s.workBriefs(ctx, workIDs)
	if err != nil {
		return nil, err
	}
	var roleRows []NameWorkRoleRow
	if err := db.Raw(`SELECT c.work_id, c.role_id, ro.key AS role_key,
		ro.name_cn AS role_name_cn, ro.name_ja AS role_name_ja,
		c.character_id, ch.display_name AS character_nm,
		`+editspec.CreditIdentitySQL("c")+` AS identity
		FROM catalog_credit c
		JOIN catalog_role ro ON ro.id = c.role_id
		LEFT JOIN catalog_character ch ON ch.id = c.character_id
		WHERE c.credit_name_id = ? AND c.work_id IN ?
		  AND `+editspec.NotSuppressedCreditSQL("c")+`
		ORDER BY c.work_id, c.role_id, character_nm NULLS FIRST`, nameID, workIDs).Scan(&roleRows).Error; err != nil {
		return nil, err
	}
	rolesByWork := make(map[int64][]NameWorkRoleRow, len(workIDs))
	for _, r := range roleRows {
		rolesByWork[r.WorkID] = append(rolesByWork[r.WorkID], r)
	}
	for _, wid := range workIDs {
		b, ok := briefs[wid]
		if !ok {
			continue
		}
		res.Works = append(res.Works, NameWorkDetail{Brief: b, Roles: rolesByWork[wid]})
	}
	return res, nil
}

// unionWorks is shared by CharacterWorks' total and its page. Suppressing a VA
// credit therefore removes the work from this union too when no roster edge
// carries the character: charter ruling 2 (read paths exclude suppressed rows
// uniformly) is not given an exemption for the union's existence half.
var unionWorks = `SELECT wc.work_id FROM catalog_work_character wc WHERE wc.character_id = ? AND ` +
	editspec.NotSuppressedRosterSQL("wc") + ` AND ` + editspec.LiveWorkSQL("wc.work_id") + `
	UNION SELECT c.work_id FROM catalog_credit c WHERE c.character_id = ? AND ` +
	editspec.NotSuppressedCreditSQL("c") + ` AND ` + editspec.LiveWorkSQL("c.work_id")

type CharacterHeadRow struct {
	ID          int64  `gorm:"column:id"`
	DisplayName string `gorm:"column:display_name"`
	Lang        string `gorm:"column:lang"`
	Latin       *string
}

type VoiceNameRow struct {
	CreditNameID int64  `gorm:"column:credit_name_id"`
	Name         string `gorm:"column:name"`
	Lang         string `gorm:"column:lang"`
	Latin        *string
	PersonID     *int64 `gorm:"column:person_id"`
}

type CharacterWorkDetail struct {
	Brief    WorkBriefRow
	Kind     int16
	Spoiler  int16
	Identity string
	Voiced   bool
	Voices   []VoiceNameRow
}

type CharacterWorksResult struct {
	Head  *CharacterHeadRow
	Works []CharacterWorkDetail
	Total int64
}

func (s *ReadService) CharacterWorks(ctx context.Context, characterID int64, limit, offset int) (*CharacterWorksResult, error) {
	db := s.db.WithContext(ctx)

	var head CharacterHeadRow
	if err := db.Raw(`SELECT id, display_name, lang, latin FROM catalog_character
		WHERE id = ? AND deleted_at IS NULL`, characterID).Scan(&head).Error; err != nil {
		return nil, err
	}
	if head.ID == 0 {
		return &CharacterWorksResult{}, nil
	}
	res := &CharacterWorksResult{Head: &head}

	if err := db.Raw(`SELECT count(*) FROM (`+unionWorks+`) u`,
		characterID, characterID).Scan(&res.Total).Error; err != nil {
		return nil, err
	}
	var workIDs []int64
	if err := db.Raw(`SELECT work_id FROM (`+unionWorks+`) u ORDER BY work_id LIMIT ? OFFSET ?`,
		characterID, characterID, limit, offset).Scan(&workIDs).Error; err != nil {
		return nil, err
	}
	if len(workIDs) == 0 {
		return res, nil
	}
	briefs, err := s.workBriefs(ctx, workIDs)
	if err != nil {
		return nil, err
	}

	var kindRows []struct {
		WorkID   int64  `gorm:"column:work_id"`
		Kind     int16  `gorm:"column:kind"`
		Spoiler  int16  `gorm:"column:spoiler"`
		Identity string `gorm:"column:identity"`
	}
	if err := db.Raw(`SELECT wc.work_id, wc.kind, wc.spoiler, `+editspec.RosterIdentitySQL("wc")+` AS identity
		FROM catalog_work_character wc
		WHERE wc.character_id = ? AND wc.work_id IN ?
		  AND `+editspec.NotSuppressedRosterSQL("wc"), characterID, workIDs).Scan(&kindRows).Error; err != nil {
		return nil, err
	}
	kindByWork := make(map[int64]int16, len(kindRows))
	spoilerByWork := make(map[int64]int16, len(kindRows))
	identityByWork := make(map[int64]string, len(kindRows))
	for _, k := range kindRows {
		kindByWork[k.WorkID] = k.Kind
		spoilerByWork[k.WorkID] = k.Spoiler
		identityByWork[k.WorkID] = k.Identity
	}

	var voiceRows []struct {
		WorkID       int64  `gorm:"column:work_id"`
		CreditNameID int64  `gorm:"column:credit_name_id"`
		Name         string `gorm:"column:name"`
		Lang         string `gorm:"column:lang"`
		Latin        *string
		PersonID     *int64 `gorm:"column:person_id"`
	}
	if err := db.Raw(`SELECT DISTINCT c.work_id, cn.id AS credit_name_id, cn.name, cn.lang, cn.latin,
			CASE WHEN cn.link_visibility = 0 THEN cn.person_id END AS person_id
		FROM catalog_credit c JOIN catalog_credit_name cn ON cn.id = c.credit_name_id
		WHERE c.character_id = ? AND c.work_id IN ?
		  AND `+editspec.NotSuppressedCreditSQL("c")+`
		ORDER BY c.work_id, cn.id`, characterID, workIDs).Scan(&voiceRows).Error; err != nil {
		return nil, err
	}
	voicesByWork := make(map[int64][]VoiceNameRow, len(workIDs))
	for _, v := range voiceRows {
		voicesByWork[v.WorkID] = append(voicesByWork[v.WorkID], VoiceNameRow{
			CreditNameID: v.CreditNameID, Name: v.Name, Lang: v.Lang, Latin: v.Latin, PersonID: v.PersonID,
		})
	}
	for _, wid := range workIDs {
		b, ok := briefs[wid]
		if !ok {
			continue
		}
		voices := voicesByWork[wid]
		res.Works = append(res.Works, CharacterWorkDetail{
			Brief: b, Kind: kindByWork[wid], Spoiler: spoilerByWork[wid],
			Identity: identityByWork[wid], Voiced: len(voices) > 0, Voices: voices,
		})
	}
	return res, nil
}

type CharacterDetail struct {
	ID            int64
	DisplayName   string
	Latin         *string
	Lang          string
	Gender        *int16
	Description   string
	InstanceOf    *int64
	ImageHash     *string
	FigureHash    *string
	BirthdayMonth *int16
	BirthdayDay   *int16
	BloodType     *int16
	HeightCm      *int16
	WeightKg      *int16
	BustCm        *int16
	WaistCm       *int16
	HipCm         *int16
	Cup           *string
	Extra         map[string]any
	AttrSources   map[string]string
	Aliases       []CharacterAliasRow
	Intros        []WorkIntroRow
	Traits        []CharacterTraitRow
}

type CharacterTraitRow struct {
	ID           int64   `gorm:"column:id"`
	Name         string  `gorm:"column:name"`
	NameZh       string  `gorm:"column:name_zh"`
	GroupTID     string  `gorm:"column:group_tid"`
	GroupName    *string `gorm:"column:group_name"`
	GroupNameZh  *string `gorm:"column:group_name_zh"`
	Sexual       bool    `gorm:"column:sexual"`
	SpoilerLevel int16   `gorm:"column:spoiler_level"`
	Lie          bool    `gorm:"column:lie"`
}

type CharacterAliasRow struct {
	ID                 int64   `gorm:"column:id"`
	Name               string  `gorm:"column:name"`
	Latin              *string `gorm:"column:latin"`
	Lang               string  `gorm:"column:lang"`
	Kind               int16   `gorm:"column:kind"`
	IsPrimaryForLocale bool    `gorm:"column:is_primary_for_locale"`
}

func (s *ReadService) CharacterByID(ctx context.Context, characterID int64, maxSpoiler int16) (*CharacterDetail, error) {
	db := s.db.WithContext(ctx)

	var head struct {
		ID              int64          `gorm:"column:id"`
		DisplayName     string         `gorm:"column:display_name"`
		Latin           *string        `gorm:"column:latin"`
		Lang            string         `gorm:"column:lang"`
		Gender          *int16         `gorm:"column:gender"`
		Description     string         `gorm:"column:description"`
		InstanceOf      *int64         `gorm:"column:instance_of"`
		ImageHash       *string        `gorm:"column:image_hash"`
		FigureHash      *string        `gorm:"column:figure_hash"`
		BirthdayMonth   *int16         `gorm:"column:birthday_month"`
		BirthdayDay     *int16         `gorm:"column:birthday_day"`
		BloodType       *int16         `gorm:"column:blood_type"`
		HeightCm        *int16         `gorm:"column:height_cm"`
		WeightKg        *int16         `gorm:"column:weight_kg"`
		BustCm          *int16         `gorm:"column:bust_cm"`
		WaistCm         *int16         `gorm:"column:waist_cm"`
		HipCm           *int16         `gorm:"column:hip_cm"`
		Cup             *string        `gorm:"column:cup"`
		Extra           datatypes.JSON `gorm:"column:extra"`
		FieldProvenance datatypes.JSON `gorm:"column:field_provenance"`
	}
	if err := db.Raw(`SELECT id, display_name, latin, lang, gender, description, instance_of, image_hash, figure_hash,
		birthday_month, birthday_day, blood_type, height_cm, weight_kg, bust_cm, waist_cm, hip_cm, cup,
		extra, field_provenance
		FROM catalog_character WHERE id = ? AND deleted_at IS NULL`, characterID).Scan(&head).Error; err != nil {
		return nil, err
	}
	if head.ID == 0 {
		return nil, nil
	}
	detail := &CharacterDetail{
		ID: head.ID, DisplayName: head.DisplayName, Latin: head.Latin, Lang: head.Lang,
		Gender: head.Gender, Description: head.Description, InstanceOf: head.InstanceOf, ImageHash: head.ImageHash,
		FigureHash:    head.FigureHash,
		BirthdayMonth: head.BirthdayMonth, BirthdayDay: head.BirthdayDay, BloodType: head.BloodType,
		HeightCm: head.HeightCm, WeightKg: head.WeightKg, BustCm: head.BustCm, WaistCm: head.WaistCm,
		HipCm: head.HipCm, Cup: head.Cup,
		Extra:       decodeCharExtra(head.Extra),
		AttrSources: attrSources(head.FieldProvenance),
	}
	if err := db.Raw(`SELECT a.id, a.name, a.latin, a.lang, a.kind, a.is_primary_for_locale
		FROM catalog_character_alias a WHERE a.character_id = ? AND `+
		editspec.NotSuppressedCharacterAliasSQL("a")+` ORDER BY a.id`,
		characterID).Scan(&detail.Aliases).Error; err != nil {
		return nil, err
	}
	var introRows []struct {
		Lang       string `gorm:"column:lang"`
		Intro      string `gorm:"column:intro"`
		SourceID   int16  `gorm:"column:source_id"`
		Provenance int16  `gorm:"column:provenance"`
	}
	if err := db.Raw(`SELECT lang, intro, source_id, provenance FROM catalog_character_intro
		WHERE character_id = ? ORDER BY lang, provenance, `+
		editspec.HumanLaneFirstSQL("source_id", "provenance")+`,
		(provenance = 1 AND source_id = ?) DESC, source_id`, characterID, sourceDerived).Scan(&introRows).Error; err != nil {
		return nil, err
	}
	seenLang := map[string]bool{}
	for _, r := range introRows {
		if seenLang[r.Lang] {
			continue
		}
		seenLang[r.Lang] = true
		detail.Intros = append(detail.Intros, WorkIntroRow{Lang: r.Lang, Intro: r.Intro, SourceID: r.SourceID, Machine: r.Provenance == 1})
	}
	sortIntros(detail.Intros)
	if err := db.Raw(`SELECT t.id, t.name, t.name_zh, t.group_tid,
			g.name AS group_name, g.name_zh AS group_name_zh,
			t.sexual_family AS sexual, l.spoiler_level, l.lie
		FROM catalog_character_trait_link l
		JOIN catalog_character_trait t ON t.id = l.trait_id
		LEFT JOIN catalog_character_trait g ON g.vndb_tid = t.group_tid
		WHERE l.character_id = ? AND l.spoiler_level <= ?
		ORDER BY t.group_tid, t.gorder, t.name`, characterID, maxSpoiler).
		Scan(&detail.Traits).Error; err != nil {
		return nil, err
	}
	return detail, nil
}

type CreditRow struct {
	WorkID       int64
	RoleID       int64
	RoleKey      string
	RoleNameCN   string
	RoleNameJA   string
	CreditNameID int64
	Name         string
	Lang         string
	Latin        *string
	CharacterID  *int64
	CharacterNM  *string
	LabelID      *int64
	LabelNM      *string
	Note         string
	SourceKey    *string
	Identity     string
}

func (s *ReadService) WorkCredits(ctx context.Context, workID int64) ([]CreditRow, error) {
	byWork, err := s.workCreditsFor(ctx, []int64{workID})
	if err != nil {
		return nil, err
	}
	return byWork[workID], nil
}

// workCreditsFor is the only place the credits read is spelled. The suppression
// predicate, the curated-lane ordering and the identity projection all live in
// it, so the works list hydrates credits by widening this query rather than
// growing a second copy that would drift away from the D9/D24 rules the detail
// face enforces.
//
// The last two ORDER BY terms are the tiebreak, and they are not decoration:
// uq_catalog_credit is (work_id, credit_name_id, role_id,
// COALESCE(character_id, 0)), so one voice actor on three characters of one work
// is three rows that tie on every earlier term. works/{id}/credits pages by
// OFFSET, and an unstable sort there duplicates or drops rows at a page
// boundary. COLLATE "C" pins src.key against the database's collation, which is
// not part of this contract.
func (s *ReadService) workCreditsFor(ctx context.Context, workIDs []int64) (map[int64][]CreditRow, error) {
	out := make(map[int64][]CreditRow, len(workIDs))
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []CreditRow
	if err := s.db.WithContext(ctx).Raw(`SELECT
		c.work_id, c.role_id, ro.key AS role_key, ro.name_cn AS role_name_cn, ro.name_ja AS role_name_ja,
		cn.id AS credit_name_id, cn.name, cn.lang, cn.latin,
		c.character_id, ch.display_name AS character_nm,
		c.label_id, la.display_name AS label_nm, c.note, src.key AS source_key,
		`+editspec.CreditIdentitySQL("c")+` AS identity
		FROM catalog_credit c
		JOIN catalog_role ro ON ro.id = c.role_id
		JOIN catalog_credit_name cn ON cn.id = c.credit_name_id
		LEFT JOIN catalog_character ch ON ch.id = c.character_id
		LEFT JOIN catalog_label la ON la.id = c.label_id
		LEFT JOIN catalog_source src ON src.id = c.source_id
		WHERE c.work_id IN ? AND `+editspec.NotSuppressedCreditSQL("c")+`
		ORDER BY c.work_id ASC, c.role_id ASC, `+editspec.HumanLaneFirstNoProvenanceSQL("c.source_id")+
		`, src.key COLLATE "C" ASC NULLS LAST, cn.id ASC, COALESCE(c.character_id, 0) ASC, c.id ASC`,
		workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], r)
	}
	return out, nil
}
