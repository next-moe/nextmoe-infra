package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

type aliasSource struct {
	table        string
	ownerCol     string
	ownerTable   string
	ownerNameCol string
	// live is the row-level suppression predicate, correlated on alias "a", and
	// empty for the families whose alias list is not a registered edit field yet.
	live string
}

var (
	labelAliasSource = aliasSource{
		table: "catalog_label_alias", ownerCol: "label_id",
		ownerTable: "catalog_label", ownerNameCol: "display_name",
		live: editspec.NotSuppressedLabelAliasSQL("a"),
	}
	characterAliasSource = aliasSource{
		table: "catalog_character_alias", ownerCol: "character_id",
		ownerTable: "catalog_character", ownerNameCol: "display_name",
		live: editspec.NotSuppressedCharacterAliasSQL("a"),
	}
	creditNameAliasSource = aliasSource{
		table: "catalog_name_alias", ownerCol: "credit_name_id",
		ownerTable: "catalog_credit_name", ownerNameCol: "name",
		live: editspec.NotSuppressedNameAliasSQL("a"),
	}
)

type displayAlias struct {
	Name       string `gorm:"column:name"`
	Lang       string `gorm:"column:lang"`
	Kind       int16  `gorm:"column:kind"`
	IsPrimary  bool   `gorm:"column:is_primary"`
	IsDisplay  bool   `gorm:"column:is_display"`
	Provenance int16  `gorm:"column:provenance"`
}

func (s *PublicService) entityAliases(ctx context.Context, src aliasSource, ownerID int64) ([]displayAlias, error) {
	grouped, err := s.entityAliasesBatch(ctx, src, []int64{ownerID})
	if err != nil {
		return nil, err
	}
	return grouped[ownerID], nil
}

func (s *PublicService) entityAliasesBatch(ctx context.Context, src aliasSource, ownerIDs []int64) (map[int64][]displayAlias, error) {
	out := make(map[int64][]displayAlias, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return out, nil
	}
	live := ""
	if src.live != "" {
		live = " AND " + src.live
	}
	q := fmt.Sprintf(`
		SELECT a.%s AS owner_id, a.name, a.lang, a.kind, a.provenance,
		       a.is_primary_for_locale AS is_primary,
		       (a.name = o.%s) AS is_display
		FROM %s a
		JOIN %s o ON o.id = a.%s
		WHERE a.%s IN ? AND a.kind <> ?%s
		ORDER BY a.name, a.id`,
		src.ownerCol, src.ownerNameCol, src.table, src.ownerTable, src.ownerCol, src.ownerCol, live)

	var rows []struct {
		OwnerID    int64  `gorm:"column:owner_id"`
		Name       string `gorm:"column:name"`
		Lang       string `gorm:"column:lang"`
		Kind       int16  `gorm:"column:kind"`
		IsPrimary  bool   `gorm:"column:is_primary"`
		IsDisplay  bool   `gorm:"column:is_display"`
		Provenance int16  `gorm:"column:provenance"`
	}
	if err := s.db.WithContext(ctx).Raw(q, ownerIDs, model.AliasKindSearchHint).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.OwnerID] = append(out[r.OwnerID], displayAlias{
			Name: r.Name, Lang: r.Lang, Kind: r.Kind,
			IsPrimary: r.IsPrimary, IsDisplay: r.IsDisplay, Provenance: r.Provenance,
		})
	}
	return out, nil
}

func (s *PublicService) localizedFor(ctx context.Context, src aliasSource, ownerIDs []int64) (map[int64]map[string]dto.PublicLocalizedName, error) {
	grouped, err := s.entityAliasesBatch(ctx, src, ownerIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]map[string]dto.PublicLocalizedName, len(grouped))
	for id, rows := range grouped {
		out[id] = localizedNames(rows)
	}
	return out, nil
}

func (s *PublicService) LocalizedForEntities(ctx context.Context, entityType string, ids []int64) (map[int64]map[string]dto.PublicLocalizedName, error) {
	var src aliasSource
	switch entityType {
	case "character":
		src = characterAliasSource
	case "label":
		src = labelAliasSource
	case "name":
		src = creditNameAliasSource
	default:
		return map[int64]map[string]dto.PublicLocalizedName{}, nil
	}
	return s.localizedFor(ctx, src, ids)
}

type WorkNameBlock struct {
	Localized map[string]dto.PublicLocalizedName
}

func (s *PublicService) WorkNamesByID(ctx context.Context, ids []int64) (map[int64]WorkNameBlock, error) {
	titles, err := s.workTitlesFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]WorkNameBlock, len(titles))
	for id, rows := range titles {
		out[id] = WorkNameBlock{Localized: workLocalized(rows)}
	}
	return out, nil
}

func locOrEmpty(m map[string]dto.PublicLocalizedName) map[string]dto.PublicLocalizedName {
	if m == nil {
		return map[string]dto.PublicLocalizedName{}
	}
	return m
}

func (s *PublicService) workTitlesFor(ctx context.Context, ids []int64) (map[int64][]WorkTitleRow, error) {
	titles := make(map[int64][]WorkTitleRow, len(ids))
	if len(ids) == 0 {
		return titles, nil
	}
	if err := s.read.nativeWorkTitles(ctx, ids, titles, false); err != nil {
		return nil, err
	}
	return titles, nil
}

func (s *PublicService) fillWorkBriefNames(ctx context.Context, briefs ...*dto.PublicWorkBrief) error {
	ids := make([]int64, 0, len(briefs))
	for _, b := range briefs {
		if b != nil {
			ids = append(ids, b.ID)
		}
	}
	titles, err := s.workTitlesFor(ctx, ids)
	if err != nil {
		return err
	}
	// Every brief construction site funnels through here, so the three head
	// columns are filled here too rather than in each of the six callers. The
	// v2 brief is a full basic Work: leaving them unset shipped olang:"" and
	// created_at:""/updated_at:"" under `format: date-time` on relations,
	// appearances and credit-name credits, which a generated client rejects.
	heads, err := s.workBriefHeads(ctx, ids)
	if err != nil {
		return err
	}
	for _, b := range briefs {
		if b == nil {
			continue
		}
		rows := titles[b.ID]
		b.Latin = workLatin(rows, b.DisplayName)
		b.Localized = workLocalized(rows)
		if h, ok := heads[b.ID]; ok {
			b.OLang, b.Created, b.Updated = h.OLang, h.Created, h.Updated
		}
	}
	return nil
}

type workBriefHead struct {
	OLang, Created, Updated string
}

func (s *PublicService) workBriefHeads(ctx context.Context, ids []int64) (map[int64]workBriefHead, error) {
	if len(ids) == 0 {
		return map[int64]workBriefHead{}, nil
	}
	var rows []struct {
		ID int64 `gorm:"column:id"`
		// Without the tag GORM looks for o_lang and every brief scans "".
		OLang     string    `gorm:"column:olang"`
		CreatedAt time.Time `gorm:"column:created_at"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, olang, created_at, updated_at FROM catalog_work WHERE id IN ?`, ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]workBriefHead, len(rows))
	for _, r := range rows {
		out[r.ID] = workBriefHead{
			OLang:   r.OLang,
			Created: r.CreatedAt.UTC().Format(time.RFC3339),
			Updated: r.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	return out, nil
}

// workLocalized is localizedNames for work titles: same first-row-wins scan over
// rows nativeWorkTitles already ordered by (provenance, kind, id), so a source
// title beats a machine one and official beats alias inside a locale.
func workLocalized(rows []WorkTitleRow) map[string]dto.PublicLocalizedName {
	out := make(map[string]dto.PublicLocalizedName, len(rows))
	for _, t := range rows {
		locale, ok := canonicalLocale(t.Lang)
		if !ok {
			continue
		}
		// The kind check precedes the slot check on purpose: the detail face
		// loads titles withHints=true, and claiming a locale for a search_hint
		// row would drop the real title behind it.
		kind, ok := titleKindKey(t.Kind)
		if !ok {
			continue
		}
		if _, taken := out[locale]; taken {
			continue
		}
		out[locale] = dto.PublicLocalizedName{
			Value: t.Title, Kind: kind,
			Machine: t.Provenance == model.WorkTitleProvenanceMachine,
		}
	}
	return out
}

func workLatin(rows []WorkTitleRow, displayName string) string {
	for _, t := range rows {
		if t.Title == displayName {
			return t.Latin
		}
	}
	return ""
}

func (s *PublicService) fillLabelLocalized(ctx context.Context, blocks ...[]dto.PublicWorkLabel) error {
	var ids []int64
	for _, blk := range blocks {
		for i := range blk {
			ids = append(ids, blk[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	loc, err := s.localizedFor(ctx, labelAliasSource, ids)
	if err != nil {
		return err
	}
	for _, blk := range blocks {
		for i := range blk {
			blk[i].Localized = locOrEmpty(loc[blk[i].ID])
		}
	}
	return nil
}

func richAliases(rows []displayAlias) []dto.PublicAlias {
	out := make([]dto.PublicAlias, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.IsDisplay {
			continue
		}
		lang := r.Lang
		if canon, ok := canonicalLocale(r.Lang); ok {
			lang = canon
		}
		key := r.Name + "\x00" + lang
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, dto.PublicAlias{
			Value: r.Name, Lang: lang, Kind: aliasKindKey(r.Kind),
			Machine: r.Provenance == model.AliasProvenanceMachine,
		})
	}
	return out
}

func localizedNames(rows []displayAlias) map[string]dto.PublicLocalizedName {
	out := make(map[string]dto.PublicLocalizedName, len(rows))
	best := make(map[string]displayAlias, len(rows))
	for _, r := range rows {
		locale, ok := canonicalLocale(r.Lang)
		if !ok {
			continue
		}
		cur, seen := best[locale]
		if seen && !aliasBeats(r, cur) {
			continue
		}
		best[locale] = r
		out[locale] = dto.PublicLocalizedName{
			Value: r.Name, Kind: aliasKindKey(r.Kind),
			Machine: r.Provenance == model.AliasProvenanceMachine,
		}
	}
	return out
}

func canonicalLocale(lang string) (string, bool) {
	if lang == "" || len(lang) > 35 {
		return "", false
	}
	parts := strings.Split(lang, "-")
	for i, p := range parts {
		if len(p) == 0 || len(p) > 8 || !isASCIIAlphanumeric(p) {
			return "", false
		}
		switch {
		case i == 0:
			if !isASCIIAlpha(p) {
				return "", false
			}
			parts[i] = strings.ToLower(p)
		case len(p) == 4 && isASCIIAlpha(p):
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		case len(p) == 2 && isASCIIAlpha(p), len(p) == 3 && isASCIIDigits(p):
			parts[i] = strings.ToUpper(p)
		default:
			parts[i] = strings.ToLower(p)
		}
	}
	return strings.Join(parts, "-"), true
}

func isASCIIAlpha(s string) bool {
	for _, c := range []byte(s) {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func isASCIIDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isASCIIAlphanumeric(s string) bool {
	for _, c := range []byte(s) {
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !alpha && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Machine rows fill a locale slot only when it has no source-provenance name
// (2026-08-18 revision of the refs/proj/178 §2 gate, which excluded them from
// localized{} entirely; the fill-in carries machine=true on the wire).
func aliasBeats(candidate, incumbent displayAlias) bool {
	cm := candidate.Provenance == model.AliasProvenanceMachine
	im := incumbent.Provenance == model.AliasProvenanceMachine
	if cm != im {
		return im
	}
	if candidate.IsPrimary != incumbent.IsPrimary {
		return candidate.IsPrimary
	}
	return candidate.Kind < incumbent.Kind
}

func aliasKindKey(kind int16) string {
	switch kind {
	case model.AliasKindTranslation:
		return "translation"
	case model.AliasKindSpellingVariant:
		return "spelling_variant"
	default:
		return fmt.Sprintf("unknown_%d", kind)
	}
}
