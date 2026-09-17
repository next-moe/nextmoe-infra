package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"api/internal/jobs/personadj"

	"gorm.io/gorm"
)

const (
	catLowConf    = "lowconf"
	catUnsure     = "unsure"
	catInstance   = "instance"
	catSameSource = "samesource"
	catAuto       = "auto"
)

const (
	panelVotes         = 3
	panelAcceptConf    = 0.85
	instanceAcceptConf = 0.95
)

type panelPair struct {
	pairMeta
	Cat    string  `json:"cat"`
	R1Conf float64 `json:"r1_conf"`
}

func classifyReview(pairs []pairMeta, verdicts []personadj.Verdict) []panelPair {
	byKey := make(map[string]personadj.Verdict, len(verdicts))
	for _, v := range verdicts {
		byKey[v.Key] = v
	}

	var tail []panelPair
	autoPairs := make([]panelPair, 0, len(pairs))
	for _, p := range pairs {
		v, ok := byKey[fmt.Sprintf("xsrc:%d:%d", p.A, p.B)]
		if !ok {
			continue
		}
		pp := panelPair{pairMeta: p, R1Conf: v.Confidence}
		switch v.Verdict {
		case personadj.VerdictMerge:
			switch {
			case p.Instance:
				pp.Cat = catInstance
				tail = append(tail, pp)
			case v.Confidence >= autoConfidence:
				autoPairs = append(autoPairs, pp)
			default:
				pp.Cat = catLowConf
				tail = append(tail, pp)
			}
		case personadj.VerdictDistinct:
		default:
			pp.Cat = catUnsure
			tail = append(tail, pp)
		}
	}

	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			r := find(p)
			parent[x] = r
			return r
		}
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
		return parent[x]
	}
	for _, p := range autoPairs {
		ra, rb := find(p.A), find(p.B)
		if ra != rb {
			parent[rb] = ra
		}
	}
	sourcesBySide := map[int64][]string{}
	for _, p := range pairs {
		sourcesBySide[p.A] = p.ASources
		sourcesBySide[p.B] = p.BSources
	}
	deferred := map[int64]bool{}
	seenByRoot := map[int64]map[string]bool{}
	for id := range parent {
		r := find(id)
		if seenByRoot[r] == nil {
			seenByRoot[r] = map[string]bool{}
		}
		for _, s := range sourcesBySide[id] {
			if seenByRoot[r][s] {
				deferred[r] = true
			}
			seenByRoot[r][s] = true
		}
	}
	for _, p := range autoPairs {
		if deferred[find(p.A)] {
			p.Cat = catSameSource
			tail = append(tail, p)
		}
	}
	sort.Slice(tail, func(i, j int) bool {
		if tail[i].A != tail[j].A {
			return tail[i].A < tail[j].A
		}
		return tail[i].B < tail[j].B
	})
	return tail
}

var panelLenses = [panelVotes]string{
	"【复核视角:反证】请专门搜寻它们是不同角色的证据(亲属同姓/同门同名/变体与本体/团体与个体/化名属他人);找不到可靠反证且写法或简介支持同一,才判 merge。",
	"【复核视角:写法】请检查两个名字的差异是否完全可由写法规则解释(空格/中点/全半角/异体字/音译/通称对全名/姓与名/译名对原名);完全可解释支持 merge,需要脑补的差异倾向 distinct 或 unsure。",
	"【复核视角:设定】请以简介与设定为主判据:两侧是否为同一人物设定(身份/关系/剧情位置);矛盾判 distinct,一致支持 merge,证据不足判 unsure。",
}

func panelKey(a, b int64, vote int) string {
	return fmt.Sprintf("xsrcb:%d:%d:%d", a, b, vote)
}

func runPanelPackets(db *gorm.DB, pairsPath, verdictsPath, pairs2Path, packets2Path string, out io.Writer) error {
	pairs, err := loadPairs(pairsPath)
	if err != nil {
		return err
	}
	verdicts, err := personadj.LoadVerdicts(verdictsPath)
	if err != nil {
		return err
	}
	tail := classifyReview(pairs, verdicts)

	ids := map[int64]bool{}
	for _, p := range tail {
		ids[p.A], ids[p.B] = true, true
	}
	redirect, err := loadRedirects(db, ids)
	if err != nil {
		return err
	}
	resolve := func(id int64) int64 {
		if to, ok := redirect[id]; ok {
			return to
		}
		return id
	}

	liveIDs := map[int64]bool{}
	for _, p := range tail {
		liveIDs[resolve(p.A)] = true
		liveIDs[resolve(p.B)] = true
	}
	info, err := loadCharInfo(db, liveIDs)
	if err != nil {
		return err
	}

	stats := map[string]int{}
	var kept []panelPair
	for _, p := range tail {
		p.A, p.B = resolve(p.A), resolve(p.B)
		if p.A == p.B {
			stats["collapsed"]++
			continue
		}
		if p.A > p.B {
			p.A, p.B = p.B, p.A
		}
		a, b := info[p.A], info[p.B]
		if a == nil || b == nil {
			stats["gone"]++
			continue
		}
		p.AName, p.BName = a.Name, b.Name
		p.ASources, p.BSources = a.Sources, b.Sources
		p.Instance = a.Instance || b.Instance
		p.ARich = richness{Img: a.HasImage, NAliases: len(a.Aliases)}
		p.BRich = richness{Img: b.HasImage, NAliases: len(b.Aliases)}
		if p.Cat != catSameSource && overlaps(a.Sources, b.Sources) {
			stats["source_conflict"]++
			continue
		}
		stats[p.Cat]++
		kept = append(kept, p)
	}

	titles, err := loadWorkTitles(db, pairMetas(kept))
	if err != nil {
		return err
	}

	pf, err := os.Create(pairs2Path)
	if err != nil {
		return err
	}
	defer pf.Close()
	kf, err := os.Create(packets2Path)
	if err != nil {
		return err
	}
	defer kf.Close()
	pe, ke := json.NewEncoder(pf), json.NewEncoder(kf)

	npackets := 0
	for _, p := range kept {
		if err := pe.Encode(p); err != nil {
			return err
		}
		if p.Cat == catSameSource {
			continue
		}
		body := evidenceText(p.pairMeta, info, titles)
		meta, _ := json.Marshal(map[string]any{"tier": p.Tier, "cat": p.Cat})
		for vote := 1; vote <= panelVotes; vote++ {
			if err := ke.Encode(personadj.Packet{
				Bucket: personadj.BucketCharacterPairStrict,
				Key:    panelKey(p.A, p.B, vote),
				User:   body + "\n" + panelLenses[vote-1] + "\n",
				Meta:   meta,
			}); err != nil {
				return err
			}
			npackets++
		}
	}
	fmt.Fprintf(out, "tail=%d kept=%d lowconf=%d unsure=%d instance=%d samesource=%d "+
		"collapsed=%d gone=%d source_conflict=%d packets=%d\n",
		len(tail), len(kept), stats[catLowConf], stats[catUnsure], stats[catInstance],
		stats[catSameSource], stats["collapsed"], stats["gone"], stats["source_conflict"], npackets)
	return nil
}

func loadPanelPairs(path string) ([]panelPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []panelPair
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p panelPair
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		out = append(out, p)
	}
	return out, nil
}

type mergeEdge struct {
	p    panelPair
	conf float64
}

// panelEdges folds the three votes of every judged panel pair. Same-source
// pairs carry no votes; they come back as edges at their round-one confidence
// only when withSameSource is set.
func panelEdges(ppairs []panelPair, verdicts []personadj.Verdict, withSameSource bool,
	stats map[string]int) (edges []mergeEdge, residual []string) {
	byKey := make(map[string]personadj.Verdict, len(verdicts))
	for _, v := range verdicts {
		byKey[v.Key] = v
	}
	for _, p := range ppairs {
		if p.Cat == catSameSource {
			if withSameSource {
				edges = append(edges, mergeEdge{p, p.R1Conf})
			}
			continue
		}
		merges, distincts := 0, 0
		minConf, judged := 1.0, 0
		var reasons []string
		for vote := 1; vote <= panelVotes; vote++ {
			v, ok := byKey[panelKey(p.A, p.B, vote)]
			if !ok {
				continue
			}
			judged++
			switch v.Verdict {
			case personadj.VerdictMerge:
				merges++
				if v.Confidence < minConf {
					minConf = v.Confidence
				}
			case personadj.VerdictDistinct:
				distincts++
			}
			reasons = append(reasons, fmt.Sprintf("v%d=%s/%.2f %s", vote, v.Verdict, v.Confidence, v.Reason))
		}
		if judged < panelVotes {
			stats["unjudged"]++
			residual = append(residual, panelLine(p, "票不全", reasons))
			continue
		}
		bar := panelAcceptConf
		if p.Cat == catInstance {
			bar = instanceAcceptConf
		}
		switch {
		case merges == panelVotes && minConf >= bar:
			stats["accept_"+p.Cat]++
			edges = append(edges, mergeEdge{p, minConf})
		case distincts >= 2:
			stats["closed_distinct"]++
		case p.Cat == catInstance:
			stats["closed_instance"]++
		default:
			stats["residual"]++
			residual = append(residual, panelLine(p, "分歧", reasons))
		}
	}
	return edges, residual
}

type mergeGroup struct {
	survivor int64
	sources  []int64
}

// unionEdges applies edges from the most confident down and refuses any edge
// that would put two rows of one source into a group.
func unionEdges(edges []mergeEdge, sourcesBySide map[int64][]string, richBySide map[int64]richness,
	stats map[string]int) (groups []mergeGroup, residual []string, applied int) {
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].conf > edges[j].conf })
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			r := find(p)
			parent[x] = r
			return r
		}
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
		return parent[x]
	}
	rootSources := map[int64]map[string]bool{}
	sourceSet := func(id int64) map[string]bool {
		r := find(id)
		if rootSources[r] == nil {
			rootSources[r] = map[string]bool{}
			for _, s := range sourcesBySide[id] {
				rootSources[r][s] = true
			}
		}
		return rootSources[r]
	}
	for _, e := range edges {
		ra, rb := find(e.p.A), find(e.p.B)
		if ra == rb {
			continue
		}
		sa, sb := sourceSet(e.p.A), sourceSet(e.p.B)
		conflict := false
		for s := range sb {
			if sa[s] {
				conflict = true
			}
		}
		if conflict {
			stats["edge_conflict"]++
			residual = append(residual, panelLine(e.p, "并组后同源冲突", nil))
			continue
		}
		for s := range sb {
			sa[s] = true
		}
		parent[rb] = ra
		rootSources[ra] = sa
		delete(rootSources, rb)
		applied++
	}

	members := map[int64][]int64{}
	for id := range parent {
		r := find(id)
		members[r] = append(members[r], id)
	}
	roots := make([]int64, 0, len(members))
	for r := range members {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	for _, r := range roots {
		if len(members[r]) < 2 {
			continue
		}
		survivor, sources := pickSurvivor(members[r], richBySide)
		groups = append(groups, mergeGroup{survivor, sources})
	}
	return groups, residual, applied
}

func writeGroups(worklistPath string, groups []mergeGroup) error {
	wl, err := os.Create(worklistPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(wl)
	for _, g := range groups {
		if err := enc.Encode(map[string]any{
			"class": "character", "survivor": g.survivor, "sources": g.sources,
		}); err != nil {
			wl.Close()
			return err
		}
	}
	return wl.Close()
}

func writeResidual(residualPath string, residual []string) error {
	if residualPath == "" {
		return nil
	}
	return os.WriteFile(residualPath, []byte(strings.Join(residual, "\n")+"\n"), 0o644)
}

func runPanelEmit(pairs2Path, verdicts2Path, worklistPath, residualPath string, out io.Writer) error {
	ppairs, err := loadPanelPairs(pairs2Path)
	if err != nil {
		return err
	}
	verdicts, err := personadj.LoadVerdicts(verdicts2Path)
	if err != nil {
		return err
	}
	stats := map[string]int{}
	edges, residual := panelEdges(ppairs, verdicts, true, stats)

	sourcesBySide := map[int64][]string{}
	richBySide := map[int64]richness{}
	for _, p := range ppairs {
		sourcesBySide[p.A] = p.ASources
		sourcesBySide[p.B] = p.BSources
		richBySide[p.A] = p.ARich
		richBySide[p.B] = p.BRich
	}
	groups, conflicts, accepted := unionEdges(edges, sourcesBySide, richBySide, stats)
	residual = append(residual, conflicts...)
	if err := writeGroups(worklistPath, groups); err != nil {
		return err
	}
	if err := writeResidual(residualPath, residual); err != nil {
		return err
	}
	fmt.Fprintf(out, "pairs=%d accept_lowconf=%d accept_unsure=%d accept_instance=%d samesource_edges=%d "+
		"closed_distinct=%d closed_instance=%d residual=%d unjudged=%d edge_conflict=%d "+
		"edges_applied=%d groups_emitted=%d\n",
		len(ppairs), stats["accept_"+catLowConf], stats["accept_"+catUnsure], stats["accept_"+catInstance],
		countCat(ppairs, catSameSource),
		stats["closed_distinct"], stats["closed_instance"], stats["residual"], stats["unjudged"],
		stats["edge_conflict"], accepted, len(groups))
	return nil
}

func countCat(ps []panelPair, cat string) int {
	n := 0
	for _, p := range ps {
		if p.Cat == cat {
			n++
		}
	}
	return n
}

func panelLine(p panelPair, why string, reasons []string) string {
	line := fmt.Sprintf("[%s/%s] tier=%d %d %s(%s) <-> %d %s(%s) r1=%.2f",
		why, p.Cat, p.Tier, p.A, p.AName, strings.Join(p.ASources, "+"),
		p.B, p.BName, strings.Join(p.BSources, "+"), p.R1Conf)
	if len(reasons) > 0 {
		line += " | " + strings.Join(reasons, " | ")
	}
	return line
}

func pairMetas(ps []panelPair) []pairMeta {
	out := make([]pairMeta, len(ps))
	for i, p := range ps {
		out[i] = p.pairMeta
	}
	return out
}

func overlaps(a, b []string) bool {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if set[s] {
			return true
		}
	}
	return false
}

func loadRedirects(db *gorm.DB, ids map[int64]bool) (map[int64]int64, error) {
	all := make([]int64, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	out := map[int64]int64{}
	for _, chunk := range chunks(all, 10000) {
		type rrow struct {
			OldID     int64
			CurrentID int64
		}
		var rows []rrow
		if err := db.Raw(`SELECT old_id, current_id FROM catalog_redirect
			WHERE entity_type = 4 AND old_id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("redirects: %w", err)
		}
		for _, r := range rows {
			out[r.OldID] = r.CurrentID
		}
	}
	return out, nil
}
