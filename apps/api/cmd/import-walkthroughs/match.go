package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

const (
	matchDLsite     = "dlsite"
	matchFanza      = "fanza"
	matchTitle      = "title"
	matchTitleBrand = "title+brand"
	matchAmbiguous  = "ambiguous"
	matchNone       = "none"

	ruleSameName = "rule:walkthrough-same-name"
	ruleNamePart = "rule:walkthrough-name-part"
)

const (
	sourceDLsite       int16 = 4
	sourceOfficialSite int16 = 9
	sourceDMM          int16 = 15
)

const titlePunct = `[\s・:：\-－~〜～！!？?、。,.★☆♪「」『』【】()（）]`

var nameNoise = regexp.MustCompile(`[\s・=＝]`)

func nameKey(s string) string {
	return nameNoise.ReplaceAllString(strings.ToLower(norm.NFKC.String(s)), "")
}

type matchSource struct {
	EntryID  int64
	Title    string
	Brand    string
	DlsiteID string
	FanzaID  string
	Extra    string
}

type characterPin struct {
	CharacterID int64  `json:"character_id"`
	Rule        string `json:"rule"`
}

type matchResult struct {
	EntryID    int64
	WorkID     *int64
	Method     string
	Candidates []int64
	Characters map[string]characterPin
}

func runMatch(ctx context.Context, src, cat *gorm.DB, entries []sourceEntry) error {
	charts, err := gatedCharts(ctx, src, entryIDs(entries))
	if err != nil {
		return err
	}
	var games []matchSource
	if err := src.WithContext(ctx).Raw(`
SELECT e.id AS entry_id, g.title, g.brand, g.dlsite_id, g.fanza_id, g.extra::text AS extra
FROM entry e JOIN game g ON g.id = e.game_id
WHERE e.id IN ?`, keysOf(charts)).Scan(&games).Error; err != nil {
		return err
	}
	fmt.Printf("\n=== import-walkthroughs match charts=%d ===\n", len(games))

	works, err := matchWorks(ctx, cat, games)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	pinned, created := 0, 0
	for _, g := range games {
		r := works[g.EntryID]
		r.EntryID = g.EntryID
		r.Characters = map[string]characterPin{}
		if r.WorkID != nil {
			roster, err := loadRoster(ctx, cat, *r.WorkID)
			if err != nil {
				return err
			}
			r.Characters = pinCharacters(charts[g.EntryID], roster)
		}
		pinned += len(r.Characters)
		if c := charts[g.EntryID]; c != nil && r.WorkID != nil {
			created += len(c.Characters) - len(r.Characters)
		}
		counts[r.Method]++
		if err := storeMatch(ctx, src, r); err != nil {
			return err
		}
	}
	fmt.Printf("dlsite=%d fanza=%d title=%d title+brand=%d ambiguous=%d none=%d characters_pinned=%d characters_to_create=%d\n",
		counts[matchDLsite], counts[matchFanza], counts[matchTitle], counts[matchTitleBrand], counts[matchAmbiguous], counts[matchNone], pinned, created)
	return nil
}

func entryIDs(entries []sourceEntry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func keysOf[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func gatedCharts(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]*walkthrough.Chart, error) {
	var rows []struct {
		EntryID int64
		Chart   string
	}
	if err := src.WithContext(ctx).Table("wt_stage.extraction").
		Select("entry_id, chart::text AS chart").
		Where("prompt_version = ? AND verdict = ? AND entry_id IN ?", extractPromptVersion, verdictOK, ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]*walkthrough.Chart, len(rows))
	for _, r := range rows {
		var c walkthrough.Chart
		if err := json.Unmarshal([]byte(r.Chart), &c); err != nil {
			return nil, fmt.Errorf("entry %d chart: %w", r.EntryID, err)
		}
		out[r.EntryID] = &c
	}
	return out, nil
}

func liveWorks(alias string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM catalog_work w WHERE w.id = %s AND w.status = %d AND w.deleted_at IS NULL)`, alias, model.WorkStatusLive)
}

func matchWorks(ctx context.Context, cat *gorm.DB, games []matchSource) (map[int64]matchResult, error) {
	out := make(map[int64]matchResult, len(games))
	byCode := func(sql string, codes []string) (map[string][]int64, error) {
		m := map[string][]int64{}
		if len(codes) == 0 {
			return m, nil
		}
		var rows []struct {
			Code   string
			WorkID int64
		}
		if err := cat.WithContext(ctx).Raw(sql, codes).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !slices.Contains(m[r.Code], r.WorkID) {
				m[r.Code] = append(m[r.Code], r.WorkID)
			}
		}
		return m, nil
	}
	var rj, fanza, titles []string
	for _, g := range games {
		if g.DlsiteID != "" {
			rj = append(rj, strings.ToUpper(g.DlsiteID))
		}
		if g.FanzaID != "" {
			fanza = append(fanza, strings.ToLower(g.FanzaID))
		}
		titles = append(titles, g.Title)
	}
	dl, err := byCode(fmt.Sprintf(`
SELECT upper(r.external_id) AS code, COALESCE(rl.work_id, r.entity_id) AS work_id
FROM catalog_external_ref r
LEFT JOIN catalog_release rl ON r.entity_type = %d AND rl.id = r.entity_id AND rl.deleted_at IS NULL
WHERE r.source_id = %d AND r.link_kind = %d AND r.entity_type IN (%d, %d) AND upper(r.external_id) IN ?
  AND (r.entity_type = %d OR rl.id IS NOT NULL) AND %s`,
		model.EntityTypeRelease, sourceDLsite, model.LinkKindExact, model.EntityTypeRelease, model.EntityTypeWork,
		model.EntityTypeWork, liveWorks("COALESCE(rl.work_id, r.entity_id)")), rj)
	if err != nil {
		return nil, err
	}
	fz, err := byCode(fmt.Sprintf(`
SELECT lower(r.external_id) AS code, r.entity_id AS work_id
FROM catalog_external_ref r
WHERE r.source_id = %d AND r.link_kind = %d AND r.entity_type = %d AND lower(r.external_id) IN ? AND %s`,
		sourceDMM, model.LinkKindExact, model.EntityTypeWork, liveWorks("r.entity_id")), fanza)
	if err != nil {
		return nil, err
	}
	tt, err := byTitle(ctx, cat, titles)
	if err != nil {
		return nil, err
	}
	var ambiguous []int64
	for _, g := range games {
		switch {
		case len(dl[strings.ToUpper(g.DlsiteID)]) == 1 && g.DlsiteID != "":
			out[g.EntryID] = matchResult{WorkID: &dl[strings.ToUpper(g.DlsiteID)][0], Method: matchDLsite}
		case len(fz[strings.ToLower(g.FanzaID)]) == 1 && g.FanzaID != "":
			out[g.EntryID] = matchResult{WorkID: &fz[strings.ToLower(g.FanzaID)][0], Method: matchFanza}
		case len(tt[g.Title]) == 1:
			out[g.EntryID] = matchResult{WorkID: &tt[g.Title][0], Method: matchTitle}
		case len(tt[g.Title]) > 1:
			out[g.EntryID] = matchResult{Method: matchAmbiguous, Candidates: tt[g.Title]}
			ambiguous = append(ambiguous, tt[g.Title]...)
		default:
			out[g.EntryID] = matchResult{Method: matchNone, Candidates: []int64{}}
		}
	}
	brands, err := loadBrands(ctx, cat, ambiguous)
	if err != nil {
		return nil, err
	}
	for _, g := range games {
		r := out[g.EntryID]
		if r.Method != matchAmbiguous {
			continue
		}
		var hit []int64
		for _, w := range r.Candidates {
			if brands[w].matches(g) {
				hit = append(hit, w)
			}
		}
		if len(hit) == 1 {
			r.WorkID, r.Method = &hit[0], matchTitleBrand
			out[g.EntryID] = r
		}
	}
	return out, nil
}

func byTitle(ctx context.Context, cat *gorm.DB, titles []string) (map[string][]int64, error) {
	var rows []struct {
		Code   string
		WorkID int64
	}
	if err := cat.WithContext(ctx).Raw(`
WITH q AS (SELECT t AS code, regexp_replace(lower(normalize(t, NFKC)), @punct, '', 'g') AS k
           FROM unnest(string_to_array(@titles, E'\x1f')) t)
SELECT DISTINCT q.code, wt.work_id
FROM q JOIN catalog_work_title wt ON regexp_replace(wt.title_norm, @punct, '', 'g') = q.k
WHERE q.k <> '' AND `+liveWorks("wt.work_id"),
		map[string]any{"punct": titlePunct, "titles": strings.Join(titles, "\x1f")}).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string][]int64{}
	for _, r := range rows {
		out[r.Code] = append(out[r.Code], r.WorkID)
	}
	return out, nil
}

type workBrands struct {
	names []string
	hosts []string
}

var brandNoise = regexp.MustCompile(`[^\p{L}\p{N}]`)

func brandKey(s string) string {
	return brandNoise.ReplaceAllString(strings.ToLower(norm.NFKC.String(s)), "")
}

func siteHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}

func (b workBrands) matches(g matchSource) bool {
	var extra struct {
		MakerURL string `json:"makerUrl"`
	}
	_ = json.Unmarshal([]byte(g.Extra), &extra)
	if h := siteHost(extra.MakerURL); h != "" && slices.Contains(b.hosts, h) {
		return true
	}
	k := brandKey(g.Brand)
	if utf8.RuneCountInString(k) < 2 {
		return false
	}
	for _, n := range b.names {
		if n == k || strings.Contains(n, k) || strings.Contains(k, n) && utf8.RuneCountInString(n) >= 3 {
			return true
		}
	}
	return false
}

func loadBrands(ctx context.Context, cat *gorm.DB, works []int64) (map[int64]workBrands, error) {
	out := map[int64]workBrands{}
	if len(works) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64
		Name   string
		Host   string
	}
	if err := cat.WithContext(ctx).Raw(fmt.Sprintf(`
SELECT wl.work_id, n.name, '' AS host
FROM catalog_work_label wl
JOIN LATERAL (
  SELECT l.display_name AS name FROM catalog_label l WHERE l.id = wl.label_id
  UNION ALL SELECT l.latin FROM catalog_label l WHERE l.id = wl.label_id AND l.latin IS NOT NULL
  UNION ALL SELECT a.name FROM catalog_label_alias a WHERE a.label_id = wl.label_id
) n ON true
WHERE wl.work_id IN ?
UNION ALL
SELECT wl.work_id, '', r.external_id
FROM catalog_work_label wl
JOIN catalog_external_ref r ON r.entity_type = %d AND r.entity_id = wl.label_id AND r.source_id = %d
WHERE wl.work_id IN ?`, model.EntityTypeLabel, sourceOfficialSite), works, works).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		b := out[r.WorkID]
		if r.Name != "" {
			b.names = append(b.names, brandKey(r.Name))
		}
		if h := siteHost(r.Host); h != "" {
			b.hosts = append(b.hosts, h)
		}
		out[r.WorkID] = b
	}
	return out, nil
}

type rosterEntry struct {
	CharacterID int64
	Names       []string
	Placeholder bool
	HasImage    bool
}

func loadRoster(ctx context.Context, cat *gorm.DB, workID int64) ([]rosterEntry, error) {
	var rows []struct {
		CharacterID int64
		DisplayName string
		Placeholder bool
		HasImage    bool
		Name        string
	}
	if err := cat.WithContext(ctx).Raw(`
SELECT c.id AS character_id, c.display_name, c.image_hash IS NOT NULL AS has_image,
  NOT EXISTS (SELECT 1 FROM catalog_work_character o WHERE o.character_id = c.id AND o.matched_by <> ?) AS placeholder,
  n.name
FROM catalog_character c
JOIN LATERAL (
  SELECT c.display_name AS name
  UNION ALL SELECT a.name FROM catalog_character_alias a WHERE a.character_id = c.id
) n ON true
WHERE c.deleted_at IS NULL AND c.id IN (
  SELECT wc.character_id FROM catalog_work_character wc WHERE wc.work_id = ? AND `+editspec.NotSuppressedRosterSQL("wc")+`)
ORDER BY c.id`, matchedByWalkthrough, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var out []rosterEntry
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].CharacterID != r.CharacterID {
			out = append(out, rosterEntry{CharacterID: r.CharacterID, Placeholder: r.Placeholder, HasImage: r.HasImage})
			if k := nameKey(r.DisplayName); k != "" {
				out[len(out)-1].Names = []string{k}
			}
		}
		last := &out[len(out)-1]
		if k := nameKey(r.Name); k != "" && !slices.Contains(last.Names, k) {
			last.Names = append(last.Names, k)
		}
	}
	return out, nil
}

func pinCharacters(c *walkthrough.Chart, roster []rosterEntry) map[string]characterPin {
	out := map[string]characterPin{}
	if c == nil {
		return out
	}
	taken := map[int64]bool{}
	sameName := func(k, n string) bool { return n == k }
	tiers := []struct {
		placeholder bool
		rule        string
		match       func(k, n string) bool
	}{
		{false, ruleSameName, sameName},
		{false, ruleNamePart, namePart},
		{true, ruleSameName, sameName},
		{true, ruleNamePart, namePart},
	}
	for _, tier := range tiers {
		for _, ch := range c.Characters {
			if _, done := out[ch.Key]; done {
				continue
			}
			var ks []string
			for _, n := range append([]string{ch.Name}, ch.Aliases...) {
				if k := nameKey(n); k != "" {
					ks = append(ks, k)
				}
			}
			var hit []rosterEntry
			for _, r := range roster {
				if r.Placeholder == tier.placeholder && !taken[r.CharacterID] &&
					slices.ContainsFunc(ks, func(k string) bool {
						return slices.ContainsFunc(r.Names, func(n string) bool { return tier.match(k, n) })
					}) {
					hit = append(hit, r)
				}
			}
			if id, ok := onePerson(hit); ok {
				out[ch.Key] = characterPin{CharacterID: id, Rule: tier.rule}
				taken[id] = true
			}
		}
	}
	return out
}

// A one-rune given name is matched only as the end of a full name of three or
// more runes: the two-rune floor left 鏡 unmatched beside 敷島 鏡 and the first
// import created ~820 name-only duplicates of real characters, while a looser
// rule would read 空 as the end of 美空.
func namePart(k, n string) bool {
	if n == k {
		return false
	}
	if utf8.RuneCountInString(k) == 1 {
		return utf8.RuneCountInString(n) >= 3 && strings.HasSuffix(n, k)
	}
	return strings.HasPrefix(n, k) || strings.HasSuffix(n, k)
}

// onePerson treats hits that share a display name as one person entered more
// than once (如月 澪 is on one roster three times) and pins the copy with an
// image, so a duplicated roster no longer reads as ambiguous.
func onePerson(hits []rosterEntry) (int64, bool) {
	if len(hits) == 0 || len(hits[0].Names) == 0 {
		return 0, false
	}
	best := hits[0]
	for _, h := range hits[1:] {
		if len(h.Names) == 0 || h.Names[0] != hits[0].Names[0] {
			return 0, false
		}
		if h.HasImage && !best.HasImage {
			best = h
		}
	}
	return best.CharacterID, true
}

func storeMatch(ctx context.Context, src *gorm.DB, r matchResult) error {
	cands, err := json.Marshal(orEmptyIDs(r.Candidates))
	if err != nil {
		return err
	}
	chars, err := json.Marshal(r.Characters)
	if err != nil {
		return err
	}
	return src.WithContext(ctx).Exec(`
INSERT INTO wt_stage.match (entry_id, work_id, method, candidates, characters, created_at)
VALUES (?, ?, ?, ?::jsonb, ?::jsonb, now())
ON CONFLICT (entry_id) DO UPDATE SET work_id = EXCLUDED.work_id, method = EXCLUDED.method,
  candidates = EXCLUDED.candidates, characters = EXCLUDED.characters, created_at = now()`,
		r.EntryID, r.WorkID, r.Method, string(cands), string(chars)).Error
}

func orEmptyIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
