package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"api/internal/platform/catalog/model"
	"api/internal/platform/provenance"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MergePreview is the computed plan of one merge, read before anything is
// written: what each side holds, where each row and anchor ends up, and what
// stops the merge or deserves a reviewer's attention.
type MergePreview struct {
	EntityType int16
	Source     PreviewSide
	Target     PreviewSide
	Blockers   []PreviewIssue
	Warnings   []PreviewIssue
	Facts      []string
	Fields     []PreviewField
	Anchors    []PreviewAnchor
	Releases   []PreviewRelease
	Titles     []PreviewTitle
	Intros     []PreviewIntro
	Counts     []PreviewCount
	Claim      PreviewClaim
	OpenID     int64
}

type PreviewSide struct {
	ID       int64
	Name     string
	Live     bool
	MediumID int16
	Site     string
}

type PreviewIssue struct {
	Code    string
	Message string
}

type PreviewField struct {
	Key            string
	SourceValue    any
	TargetValue    any
	Default        string
	HumanProtected bool
}

type PreviewAnchor struct {
	Side       string
	SourceKey  string
	ExternalID string
	KindBefore int16
	KindAfter  int16
	Fate       string
	Reason     string
}

type PreviewRelease struct {
	Side        string
	ID          int64
	Title       string
	Kind        int16
	KindKey     string
	Platform    string
	Lang        string
	Y, M, D     *int16
	VNDBRid     string
	DuplicateOf int64
	DuplicateBy string
}

type PreviewTitle struct {
	Side  string
	Title string
	Lang  string
	Kind  int16
	Fate  string
}

type PreviewIntro struct {
	Lang       string
	SourceHas  bool
	TargetHas  bool
	SourceText string
	TargetText string
	Fate       string
}

type PreviewCount struct {
	Key       string
	Source    int64
	Target    int64
	Duplicate int64
}

type PreviewClaim struct {
	SourceSite string
	TargetSite string
	Fate       string
}

const (
	FateMoves           = "moves"
	FateStays           = "stays"
	FateDemotedRelated  = "demoted_related"
	FateDemotedProbable = "demoted_probable"
	FateDuplicate       = "duplicate"
	FateBecomesAlias    = "becomes_alias"
	FateDropped         = "dropped"
	FateKeptOut         = "kept_out"
	FateReplacesMachine = "replaces_machine"
	FateDropsClaim      = "dropped"
	FateNone            = "none"
)

// PreviewMerge computes the plan for merging src into dst without writing.
// It resolves neither id through redirects: the editor names live entities,
// and a retired id is itself a blocker worth showing.
func (s *MergeService) PreviewMerge(ctx context.Context, entityType int16, src, dst int64) (*MergePreview, error) {
	db := s.db.WithContext(ctx)
	if _, ok := entityTableName(entityType); !ok {
		return nil, fmt.Errorf("catalog merge: unsupported entity type %d", entityType)
	}
	p := &MergePreview{EntityType: entityType}
	var err error
	if p.Source, err = previewSide(db, entityType, src); err != nil {
		return nil, err
	}
	if p.Target, err = previewSide(db, entityType, dst); err != nil {
		return nil, err
	}
	p.Blockers = previewBlockers(db, p)
	if len(p.Blockers) > 0 && (!p.Source.Live || !p.Target.Live) {
		return p, nil
	}
	if err := db.Raw(`SELECT COALESCE(max(id), 0) FROM catalog_merge_proposal
		WHERE entity_type = ? AND status = ? AND source_entity_id = ? AND target_entity_id = ?`,
		entityType, model.ProposalStatusOpen, src, dst).Scan(&p.OpenID).Error; err != nil {
		return nil, err
	}
	if p.Fields, err = previewFields(db, entityType, src, dst); err != nil {
		return nil, err
	}
	if p.Anchors, err = previewAnchors(db, entityType, src, dst); err != nil {
		return nil, err
	}
	if p.Counts, err = previewCounts(db, entityType, src, dst); err != nil {
		return nil, err
	}
	if entityType == model.EntityTypeWork {
		if p.Releases, err = previewReleases(db, src, dst); err != nil {
			return nil, err
		}
		if p.Titles, err = previewTitles(db, src, dst); err != nil {
			return nil, err
		}
		if p.Intros, err = previewIntros(db, src, dst); err != nil {
			return nil, err
		}
		p.Claim = previewClaim(p.Source.Site, p.Target.Site)
	}
	p.Warnings = append(p.Warnings, previewAnchorWarnings(p.Anchors)...)
	if p.Claim.Fate == FateDropsClaim {
		p.Warnings = append(p.Warnings, PreviewIssue{"claim_dropped",
			fmt.Sprintf("两边都被站点认领：保留方的认领（%s）不变，被并入方在 %s 的页面以后跟随重定向。", p.Target.Site, p.Source.Site)})
	}
	p.Facts = previewFacts(p)
	return p, nil
}

func previewSide(db *gorm.DB, et int16, id int64) (PreviewSide, error) {
	table, _ := entityTableName(et)
	nameCol := "display_name"
	if et == model.EntityTypeCreditName {
		nameCol = "name"
	}
	deleted := "deleted_at IS NULL"
	if et == model.EntityTypeCreditName {
		deleted = "true"
	}
	side := PreviewSide{ID: id}
	var row struct {
		Name     string
		Live     bool
		MediumID int16
		Site     *string
	}
	extra := "0::smallint AS medium_id, NULL::text AS site"
	if et == model.EntityTypeWork {
		extra = "medium_id, site"
	}
	err := db.Raw(fmt.Sprintf(`SELECT %s AS name, %s AS live, %s FROM %s WHERE id = ?`, nameCol, deleted, extra, table), id).Scan(&row).Error
	if err != nil {
		return side, err
	}
	side.Name, side.Live, side.MediumID = row.Name, row.Live, row.MediumID
	if row.Site != nil {
		side.Site = *row.Site
	}
	return side, nil
}

func previewBlockers(db *gorm.DB, p *MergePreview) []PreviewIssue {
	var out []PreviewIssue
	if p.Source.ID == p.Target.ID {
		return []PreviewIssue{{"same_entity", "不能把一个条目并入它自己。"}}
	}
	for _, side := range []PreviewSide{p.Source, p.Target} {
		if !side.Live {
			out = append(out, PreviewIssue{"not_live", fmt.Sprintf("#%d 已被合并或删除，不能再参与合并。", side.ID)})
		}
	}
	if len(out) > 0 {
		return out
	}
	if p.EntityType == model.EntityTypeWork && p.Source.MediumID != p.Target.MediumID {
		out = append(out, PreviewIssue{"cross_medium", "两部作品属于不同媒介（例如游戏与小说），不能合并；它们之间应该用关联作品连接。"})
	}
	if err := assertSurvivorNotHidden(db, p.EntityType, p.Source.ID, p.Target.ID); errors.Is(err, ErrHiddenSurvivor) {
		out = append(out, PreviewIssue{"hidden_survivor", "保留方的认领已被隐藏，而被并入方没有：合并会把公开的条目并进隐藏的条目。请交换方向，或先处理认领。"})
	}
	return out
}

func previewFields(db *gorm.DB, et int16, src, dst int64) ([]PreviewField, error) {
	table, _ := entityTableName(et)
	keys := mergeChoiceFields[et]
	var rows []struct {
		ID   int64
		Doc  datatypes.JSON
		Prov datatypes.JSON
	}
	if err := db.Raw(fmt.Sprintf(`SELECT id, to_jsonb(t) AS doc, COALESCE(t.field_provenance, '{}'::jsonb) AS prov
		FROM %s t WHERE id IN (?, ?)`, table), src, dst).Scan(&rows).Error; err != nil {
		return nil, err
	}
	docs := map[int64]map[string]any{}
	provs := map[int64]datatypes.JSON{}
	for _, r := range rows {
		var m map[string]any
		if err := jsonUnmarshal(r.Doc, &m); err != nil {
			return nil, err
		}
		docs[r.ID], provs[r.ID] = m, r.Prov
	}
	out := make([]PreviewField, 0, len(keys))
	for _, key := range keys {
		sv, tv := fieldValue(docs[src], key), fieldValue(docs[dst], key)
		f := PreviewField{Key: key, SourceValue: sv, TargetValue: tv, Default: ResolutionTarget}
		if isEmptyValue(tv) && !isEmptyValue(sv) {
			f.Default = ResolutionSource
		}
		f.HumanProtected = !isEmptyValue(tv) &&
			provenance.IsHuman(firstProvSource(provs[dst], key)) &&
			!provenance.IsHuman(firstProvSource(provs[src], key))
		out = append(out, f)
	}
	return out, nil
}

func fieldValue(doc map[string]any, key string) any {
	if key == "birth" {
		y, m, d := doc["birth_y"], doc["birth_m"], doc["birth_d"]
		if y == nil && m == nil && d == nil {
			return nil
		}
		return map[string]any{"y": y, "m": m, "d": d}
	}
	return doc[key]
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	}
	return false
}

func previewClaim(srcSite, dstSite string) PreviewClaim {
	c := PreviewClaim{SourceSite: srcSite, TargetSite: dstSite, Fate: FateNone}
	switch {
	case srcSite == "":
	case dstSite == "":
		c.Fate = FateMoves
	default:
		c.Fate = FateDropsClaim
	}
	return c
}

func previewAnchorWarnings(anchors []PreviewAnchor) []PreviewIssue {
	var out []PreviewIssue
	for _, a := range anchors {
		if a.Side != ResolutionSource || (a.Fate != FateDemotedRelated && a.Fate != FateDemotedProbable) {
			continue
		}
		msg := fmt.Sprintf("两边在 %s 对应不同的条目，被并入方的 %s 会降为%s，以后不再从它导入数据。",
			a.SourceKey, a.ExternalID, map[string]string{FateDemotedRelated: "相关链接", FateDemotedProbable: "待确认对应"}[a.Fate])
		if a.SourceKey == "vndb" {
			msg = fmt.Sprintf("两边对应不同的 VNDB 条目（被并入方 %s）：VNDB 认为它们是不同的作品，请确认真的是重复。", a.ExternalID)
		}
		out = append(out, PreviewIssue{"conflicting_anchor", msg})
	}
	return out
}

func previewFacts(p *MergePreview) []string {
	var facts []string
	for _, r := range p.Releases {
		if r.Side != ResolutionSource || r.DuplicateOf == 0 {
			continue
		}
		var other PreviewRelease
		for _, t := range p.Releases {
			if t.ID == r.DuplicateOf {
				other = t
			}
		}
		if r.DuplicateBy == "vndb" {
			facts = append(facts, fmt.Sprintf("两边都有 VNDB 发行 %s。", r.VNDBRid))
		} else if other.VNDBRid != "" {
			facts = append(facts, fmt.Sprintf("「%s」的发行与「%s」的 VNDB 发行 %s 平台、发售日一致（%s，%s）。",
				p.Source.Name, p.Target.Name, other.VNDBRid, r.Platform, dateText(r.Y, r.M, r.D)))
		} else {
			facts = append(facts, fmt.Sprintf("两边有平台、发售日相同的发行（%s，%s）。", r.Platform, dateText(r.Y, r.M, r.D)))
		}
	}
	var srcVNDB, dstVNDB string
	for _, a := range p.Anchors {
		if a.SourceKey == "vndb" && a.KindBefore == model.LinkKindExact {
			if a.Side == ResolutionSource {
				srcVNDB = a.ExternalID
			} else {
				dstVNDB = a.ExternalID
			}
		}
	}
	if srcVNDB == "" && dstVNDB != "" {
		facts = append(facts, fmt.Sprintf("VNDB 只收录了「%s」（%s），「%s」在 VNDB 没有独立条目。", p.Target.Name, dstVNDB, p.Source.Name))
	}
	for _, t := range p.Titles {
		if t.Side == ResolutionSource && t.Fate == FateDuplicate {
			facts = append(facts, fmt.Sprintf("两边有相同的标题「%s」。", t.Title))
			break
		}
	}
	return slices.Compact(facts)
}

func dateText(y, m, d *int16) string {
	if y == nil {
		return "日期不明"
	}
	return ReleasedText(y, m, d)
}

// CalendarDate is the v2 release_date convention for a partial date: month
// dates sit on the 1st, year dates on January 1, with the precision beside.
func CalendarDate(y, m, d *int16) (*string, *string) {
	if y == nil {
		return nil, nil
	}
	precision, mm, dd := "year", int16(1), int16(1)
	if m != nil {
		precision, mm = "month", *m
		if d != nil {
			precision, dd = "day", *d
		}
	}
	date := fmt.Sprintf("%04d-%02d-%02d", *y, mm, dd)
	return &date, &precision
}

// ReleasedText renders a partial date as YYYY, YYYY-MM or YYYY-MM-DD.
func ReleasedText(y, m, d *int16) string {
	switch {
	case y == nil:
		return ""
	case m == nil:
		return fmt.Sprintf("%04d", *y)
	case d == nil:
		return fmt.Sprintf("%04d-%02d", *y, *m)
	}
	return fmt.Sprintf("%04d-%02d-%02d", *y, *m, *d)
}
