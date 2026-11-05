package getchuattach

import (
	"regexp"
	"slices"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"
)

const (
	placedExtlink = "vndb-extlink"
	placedJAN     = "jan"
	placedDay     = "same-day"
)

const (
	tieTitle  = "title"
	tieLowest = "lowest"
)

type relRow struct {
	ID         int64
	Kind       int16
	Day        string
	Title      string
	Platforms  []string
	GetchuOnly bool
}

type placement struct {
	ReleaseID int64
	By        string
	Tie       string
}

type platformFit int8

const (
	fitNone platformFit = iota
	fitUnknown
	fitExact
)

type editionFormat int8

const (
	formatUnknown editionFormat = iota
	formatPhysical
	formatDigital
)

const pcPlatform = "pc"

var pcPlatforms = []string{"win", "dos", "p98", "p88", "x68", "fmt", "mac", "lin", "msx", "fm7", "x1s"}

var consolePatterns = []struct {
	re   *regexp.Regexp
	code string
}{
	{regexp.MustCompile(`(?i)switch\s*2`), "sw2"},
	{regexp.MustCompile(`(?i)nintendo\s*switch|switch\s*版`), "swi"},
	{regexp.MustCompile(`(?i)playstation\s*5|\bps5\b`), "ps5"},
	{regexp.MustCompile(`(?i)playstation\s*4|\bps4\b`), "ps4"},
	{regexp.MustCompile(`(?i)playstation\s*3|\bps3\b`), "ps3"},
	{regexp.MustCompile(`(?i)playstation\s*2|\bps2\b`), "ps2"},
	{regexp.MustCompile(`(?i)(playstation|ps)\s*vita`), "psv"},
	{regexp.MustCompile(`(?i)\bpsp\b|\bumd\b`), "psp"},
	{regexp.MustCompile(`(?i)xbox\s*360`), "xb3"},
	{regexp.MustCompile(`(?i)xbox\s*one`), "xbo"},
	{regexp.MustCompile(`(?i)xbox\s*series`), "xxs"},
	{regexp.MustCompile(`(?i)ニンテンドー\s*3ds|\b3ds\b`), "n3d"},
	{regexp.MustCompile(`(?i)ニンテンドー\s*ds|nintendo\s*ds\b`), "nds"},
	{regexp.MustCompile(`(?i)dreamcast|ドリームキャスト`), "drc"},
	{regexp.MustCompile(`(?i)セガサターン|sega\s*saturn`), "sat"},
	{regexp.MustCompile(`(?i)playstation\s*(版|\])|\bps1\b`), "ps1"},
	{regexp.MustCompile(`(?i)blu-?ray\s*players\s*game`), "bdp"},
	{regexp.MustCompile(`(?i)dvd\s*players\s*game|dvdpg`), "dvd"},
}

var digitalTitle = regexp.MustCompile(`ダウンロード|\bdl|download`)

func itemPlatform(it item) string {
	s := nfkc(it.Title) + " " + nfkc(it.Media)
	for _, p := range consolePatterns {
		if p.re.MatchString(s) {
			return p.code
		}
	}
	return pcPlatform
}

func fitPlatform(item string, row []string) platformFit {
	if len(row) == 0 {
		return fitUnknown
	}
	for _, p := range row {
		if p == item || (item == pcPlatform && slices.Contains(pcPlatforms, p)) {
			return fitExact
		}
	}
	return fitNone
}

func itemFormat(it item) editionFormat {
	if releaseKind(it.Media) == model.ReleaseKindDigital || titleSaysDigital(it.Title) {
		return formatDigital
	}
	return formatPhysical
}

func rowFormat(r relRow) editionFormat {
	switch r.Kind {
	case model.ReleaseKindDigital:
		return formatDigital
	case model.ReleaseKindPhysical:
		return formatPhysical
	}
	if titleSaysDigital(r.Title) {
		return formatDigital
	}
	n := nfkcFold(r.Title)
	for _, s := range []string{"パッケージ", "初回", "限定", "通常版", "豪華", "特装"} {
		if strings.Contains(n, s) {
			return formatPhysical
		}
	}
	return formatUnknown
}

func titleSaysDigital(title string) bool {
	return digitalTitle.MatchString(nfkcFold(title))
}

// counterpart finds the release already standing for the edition a Getchu
// item sells, so the item's ref joins that row instead of founding a second
// one. Two rows for one edition each follow their own source, and the work's
// date is the earliest of them: 君色パトラIF (VNDB v58614) stayed in the
// 2026-11 calendar after its delay to 2027-01-28 because Getchu never moved
// its three SKUs while VNDB and EG did.
func counterpart(it item, workID int64, snap snapshot) (placement, bool) {
	var rows []relRow
	for _, r := range snap.workReleases[workID] {
		if !r.GetchuOnly {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return placement{}, false
	}
	if pool := rowsIn(rows, snap.vndbLinked[it.GetchuID]); len(pool) > 0 {
		return choose(it, pool, placedExtlink), true
	}
	if it.JAN != 0 {
		var ids []int64
		for _, h := range snap.janVNDB[it.JAN] {
			ids = append(ids, h.ReleaseID)
		}
		if pool := rowsIn(rows, ids); len(pool) > 0 {
			return choose(it, pool, placedJAN), true
		}
	}
	day := catalogDay(it.ReleaseDate)
	if day == "" {
		return placement{}, false
	}
	plat, format := itemPlatform(it), itemFormat(it)
	var pool []relRow
	for _, r := range rows {
		if r.Day != day {
			continue
		}
		if f := rowFormat(r); f != formatUnknown && f != format {
			continue
		}
		if fitPlatform(plat, r.Platforms) != fitNone {
			pool = append(pool, r)
		}
	}
	pool = narrow(pool, func(r relRow) bool { return fitPlatform(plat, r.Platforms) == fitExact })
	pool = narrow(pool, func(r relRow) bool { return rowFormat(r) == format })
	if len(pool) == 0 {
		return placement{}, false
	}
	return choose(it, pool, placedDay), true
}

func narrow(pool []relRow, keep func(relRow) bool) []relRow {
	var out []relRow
	for _, r := range pool {
		if keep(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return pool
	}
	return out
}

func rowsIn(rows []relRow, ids []int64) []relRow {
	var out []relRow
	for _, r := range rows {
		if slices.Contains(ids, r.ID) {
			out = append(out, r)
		}
	}
	return out
}

func choose(it item, pool []relRow, by string) placement {
	if len(pool) == 1 {
		return placement{ReleaseID: pool[0].ID, By: by}
	}
	want := bigrams(it.Title)
	best, bestScore, tied := int64(0), 0.0, false
	for _, r := range pool {
		score := jaccard(want, bigrams(r.Title))
		switch {
		case score > bestScore:
			best, bestScore, tied = r.ID, score, false
		case score == bestScore:
			tied = true
		}
	}
	if bestScore > 0 && !tied {
		return placement{ReleaseID: best, By: by, Tie: tieTitle}
	}
	lowest := pool[0].ID
	for _, r := range pool[1:] {
		lowest = min(lowest, r.ID)
	}
	return placement{ReleaseID: lowest, By: by, Tie: tieLowest}
}

func bigrams(title string) map[string]struct{} {
	runes := []rune(titlekey.Loose(title))
	out := make(map[string]struct{}, len(runes))
	for i := 0; i+1 < len(runes); i++ {
		out[string(runes[i:i+2])] = struct{}{}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for k := range a {
		if _, ok := b[k]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
