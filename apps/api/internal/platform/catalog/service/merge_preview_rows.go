package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func jsonUnmarshal(raw []byte, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func sideOf(entityID, src int64) string {
	if entityID == src {
		return ResolutionSource
	}
	return ResolutionTarget
}

// previewAnchors mirrors mergeExternalRefs: the survivor's refs never change;
// a source exact that contradicts a survivor exact from the same source is
// demoted, an edition-splitting source keeping only its shortest id; a ref
// both sides hold is dropped from the source as a duplicate.
func previewAnchors(db *gorm.DB, et int16, src, dst int64) ([]PreviewAnchor, error) {
	var rows []struct {
		EntityID   int64
		SourceID   int16
		SourceKey  string
		ExternalID string
		LinkKind   int16
	}
	if err := db.Raw(`SELECT r.entity_id, r.source_id, s.key AS source_key, r.external_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id IN (?, ?)
		ORDER BY r.entity_id = ? DESC, s.key, r.link_kind, length(r.external_id), r.external_id`,
		et, src, dst, src).Scan(&rows).Error; err != nil {
		return nil, err
	}
	dstExact := map[int16][]string{}
	dstAll := map[string]bool{}
	srcExact := map[int16][]string{}
	for _, r := range rows {
		key := fmt.Sprintf("%d:%s", r.SourceID, r.ExternalID)
		if r.EntityID == dst {
			dstAll[key] = true
			if r.LinkKind == model.LinkKindExact {
				dstExact[r.SourceID] = append(dstExact[r.SourceID], r.ExternalID)
			}
		} else if r.LinkKind == model.LinkKindExact {
			srcExact[r.SourceID] = append(srcExact[r.SourceID], r.ExternalID)
		}
	}
	edition := func(id int16) bool {
		return et == model.EntityTypeWork && slices.Contains(model.EditionSplittingSourceIDs, id)
	}
	out := make([]PreviewAnchor, 0, len(rows))
	for _, r := range rows {
		a := PreviewAnchor{Side: sideOf(r.EntityID, src), SourceKey: r.SourceKey, ExternalID: r.ExternalID,
			KindBefore: r.LinkKind, KindAfter: r.LinkKind, Fate: FateMoves}
		switch {
		case r.EntityID == dst:
			a.Fate = FateStays
		case dstAll[fmt.Sprintf("%d:%s", r.SourceID, r.ExternalID)]:
			a.Fate, a.Reason = FateDuplicate, "保留方已有同一个对应"
		case r.LinkKind != model.LinkKindExact || slices.Contains(curatedSourceKeys, r.SourceKey):
		case len(dstExact[r.SourceID]) > 0:
			a.KindAfter, a.Fate = model.LinkKindProbable, FateDemotedProbable
			if edition(r.SourceID) {
				a.KindAfter, a.Fate = model.LinkKindRelated, FateDemotedRelated
			}
			a.Reason = fmt.Sprintf("保留方在 %s 已对应 %s", sourceDisplayName(r.SourceKey), strings.Join(dstExact[r.SourceID], "、"))
		case edition(r.SourceID) && len(srcExact[r.SourceID]) > 1 && srcExact[r.SourceID][0] != r.ExternalID:
			a.KindAfter, a.Fate = model.LinkKindRelated, FateDemotedRelated
			a.Reason = fmt.Sprintf("同一来源只保留一个正式对应（%s）", srcExact[r.SourceID][0])
		}
		out = append(out, a)
	}
	return out, nil
}

func previewReleases(db *gorm.DB, src, dst int64) ([]PreviewRelease, error) {
	var rows []struct {
		ID       int64
		WorkID   int64
		Kind     int16
		Title    *string
		Lang     *string
		Platform *string
		Y, M, D  *int16
		Rid      *string
	}
	if err := db.Raw(`SELECT r.id, r.work_id, r.kind, r.title, r.lang, r.platform,
		       r.released_y AS y, r.released_m AS m, r.released_d AS d,
		       COALESCE(NULLIF(r.extra->>'vndb_id', ''),
		                (SELECT e.external_id FROM catalog_external_ref e
		                  WHERE e.entity_type = ? AND e.entity_id = r.id AND e.source_id = ? AND e.link_kind = ?
		                  ORDER BY e.external_id LIMIT 1)) AS rid
		FROM catalog_release r WHERE r.work_id IN (?, ?) AND r.deleted_at IS NULL
		ORDER BY r.work_id = ? DESC, r.released_y NULLS LAST, r.released_m NULLS LAST, r.released_d NULLS LAST, r.id`,
		model.EntityTypeRelease, model.SourceVNDB, model.LinkKindExact, src, dst, src).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PreviewRelease, 0, len(rows))
	for _, r := range rows {
		out = append(out, PreviewRelease{Side: sideOf(r.WorkID, src), ID: r.ID, Kind: r.Kind, KindKey: releaseKindKey(r.Kind),
			Title: deref(r.Title), Lang: deref(r.Lang), Platform: deref(r.Platform),
			Y: r.Y, M: r.M, D: r.D, VNDBRid: deref(r.Rid)})
	}
	for i := range out {
		if out[i].Side != ResolutionSource {
			continue
		}
		for _, t := range out {
			if t.Side != ResolutionTarget {
				continue
			}
			if by := releaseDuplicate(out[i], t); by != "" {
				out[i].DuplicateOf, out[i].DuplicateBy = t.ID, by
				break
			}
		}
	}
	return out, nil
}

// releaseDuplicate names how a source release repeats a survivor release:
// the same VNDB release, or the same platform on the same full date with
// compatible kinds (a default-kind release matches a digital or physical one;
// trials and patches only match their own kind).
func releaseDuplicate(a, b PreviewRelease) string {
	if a.VNDBRid != "" && a.VNDBRid == b.VNDBRid {
		return "vndb"
	}
	if a.Platform == "" || a.Platform != b.Platform || !sameFullDate(a, b) {
		return ""
	}
	compatible := a.Kind == b.Kind ||
		(a.Kind == model.ReleaseKindDefault && slices.Contains([]int16{model.ReleaseKindDigital, model.ReleaseKindPhysical}, b.Kind)) ||
		(b.Kind == model.ReleaseKindDefault && slices.Contains([]int16{model.ReleaseKindDigital, model.ReleaseKindPhysical}, a.Kind))
	if !compatible {
		return ""
	}
	return "platform_date"
}

func sameFullDate(a, b PreviewRelease) bool {
	eq := func(x, y *int16) bool { return x != nil && y != nil && *x == *y }
	return eq(a.Y, b.Y) && eq(a.M, b.M) && eq(a.D, b.D)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// previewTitles mirrors workTitleStmts.
func previewTitles(db *gorm.DB, src, dst int64) ([]PreviewTitle, error) {
	var rows []struct {
		WorkID     int64
		Lang       string
		Title      string
		Kind       int16
		Provenance int16
	}
	if err := db.Raw(`SELECT t.work_id, t.lang, t.title, t.kind, t.provenance FROM catalog_work_title t
		WHERE t.work_id IN (?, ?) AND t.kind <> ? AND `+editspec.NotSuppressedWorkTitleSQL("t")+`
		ORDER BY t.work_id = ? DESC, t.lang, t.kind, t.id`, src, dst, model.WorkTitleKindSearchHint, dst).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var target struct {
		DisplayName string
		OLang       string
	}
	if err := db.Raw(`SELECT display_name, olang FROM catalog_work WHERE id = ?`, dst).Scan(&target).Error; err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	titles := map[string]bool{target.DisplayName: target.DisplayName != ""}
	exact := map[string]bool{}
	if target.DisplayName != "" && target.OLang != "" {
		taken[strings.ToLower(target.OLang)] = true
	}
	for _, r := range rows {
		if r.WorkID != dst {
			continue
		}
		if r.Lang != "" {
			taken[strings.ToLower(r.Lang)] = true
		}
		titles[r.Title] = true
		exact[fmt.Sprintf("%s|%s|%d", r.Lang, r.Title, r.Kind)] = true
	}
	out := make([]PreviewTitle, 0, len(rows))
	for _, r := range rows {
		t := PreviewTitle{Side: sideOf(r.WorkID, src), Title: r.Title, Lang: r.Lang, Kind: r.Kind, Fate: FateStays}
		if r.WorkID == src {
			switch {
			case taken[strings.ToLower(r.Lang)] && r.Provenance == model.WorkTitleProvenanceMachine:
				t.Fate = FateDropped
			case taken[strings.ToLower(r.Lang)] && titles[r.Title]:
				t.Fate = FateDuplicate
			case taken[strings.ToLower(r.Lang)]:
				t.Fate = FateBecomesAlias
			case exact[fmt.Sprintf("%s|%s|%d", r.Lang, r.Title, r.Kind)]:
				t.Fate = FateDuplicate
			default:
				t.Fate = FateMoves
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// previewIntros mirrors introStmts per language.
func previewIntros(db *gorm.DB, src, dst int64) ([]PreviewIntro, error) {
	var rows []struct {
		WorkID     int64
		Lang       string
		Provenance int16
		Excerpt    string
	}
	if err := db.Raw(`SELECT work_id, lang, provenance, left(intro, 200) AS excerpt FROM catalog_work_intro
		WHERE work_id IN (?, ?) ORDER BY lang, provenance, source_id`, src, dst).Scan(&rows).Error; err != nil {
		return nil, err
	}
	type side struct {
		has, source bool
		text        string
	}
	byLang := map[string]*[2]side{}
	var langs []string
	for _, r := range rows {
		entry, ok := byLang[r.Lang]
		if !ok {
			entry = &[2]side{}
			byLang[r.Lang] = entry
			langs = append(langs, r.Lang)
		}
		i := 1
		if r.WorkID == src {
			i = 0
		}
		if !entry[i].has || (r.Provenance == model.IntroProvenanceSource && !entry[i].source) {
			entry[i].text = r.Excerpt
		}
		entry[i].has = true
		entry[i].source = entry[i].source || r.Provenance == model.IntroProvenanceSource
	}
	out := make([]PreviewIntro, 0, len(langs))
	for _, lang := range langs {
		s, t := byLang[lang][0], byLang[lang][1]
		in := PreviewIntro{Lang: lang, SourceHas: s.has, TargetHas: t.has, SourceText: s.text, TargetText: t.text, Fate: FateStays}
		switch {
		case !s.has:
		case !t.has:
			in.Fate = FateMoves
		case t.source || !s.source:
			in.Fate = FateKeptOut
		default:
			in.Fate = FateReplacesMachine
		}
		out = append(out, in)
	}
	return out, nil
}

type countSpec struct {
	key, sourceSQL, dupSQL string
}

var workCountSpecs = []countSpec{
	{"tags", `SELECT count(DISTINCT name) FROM catalog_work_tag WHERE work_id = @src`,
		`SELECT count(DISTINCT s.name) FROM catalog_work_tag s JOIN catalog_work_tag d ON d.work_id = @dst AND d.name = s.name WHERE s.work_id = @src`},
	{"credits", `SELECT count(*) FROM catalog_credit WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_credit s JOIN catalog_credit d ON d.work_id = @dst AND d.credit_name_id = s.credit_name_id
		   AND d.role_id = s.role_id AND COALESCE(d.character_id, 0) = COALESCE(s.character_id, 0) WHERE s.work_id = @src`},
	{"characters", `SELECT count(*) FROM catalog_work_character WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_work_character s JOIN catalog_work_character d ON d.work_id = @dst AND d.character_id = s.character_id WHERE s.work_id = @src`},
	{"companies", `SELECT count(*) FROM catalog_work_label WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_work_label s JOIN catalog_work_label d ON d.work_id = @dst AND d.label_id = s.label_id AND d.kind = s.kind WHERE s.work_id = @src`},
	{"relations", `SELECT count(*) FROM catalog_work_relation WHERE (a_work_id = @src AND b_work_id <> @dst) OR (b_work_id = @src AND a_work_id <> @dst)`, ``},
	{"series", `SELECT count(*) FROM catalog_series_member WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_series_member s JOIN catalog_series_member d ON d.work_id = @dst AND d.series_id = s.series_id WHERE s.work_id = @src`},
	{"covers", `SELECT count(*) FROM catalog_work_cover WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_work_cover s JOIN catalog_work_cover d ON d.work_id = @dst AND d.image_hash = s.image_hash WHERE s.work_id = @src`},
	{"screenshots", `SELECT count(*) FROM catalog_work_screenshot WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_work_screenshot s JOIN catalog_work_screenshot d ON d.work_id = @dst AND d.image_hash = s.image_hash WHERE s.work_id = @src`},
	{"platforms", `SELECT count(DISTINCT platform) FROM catalog_work_platform WHERE work_id = @src`,
		`SELECT count(DISTINCT s.platform) FROM catalog_work_platform s JOIN catalog_work_platform d ON d.work_id = @dst AND d.platform = s.platform WHERE s.work_id = @src`},
	{"user_ratings", `SELECT count(*) FROM catalog_user_work_rating WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_user_work_rating s JOIN catalog_user_work_rating d ON d.work_id = @dst AND d.actor_uid = s.actor_uid WHERE s.work_id = @src`},
	{"user_folders", `SELECT count(*) FROM catalog_user_folder_item WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_user_folder_item s JOIN catalog_user_folder_item d ON d.work_id = @dst AND d.folder_id = s.folder_id WHERE s.work_id = @src`},
	{"user_states", `SELECT count(*) FROM catalog_user_work_state WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_user_work_state s JOIN catalog_user_work_state d ON d.work_id = @dst AND d.actor_uid = s.actor_uid WHERE s.work_id = @src`},
	{"user_playtime", `SELECT count(*) FROM catalog_user_playtime WHERE work_id = @src`,
		`SELECT count(*) FROM catalog_user_playtime s JOIN catalog_user_playtime d ON d.work_id = @dst AND d.actor_uid = s.actor_uid AND d.client_id = s.client_id WHERE s.work_id = @src`},
}

func previewCounts(db *gorm.DB, et int16, src, dst int64) ([]PreviewCount, error) {
	named := map[string]any{"src": src, "dst": dst}
	if et == model.EntityTypeWork {
		out := make([]PreviewCount, 0, len(workCountSpecs))
		for _, spec := range workCountSpecs {
			c := PreviewCount{Key: spec.key}
			if err := db.Raw(spec.sourceSQL, named).Scan(&c.Source).Error; err != nil {
				return nil, fmt.Errorf("count %s: %w", spec.key, err)
			}
			if err := db.Raw(strings.ReplaceAll(spec.sourceSQL, "@src", "@dst"), named).Scan(&c.Target).Error; err != nil {
				return nil, fmt.Errorf("count %s: %w", spec.key, err)
			}
			if spec.dupSQL != "" {
				if err := db.Raw(spec.dupSQL, named).Scan(&c.Duplicate).Error; err != nil {
					return nil, fmt.Errorf("count %s: %w", spec.key, err)
				}
			}
			out = append(out, c)
		}
		return out, nil
	}
	var out []PreviewCount
	for _, r := range movementRefs(et) {
		if r.table == "catalog_external_ref" || r.table == "catalog_entity_usage" || r.table == "edit_suppressed_row" {
			continue
		}
		c := PreviewCount{Key: MovementKey(r.table)}
		q := fmt.Sprintf(`SELECT count(*) FROM %[1]s t WHERE t.%[2]q = ? AND (%[3]s) = ?`, r.table, r.column, r.typeSQL)
		if err := db.Raw(q, src, et).Scan(&c.Source).Error; err != nil {
			return nil, err
		}
		if err := db.Raw(q, dst, et).Scan(&c.Target).Error; err != nil {
			return nil, err
		}
		if c.Source == 0 && c.Target == 0 {
			continue
		}
		if i := slices.IndexFunc(out, func(x PreviewCount) bool { return x.Key == c.Key }); i >= 0 {
			out[i].Source += c.Source
			out[i].Target += c.Target
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
