package importer

import (
	"log/slog"

	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
)

const (
	ruleDLsiteEdition     = "rule:dlsite-edition"
	ruleDLsiteBgmXlink    = "rule:dlsite-bgm-xlink"
	ruleDLsiteTitleCircle = "rule:dlsite-title+circle"
	ruleDLsiteTitleDate   = "rule:dlsite-title+date"
	ruleDLsiteTitleBgm    = "rule:dlsite-title+bgmdate"
	ruleDLsiteGameImport  = "rule:dlsite-game-import"
)

var dlGameTypes = map[string]struct{}{
	"RPG": {}, "SLN": {}, "ADV": {}, "DNV": {}, "ACN": {},
	"PZL": {}, "ETC": {}, "TBL": {}, "STG": {}, "QIZ": {}, "TYP": {},
}

type DLsiteGamesStats struct {
	Population          int
	TotalGroups         int
	PackProducts        int
	EditionGroups       int
	DeclaredGroups      int
	SplitGroups         int
	TitleAttachedGroups int
	RejectedSkips       int
	QuarantinedGroups   int
	MintedGroups        int
	FoldedGroups        int
	ReleasesPlanned     int
	RefsPlanned         int
	CandidatesPlanned   int
	LimitedGroups       int
	Written             int
	Errors              int
}

func (st DLsiteGamesStats) slogArgs() []any {
	return []any{
		"population", st.Population,
		"total_groups", st.TotalGroups,
		"pack_products", st.PackProducts,
		"edition_groups", st.EditionGroups,
		"declared_groups", st.DeclaredGroups,
		"split_groups", st.SplitGroups,
		"title_attached_groups", st.TitleAttachedGroups,
		"rejected_skips", st.RejectedSkips,
		"quarantined_groups", st.QuarantinedGroups,
		"minted_groups", st.MintedGroups,
		"folded_groups", st.FoldedGroups,
		"releases_planned", st.ReleasesPlanned,
		"refs_planned", st.RefsPlanned,
		"candidates_planned", st.CandidatesPlanned,
		"limited_groups", st.LimitedGroups,
		"written", st.Written,
		"errors", st.Errors,
	}
}

type DLsiteGamesRun struct {
	Receipts string
}

type DLsiteGamesReceipt struct {
	Action     string   `json:"action"`
	Rule       string   `json:"rule,omitempty"`
	Worknos    []string `json:"worknos"`
	WorkID     int64    `json:"work_id,omitempty"`
	Hits       []int64  `json:"hits,omitempty"`
	Quarantine bool     `json:"quarantine,omitempty"`
}

type DLsiteHoldoutReport struct {
	N             int
	AttachCorrect int
	AttachWrong   int
	Quarantined   int
	Minted        int
	ByRule        []DLsiteHoldoutRule
	Wrong         []DLsiteGamesReceipt
}

type DLsiteHoldoutRule struct {
	Rule    string
	Correct int
	Wrong   int
}

func (im *Importer) RunDLsiteGames(dlsiteDB *gorm.DB) (DLsiteGamesStats, error) {
	return im.RunDLsiteGamesWith(dlsiteDB, DLsiteGamesRun{})
}

func (im *Importer) RunDLsiteGamesWith(dlsiteDB *gorm.DB, opts DLsiteGamesRun) (DLsiteGamesStats, error) {
	st, receipts, err := im.planDLsiteGames(dlsiteDB)
	if err != nil {
		return st, err
	}
	if err := WriteDLsiteGamesReceipts(opts.Receipts, receipts); err != nil {
		return st, err
	}
	slog.Info("dlsite-games wave summary", st.slogArgs()...)
	return st, nil
}

func (im *Importer) planDLsiteGames(dlsiteDB *gorm.DB) (DLsiteGamesStats, []DLsiteGamesReceipt, error) {
	var st DLsiteGamesStats
	snap, err := im.loadDLsiteGamesSnap(dlsiteDB)
	if err != nil {
		return st, nil, err
	}
	st.Population = len(snap.population) + snap.packCount
	st.PackProducts = snap.packCount
	groups := snap.buildGroups()
	st.TotalGroups = len(groups)
	plans := make([]dlGamePlan, 0, len(groups))
	for _, g := range groups {
		p := snap.decideGroup(g, &st)
		if p.kind != "" {
			plans = append(plans, p)
		}
	}
	plans = snap.foldIntraBatch(plans, &st)
	plans = applyDLsiteGamesLimit(plans, im.limit, &st)
	receipts := receiptsFromPlans(plans)
	countPlanWrites(plans, &st)
	if im.dryRun {
		return st, receipts, nil
	}
	if err := im.writeDLsiteGames(snap, plans, &st); err != nil {
		return st, receipts, err
	}
	return st, receipts, nil
}

type dlGameProd struct {
	workno    string
	name      string
	kana      string
	makerExt  string
	makerName string
	age       string
	workType  string
	ymd       string
	lang      string
	altNames  []string
	pack      bool
	links     []string
	credits   []dlCredit
	y, m, d   *int16
}

func (p dlGameProd) names() []string {
	out := []string{p.name}
	out = append(out, p.altNames...)
	return out
}

func (p dlGameProd) keys() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, n := range p.names() {
		for _, k := range titlekey.Keys(n) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func (p dlGameProd) primaryKey() DLsitePrimaryKey {
	return DLsitePrimaryKey{HasJPN: p.lang == "JPN", YMD: p.ymd, Workno: p.workno}
}

type dlGameGroup struct {
	worknos []string
	pop     []dlGameProd
}

func (g dlGameGroup) primaryKey() DLsitePrimaryKey {
	k := DLsitePrimaryKey{Workno: g.worknos[0]}
	for _, w := range g.worknos {
		if w < k.Workno {
			k.Workno = w
		}
	}
	for _, p := range g.pop {
		if p.lang == "JPN" {
			k.HasJPN = true
		}
		if p.ymd != "" && (k.YMD == "" || p.ymd < k.YMD) {
			k.YMD = p.ymd
		}
	}
	return k
}

func (g dlGameGroup) primaryMember() dlGameProd {
	best := g.pop[0]
	for _, p := range g.pop[1:] {
		if DLsitePrimaryLess(p.primaryKey(), best.primaryKey()) {
			best = p
		}
	}
	return best
}

func (g dlGameGroup) keys() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range g.pop {
		for _, k := range p.keys() {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

type dlGamePlan struct {
	kind       string
	rule       string
	workID     int64
	members    []dlGameProd
	quarantine bool
	hits       []int64
	folded     int
}

type dlGamesSnap struct {
	byWorkno     map[string]dlGameProd
	population   map[string]struct{}
	packCount    int
	heldAny      map[string][]int64
	heldLiveStub map[string][]int64
	declared     map[string][]int64
	titleIndex   map[string][]int64
	workLabels   map[int64]map[int64]struct{}
	makerLabel   map[string][]int64
	workDates    map[int64][]string
	bgmDates     map[int64][]string
	rejected     map[string]struct{}
	roleMap      map[string]int64
	creaters     map[string]dlNamed
}
