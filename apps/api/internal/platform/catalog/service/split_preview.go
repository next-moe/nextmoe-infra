package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type SplitPreviewRelease struct {
	ID         int64
	Title      string
	Kind       int16
	KindKey    string
	Lang       string
	Platform   string
	ReleasedY  *int16
	ReleasedM  *int16
	ReleasedD  *int16
	VNDBID     string
	Sources    []string
	Labels     int64
	Credits    int64
	Selected   bool
	Tombstoned bool
}

type SplitPreviewRef struct {
	Source     string
	ExternalID string
	LinkKind   int16
	MatchedBy  string
	HeldBy     *int64
	Selected   bool
}

type SplitPreviewCount struct {
	Key   string
	Count int64
}

type SplitPreview struct {
	WorkID        int64
	WorkName      string
	MediumID      int16
	OLang         string
	VNDBAnchor    string
	Releases      []SplitPreviewRelease
	Candidates    []SplitPreviewRef
	SuggestedName string
	SuggestedLang string
	Titles        []model.SplitTitle
	Moves         []SplitPreviewCount
	Stays         []SplitPreviewCount
	Blockers      []PreviewIssue
	Warnings      []PreviewIssue
	Facts         []PreviewIssue
	PendingID     *int64
}

func (s *SplitService) Preview(ctx context.Context, workID int64, selected []int64, promote []model.SplitRef, ownRequestID int64) (*SplitPreview, error) {
	db := s.db.WithContext(ctx)
	var w model.CatalogWork
	if err := db.First(&w, workID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: work %d", ErrNotFound, workID)
		}
		return nil, err
	}
	p := &SplitPreview{WorkID: w.ID, WorkName: w.DisplayName, MediumID: w.MediumID, OLang: w.OLang, SuggestedLang: w.OLang}
	if err := db.Raw(`SELECT r.external_id FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND s.key = 'vndb' AND r.link_kind = ? LIMIT 1`,
		model.EntityTypeWork, w.ID, model.LinkKindExact).Scan(&p.VNDBAnchor).Error; err != nil {
		return nil, err
	}

	var rows []struct {
		ID        int64
		Title     string
		Kind      int16
		Lang      string
		Platform  string
		ReleasedY *int16
		ReleasedM *int16
		ReleasedD *int16
		VNDBID    string
		Sources   string
		Labels    int64
		Credits   int64
	}
	if err := db.Raw(`
		SELECT r.id, r.title, r.kind, r.lang, r.platform, r.released_y, r.released_m, r.released_d,
		       COALESCE(r.extra->>'vndb_id', '') AS vndb_id,
		       COALESCE((SELECT string_agg(DISTINCT s.key, ',' ORDER BY s.key) FROM catalog_external_ref x
		                  JOIN catalog_source s ON s.id = x.source_id
		                 WHERE x.entity_type = ? AND x.entity_id = r.id), '') AS sources,
		       (SELECT count(*) FROM catalog_release_label l WHERE l.release_id = r.id) AS labels,
		       (SELECT count(*) FROM catalog_credit c WHERE c.work_id = r.work_id AND c.release_id = r.id) AS credits
		  FROM catalog_release r
		 WHERE r.work_id = ? AND r.deleted_at IS NULL
		 ORDER BY r.released_y NULLS LAST, r.released_m NULLS LAST, r.released_d NULLS LAST, r.id`,
		model.EntityTypeRelease, w.ID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	live := make([]int64, 0, len(rows))
	for _, r := range rows {
		live = append(live, r.ID)
		rel := SplitPreviewRelease{
			ID: r.ID, Title: r.Title, Kind: r.Kind, KindKey: releaseKindKey(r.Kind), Lang: r.Lang, Platform: r.Platform,
			ReleasedY: r.ReleasedY, ReleasedM: r.ReleasedM, ReleasedD: r.ReleasedD,
			VNDBID: r.VNDBID, Labels: r.Labels, Credits: r.Credits,
			Selected: slices.Contains(selected, r.ID),
		}
		if r.Sources != "" {
			rel.Sources = strings.Split(r.Sources, ",")
		}
		rel.Tombstoned = rel.Selected && rel.VNDBID != ""
		p.Releases = append(p.Releases, rel)
	}

	var refs []struct {
		Source     string
		ExternalID string
		LinkKind   int16
		MatchedBy  string
		HeldBy     *int64
	}
	if err := db.Raw(`
		SELECT s.key AS source, r.external_id, r.link_kind, r.matched_by,
		       (SELECT h.entity_id FROM catalog_external_ref h WHERE h.entity_type = r.entity_type
		           AND h.source_id = r.source_id AND h.external_id = r.external_id AND h.link_kind = ?) AS held_by
		  FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		 WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind <> ? AND r.source_id IN ?
		 ORDER BY s.key, r.external_id`,
		model.LinkKindExact, model.EntityTypeWork, w.ID, model.LinkKindExact, model.EditionSplittingSourceIDs).Scan(&refs).Error; err != nil {
		return nil, err
	}
	for _, r := range refs {
		ref := model.SplitRef{Source: r.Source, ExternalID: r.ExternalID}
		p.Candidates = append(p.Candidates, SplitPreviewRef{
			Source: r.Source, ExternalID: r.ExternalID, LinkKind: r.LinkKind, MatchedBy: r.MatchedBy,
			HeldBy: r.HeldBy, Selected: slices.Contains(promote, ref),
		})
	}

	var pending int64
	if err := db.Raw(`SELECT id FROM catalog_split_request WHERE entity_type = ? AND entity_id = ? AND status IN (?, ?) AND id <> ? ORDER BY id DESC LIMIT 1`,
		model.EntityTypeWork, w.ID, model.ProposalStatusOpen, model.ProposalStatusApproved, ownRequestID).Scan(&pending).Error; err != nil {
		return nil, err
	}
	if pending != 0 {
		p.PendingID = &pending
	}

	p.Titles, p.SuggestedName = suggestSplitTitles(p.Releases, w.OLang)
	if err := s.splitCounts(db, p, w.ID); err != nil {
		return nil, err
	}
	splitIssues(p, live, selected, promote)
	return p, nil
}

func (s *SplitService) splitCounts(db *gorm.DB, p *SplitPreview, workID int64) error {
	var stays struct {
		Credits, Characters, Tags, Titles, Releases int64
	}
	if err := db.Raw(`SELECT
		(SELECT count(*) FROM catalog_credit WHERE work_id = ?) AS credits,
		(SELECT count(*) FROM catalog_work_character WHERE work_id = ?) AS characters,
		(SELECT count(*) FROM catalog_work_tag WHERE work_id = ?) AS tags,
		(SELECT count(*) FROM catalog_work_title WHERE work_id = ?) AS titles`,
		workID, workID, workID, workID).Scan(&stays).Error; err != nil {
		return err
	}
	var moving struct {
		Releases, Tombstones, Labels, ReleaseRefs, Credits int64
	}
	for _, r := range p.Releases {
		if !r.Selected {
			continue
		}
		moving.Releases++
		moving.Labels += r.Labels
		moving.Credits += r.Credits
		moving.ReleaseRefs += int64(len(r.Sources))
		if r.Tombstoned {
			moving.Tombstones++
		}
	}
	p.Moves = []SplitPreviewCount{
		{"releases", moving.Releases}, {"release_labels", moving.Labels},
		{"release_refs", moving.ReleaseRefs}, {"credits", moving.Credits}, {"tombstones", moving.Tombstones},
	}
	p.Stays = []SplitPreviewCount{
		{"releases", int64(len(p.Releases)) - moving.Releases}, {"credits", stays.Credits - moving.Credits},
		{"characters", stays.Characters}, {"tags", stays.Tags}, {"titles", stays.Titles},
	}
	return nil
}

func splitIssues(p *SplitPreview, live, selected []int64, promote []model.SplitRef) {
	if p.PendingID != nil {
		p.Blockers = append(p.Blockers, PreviewIssue{"pending_request",
			fmt.Sprintf("这部作品已有一条进行中的拆分请求 #%d，处理完之前不能再发起。", *p.PendingID)})
	}
	if len(live) < 2 {
		p.Blockers = append(p.Blockers, PreviewIssue{"too_few_releases", "作品至少要有两个发行才能拆分：一个留下，一个拆出去。"})
	}
	n := 0
	for _, id := range selected {
		if slices.Contains(live, id) {
			n++
		}
	}
	if n > 0 && n >= len(live) {
		p.Blockers = append(p.Blockers, PreviewIssue{"all_releases", "至少要留一个发行在原作品里；全部移走等于换了编号，请改用编辑。"})
	}
	for _, c := range p.Candidates {
		if c.Selected && c.HeldBy != nil {
			p.Blockers = append(p.Blockers, PreviewIssue{"anchor_held",
				fmt.Sprintf("%s %s 已经是作品 #%d 的正式对应，不能再设给新作品。", sourceLabel(c.Source), c.ExternalID, *c.HeldBy)})
		}
	}
	for _, ref := range promote {
		found := false
		for _, c := range p.Candidates {
			if c.Source == ref.Source && c.ExternalID == ref.ExternalID {
				found = true
			}
		}
		if !found {
			p.Blockers = append(p.Blockers, PreviewIssue{"anchor_unknown",
				fmt.Sprintf("%s %s 没有以相关或待确认链接挂在这部作品上。", sourceLabel(ref.Source), ref.ExternalID)})
		}
	}

	if p.VNDBAnchor != "" {
		withRID := 0
		for _, r := range p.Releases {
			if r.Selected && r.VNDBID != "" {
				withRID++
			}
		}
		if withRID > 0 {
			p.Facts = append(p.Facts, PreviewIssue{"vndb_same_vn",
				fmt.Sprintf("VNDB 把所选的 %d 个发行都列在 %s 下，认为它们和原作品是同一部作品。拆分是例外，需要审核者认可理由。", withRID, p.VNDBAnchor)})
			p.Warnings = append(p.Warnings, PreviewIssue{"vndb_updates_stop",
				fmt.Sprintf("拆出去的 %d 个 VNDB 发行以后不再从 VNDB 更新；原作品里会留下「已移走」记录，防止导入把它们建回来。", withRID)})
		}
	}
	separate := 0
	bySource := map[string][]string{}
	var order []string
	for _, c := range p.Candidates {
		if c.HeldBy != nil {
			continue
		}
		separate++
		if _, seen := bySource[c.Source]; !seen {
			order = append(order, c.Source)
		}
		bySource[c.Source] = append(bySource[c.Source], c.ExternalID)
	}
	for _, source := range order {
		ids := bySource[source]
		what := "一个条目"
		if len(ids) > 1 {
			what = fmt.Sprintf("%d 个条目", len(ids))
		}
		p.Facts = append(p.Facts, PreviewIssue{"listed_separately",
			fmt.Sprintf("%s 单独收录了%s（%s），目前以相关或待确认链接挂在原作品上；可以把对应这个版本的那一个设为新作品的正式对应。",
				sourceLabel(source), what, strings.Join(ids, "、"))})
	}
	if separate == 0 {
		p.Facts = append(p.Facts, PreviewIssue{"no_separate_entry", "Bangumi 和批评空间都没有以相关链接挂着单独的条目；新作品不会有资料源对应，标题、标签、制作人员要靠人工补。"})
	}
	if n > 0 && len(promote) == 0 && separate > 0 {
		p.Warnings = append(p.Warnings, PreviewIssue{"no_anchor_chosen", "没有选资料源对应：新作品只有移过去的发行和填写的标题，其他资料不会自动补齐。"})
	}
}

func suggestSplitTitles(releases []SplitPreviewRelease, olang string) ([]model.SplitTitle, string) {
	var out []model.SplitTitle
	at := map[string]int{}
	for _, r := range releases {
		if !r.Selected {
			continue
		}
		base := trimEditionSuffix(r.Title)
		if base == "" {
			continue
		}
		if i, seen := at[base]; seen {
			if r.Lang == olang {
				out[i].Lang = olang
			}
			continue
		}
		at[base] = len(out)
		out = append(out, model.SplitTitle{Lang: r.Lang, Title: base, Kind: model.WorkTitleKindAlias})
	}
	best := -1
	for i, t := range out {
		if best < 0 || better(t, out[best], olang) {
			best = i
		}
	}
	if best < 0 {
		return out, ""
	}
	out[best].Kind = model.WorkTitleKindOfficial
	out[0], out[best] = out[best], out[0]
	return out, out[0].Title
}

func better(a, b model.SplitTitle, olang string) bool {
	if (a.Lang == olang) != (b.Lang == olang) {
		return a.Lang == olang
	}
	return len([]rune(a.Title)) < len([]rune(b.Title))
}

var editionSuffixes = []string{
	" - Download Edition", " - Regular Edition", " Steam Edition", " Improvement Patch", " Physical edition",
	" ダウンロード版", " パッケージ版", " 通常版", " 初回限定版", " 体験版", " 体验版",
}

func trimEditionSuffix(title string) string {
	t := strings.TrimSpace(title)
	for changed := true; changed; {
		changed = false
		for _, suf := range editionSuffixes {
			if strings.HasSuffix(t, suf) && len(t) > len(suf) {
				t = strings.TrimSpace(strings.TrimSuffix(t, suf))
				changed = true
			}
		}
	}
	return t
}

func sourceLabel(source string) string {
	switch source {
	case "bangumi":
		return "Bangumi"
	case "erogamescape":
		return "批评空间"
	case "vndb":
		return "VNDB"
	}
	return source
}

func linkKindLabel(kind int16) string {
	if kind == model.LinkKindProbable {
		return "待确认链接"
	}
	return "相关链接"
}
