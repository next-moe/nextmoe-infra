package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	catsvc "api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const DescribeSystemPrompt = `你是 galgame（视觉小说）领域的资深本地化译者，负责把 VNDB 角色特征（trait）的英文释义翻译成简体中文，供中文用户在角色筛选界面阅读。
要求：
1. 忠实完整：逐段翻译「英文释义」的全部内容，包括第一句之后的百科式说明、补充段落和「Example:」示例行，一句都不能省略；不增删信息，保留原文的分段与列表结构；网址原样保留。
2. 通顺自然：用地道的中文书面语，避免翻译腔。"This character ..." 之类的开头译成「该角色……」「角色……」等自然说法，不必逐字对应。
3. 指代单个角色的 they/them/their 不要译成「他们」「她们」，改用「该角色」「其」或省略主语。
4. 术语一致：本特征及文中提到的其他特征，一律使用「术语表」给出的中文译名；ACG 领域已有通行说法的词（如 tsundere→傲娇）使用通行说法。
5. 性相关内容如实、中性地翻译，不回避、不渲染。
6. 只输出译文正文，不要标题、解释、注释、引号或英文原文。`

const descriptionHashSQL = `encode(sha256(convert_to(description, 'UTF8')), 'hex')`

type descRow struct {
	ID          int64  `gorm:"column:id"`
	VndbTID     string `gorm:"column:vndb_tid"`
	Name        string `gorm:"column:name"`
	NameZh      string `gorm:"column:name_zh"`
	GroupTID    string `gorm:"column:group_tid"`
	Description string `gorm:"column:description"`
}

type traitLex struct {
	ID       int64  `gorm:"column:id"`
	VndbTID  string `gorm:"column:vndb_tid"`
	Name     string `gorm:"column:name"`
	NameZh   string `gorm:"column:name_zh"`
	GroupTID string `gorm:"column:group_tid"`
}

type glossPair struct {
	En string
	Zh string
}

type descItem struct {
	Row      descRow
	Prepared string
	Hash     string
	Glossary []glossPair
	RefLabel map[string]string
}

type descTranslator interface {
	Translate(ctx context.Context, name, prepared string, gloss []glossPair) (string, error)
	Configured() bool
}

func sourceHash(desc string) string {
	sum := sha256.Sum256([]byte(desc))
	return hex.EncodeToString(sum[:])
}

var (
	descURLTag = regexp.MustCompile(`(?is)\[url=([^\]]*)\](.*?)\[/url\]`)
	urlPathTID = regexp.MustCompile(`(?i)^/i(\d+)$`)
	urlHostTID = regexp.MustCompile(`(?i)vndb\.org/i(\d+)$`)
)

func traitTIDFromURL(raw string) string {
	u := strings.TrimSpace(raw)
	if m := urlPathTID.FindStringSubmatch(u); m != nil {
		return "i" + m[1]
	}
	if m := urlHostTID.FindStringSubmatch(u); m != nil {
		return "i" + m[1]
	}
	return ""
}

func rewriteTraitLinks(desc string, names map[string]string) (string, []string) {
	var b strings.Builder
	var linked []string
	seen := map[string]struct{}{}
	last := 0
	for _, m := range descURLTag.FindAllStringSubmatchIndex(desc, -1) {
		b.WriteString(desc[last:m[0]])
		url := desc[m[2]:m[3]]
		text := desc[m[4]:m[5]]
		if tid := traitTIDFromURL(url); tid != "" {
			if _, ok := seen[tid]; !ok {
				seen[tid] = struct{}{}
				linked = append(linked, tid)
			}
			if text == tid {
				if name := names[tid]; name != "" {
					text = name
				}
			}
		}
		b.WriteString("[url=")
		b.WriteString(url)
		b.WriteString("]")
		b.WriteString(text)
		b.WriteString("[/url]")
		last = m[1]
	}
	b.WriteString(desc[last:])
	return b.String(), linked
}

func buildGlossary(self traitLex, group *traitLex, parents, linked []traitLex) []glossPair {
	var out []glossPair
	seen := map[string]struct{}{}
	add := func(name, zh string) {
		if name == "" || zh == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, glossPair{En: name, Zh: zh})
	}
	add(self.Name, self.NameZh)
	if group != nil {
		add(group.Name, group.NameZh)
	}
	for _, p := range parents {
		add(p.Name, p.NameZh)
	}
	for _, l := range linked {
		add(l.Name, l.NameZh)
	}
	return out
}

func describeUserMessage(name, prepared string, gloss []glossPair) string {
	var b strings.Builder
	b.WriteString("特征名：")
	b.WriteString(name)
	b.WriteByte('\n')
	if len(gloss) > 0 {
		b.WriteString("术语表：\n")
		for _, g := range gloss {
			b.WriteString("- ")
			b.WriteString(g.En)
			b.WriteString(" → ")
			b.WriteString(g.Zh)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString("英文释义：\n")
	b.WriteString(prepared)
	return b.String()
}

func parseIDList(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var ids []int64
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid id %q", p)
		}
		ids = append(ids, n)
	}
	return ids, nil
}

func loadDescCandidates(ctx context.Context, db *gorm.DB, limit int, ids []int64) ([]descItem, error) {
	q := `SELECT id, vndb_tid, name, name_zh, group_tid, description
	        FROM catalog_character_trait
	       WHERE btrim(description) <> ''`
	var args []any
	if len(ids) > 0 {
		q += ` AND id IN ?`
		args = append(args, ids)
	} else {
		q += ` AND (description_zh = '' OR description_zh_source_hash <> ` + descriptionHashSQL + `)`
	}
	q += ` ORDER BY id`
	if limit > 0 {
		q += ` LIMIT ` + strconv.Itoa(limit)
	}
	var rows []descRow
	if err := db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return prepareDescItems(ctx, db, rows)
}

func loadAllTraitLex(ctx context.Context, db *gorm.DB) (map[string]traitLex, map[int64]traitLex, error) {
	var rows []traitLex
	if err := db.WithContext(ctx).Raw(
		`SELECT id, vndb_tid, name, name_zh, group_tid FROM catalog_character_trait`).
		Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	byTID := make(map[string]traitLex, len(rows))
	byID := make(map[int64]traitLex, len(rows))
	for _, r := range rows {
		byTID[r.VndbTID] = r
		byID[r.ID] = r
	}
	return byTID, byID, nil
}

func loadParentLex(ctx context.Context, db *gorm.DB, ids []int64, byID map[int64]traitLex) (map[int64][]traitLex, error) {
	out := map[int64][]traitLex{}
	if len(ids) == 0 {
		return out, nil
	}
	var edges []struct {
		TraitID  int64 `gorm:"column:trait_id"`
		ParentID int64 `gorm:"column:parent_id"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT trait_id, parent_id FROM catalog_character_trait_parent
		  WHERE trait_id IN ? ORDER BY trait_id, parent_id`, ids).
		Scan(&edges).Error; err != nil {
		return nil, err
	}
	for _, e := range edges {
		if p, ok := byID[e.ParentID]; ok {
			out[e.TraitID] = append(out[e.TraitID], p)
		}
	}
	return out, nil
}

func prepareDescItems(ctx context.Context, db *gorm.DB, rows []descRow) ([]descItem, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	byTID, byID, err := loadAllTraitLex(ctx, db)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	parents, err := loadParentLex(ctx, db, ids, byID)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(byTID))
	for tid, lex := range byTID {
		names[tid] = lex.Name
	}
	out := make([]descItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, prepareDescItem(r, byTID, names, parents[r.ID]))
	}
	return out, nil
}

func prepareDescItem(row descRow, byTID map[string]traitLex, names map[string]string, parents []traitLex) descItem {
	rewritten, linkedTIDs := rewriteTraitLinks(row.Description, names)
	var linked []traitLex
	for _, tid := range linkedTIDs {
		if lex, ok := byTID[tid]; ok {
			linked = append(linked, lex)
		}
	}
	var group *traitLex
	if row.GroupTID != "" {
		if g, ok := byTID[row.GroupTID]; ok {
			cp := g
			group = &cp
		}
	}
	return descItem{
		Row:      row,
		Prepared: catsvc.PlainTraitDescription(rewritten),
		Hash:     sourceHash(row.Description),
		Glossary: buildGlossary(traitLex{Name: row.Name, NameZh: row.NameZh}, group, parents, linked),
		RefLabel: bareRefLabels(row, byTID),
	}
}

var traitIDToken = regexp.MustCompile(`i\d+`)

func isRefBoundary(b byte) bool {
	return !(b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '/')
}

// bareTraitRefs yields the byte spans of iNNN tokens that stand alone in
// running text, not inside a URL or a longer word.
func bareTraitRefs(s string) [][]int {
	var out [][]int
	for _, m := range traitIDToken.FindAllStringIndex(s, -1) {
		if m[0] > 0 && !isRefBoundary(s[m[0]-1]) {
			continue
		}
		if m[1] < len(s) && !isRefBoundary(s[m[1]]) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func bareRefLabels(row descRow, byTID map[string]traitLex) map[string]string {
	labels := map[string]string{}
	for _, m := range bareTraitRefs(row.Description) {
		tid := row.Description[m[0]:m[1]]
		t, ok := byTID[tid]
		if !ok {
			continue
		}
		name := t.NameZh
		if name == "" {
			name = t.Name
		}
		label := "「" + name + "」"
		if t.Name == row.Name || (t.NameZh != "" && t.NameZh == row.NameZh) {
			if g, ok := byTID[t.GroupTID]; ok {
				gname := g.NameZh
				if gname == "" {
					gname = g.Name
				}
				label += "（" + gname + "）"
			}
		}
		labels[tid] = label
	}
	return labels
}

func replaceBareTraitRefs(zh string, labels map[string]string) string {
	if len(labels) == 0 {
		return zh
	}
	var b strings.Builder
	last := 0
	for _, m := range bareTraitRefs(zh) {
		label, ok := labels[zh[m[0]:m[1]]]
		if !ok {
			continue
		}
		b.WriteString(strings.TrimRight(zh[last:m[0]], " \t"))
		b.WriteString(label)
		last = m[1]
		for last < len(zh) && (zh[last] == ' ' || zh[last] == '\t') {
			last++
		}
	}
	b.WriteString(zh[last:])
	return b.String()
}

func runMTDesc(ctx context.Context, db *gorm.DB, tr descTranslator, out string, limit int, delay time.Duration, ids []int64) error {
	cands, err := loadDescCandidates(ctx, db, limit, ids)
	if err != nil {
		return err
	}
	fmt.Printf("\n=== ingest-trait-zh MT-DESC (candidates: %d) ===\n", len(cands))
	rows := make([]descCSVRow, 0, len(cands))
	var errs int
	for i, c := range cands {
		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}
		zh, err := tr.Translate(ctx, c.Row.Name, c.Prepared, c.Glossary)
		if err != nil {
			errs++
			slog.Warn("describe failed", "trait", c.Row.Name, "id", c.Row.ID, "error", err)
			zh = ""
		}
		zh = replaceBareTraitRefs(zh, c.RefLabel)
		rows = append(rows, descCSVRow{
			TraitID:       c.Row.ID,
			VndbTID:       c.Row.VndbTID,
			Name:          c.Row.Name,
			SourceHash:    c.Hash,
			SourceText:    c.Prepared,
			DescriptionZh: zh,
		})
		if (i+1)%50 == 0 {
			slog.Info("mt-desc progress", "done", i+1, "of", len(cands), "errors", errs)
		}
	}
	if err := writeDescCSV(out, rows); err != nil {
		return err
	}
	fmt.Printf("proposed=%d errors=%d → %s\nreview it, then: --apply-desc-csv %s\n", len(rows)-errs, errs, out, out)
	if errs > 0 {
		os.Exit(1)
	}
	return nil
}
