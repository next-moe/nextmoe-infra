package releasemeta

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/catalog/sourcedate"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

type dateVal struct {
	empty bool
	y     int16
	m, d  *int16
}

func (v dateVal) equal(o dateVal) bool {
	if v.empty || o.empty {
		return v.empty && o.empty
	}
	return v.y == o.y && ptr16eq(v.m, o.m) && ptr16eq(v.d, o.d)
}

func (v dateVal) format() string {
	if v.empty {
		return ""
	}
	if v.m == nil {
		return fmt.Sprintf("%04d", v.y)
	}
	if v.d == nil {
		return fmt.Sprintf("%04d-%02d", v.y, *v.m)
	}
	return fmt.Sprintf("%04d-%02d-%02d", v.y, *v.m, *v.d)
}

func (v dateVal) receipt() receiptYMD {
	if v.empty {
		return receiptYMD{}
	}
	y := v.y
	return receiptYMD{Y: &y, M: v.m, D: v.d}
}

func ptr16eq(a, b *int16) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func normalizeDate(y, m, d *int16) dateVal {
	if y == nil || *y == 0 {
		return dateVal{empty: true}
	}
	v := dateVal{y: *y}
	if m == nil || *m == 0 {
		return v
	}
	mv := *m
	v.m = &mv
	if d == nil || *d == 0 {
		return v
	}
	dv := *d
	v.d = &dv
	return v
}

func datedVal(v sourcedate.Verdict) dateVal {
	out := dateVal{y: v.Y}
	if v.M != nil {
		m := *v.M
		out.m = &m
		if v.D != nil {
			d := *v.D
			out.d = &d
		}
	}
	return out
}

type dateCandidate struct {
	ReleaseID    int64   `gorm:"column:release_id"`
	WorkID       int64   `gorm:"column:work_id"`
	ReleasedY    *int16  `gorm:"column:released_y"`
	ReleasedM    *int16  `gorm:"column:released_m"`
	ReleasedD    *int16  `gorm:"column:released_d"`
	Site         *string `gorm:"column:site"`
	LiveReleases int     `gorm:"column:live_releases"`
	DateSource   string  `gorm:"column:date_source"`
	refs         map[string][]string
}

func (c dateCandidate) egOK() bool {
	return c.LiveReleases == 1 && (c.Site == nil || *c.Site == "")
}

func (c dateCandidate) bgmOK() bool {
	return c.LiveReleases == 1
}

type laneVerdict struct {
	state sourcedate.State
	val   dateVal
	ext   string
	ok    bool
}

func runDateSync(ctx context.Context, db, dlDB, egDB, gcDB *gorm.DB, w *writer, reg registry, opts Opts, now time.Time, maxYear int) error {
	cands, err := loadDateCandidates(ctx, db, reg, opts.Limit, opts.Offset)
	if err != nil {
		return fmt.Errorf("load date candidates: %w", err)
	}
	st := w.stats
	st.DatesCandidates = len(cands)
	if len(cands) == 0 {
		return nil
	}
	if err := attachDateRefs(ctx, db, reg, cands); err != nil {
		return fmt.Errorf("load date refs: %w", err)
	}

	vndbIDs, dlNos, gcIDs, egIDs, bgmIDs := collectMirrorKeys(cands)
	vndb, err := loadVndbReleased(ctx, db, vndbIDs)
	if err != nil {
		return fmt.Errorf("load vndb mirror dates: %w", err)
	}
	dl, err := loadDlsiteYMD(ctx, dlDB, dlNos)
	if err != nil {
		return fmt.Errorf("load dlsite mirror dates: %w", err)
	}
	gc, err := loadGetchuDates(ctx, gcDB, gcIDs)
	if err != nil {
		return fmt.Errorf("load getchu mirror dates: %w", err)
	}
	eg, err := loadEgSelldays(ctx, egDB, egIDs)
	if err != nil {
		return fmt.Errorf("load EG mirror selldays: %w", err)
	}
	bgm, err := loadBgmDates(ctx, db, bgmIDs)
	if err != nil {
		return fmt.Errorf("load bangumi mirror dates: %w", err)
	}

	for i := range cands {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c := &cands[i]
		if err := decideDate(ctx, w, c, vndb, dl, gc, eg, bgm, now, maxYear); err != nil {
			return err
		}
	}
	return nil
}

func decideDate(ctx context.Context, w *writer, c *dateCandidate, vndb map[string]int64, dl, gc map[string]string, eg map[int64]string, bgm map[int64]string, now time.Time, maxYear int) error {
	st := w.stats
	current := normalizeDate(c.ReleasedY, c.ReleasedM, c.ReleasedD)

	type laneHit struct {
		lane string
		lv   laneVerdict
	}
	var hits []laneHit
	for _, lane := range []string{laneVNDB, laneDL, laneGC, laneEG, laneBGM} {
		refs := c.refs[lane]
		if len(refs) == 0 {
			continue
		}
		sortLaneRefs(lane, refs)
		lv, missing := verdictForLane(lane, refs, vndb, dl, gc, eg, bgm, now, maxYear)
		if missing {
			countMissing(st, lane)
			continue
		}
		hits = append(hits, laneHit{lane: lane, lv: lv})
	}

	var desired dateVal
	desired.empty = true
	deciding := ""
	ext := ""
	var firstTBA *laneHit
	foundDated := false
	for i := range hits {
		h := &hits[i]
		switch h.lv.state {
		case sourcedate.Dated:
			if !foundDated {
				foundDated = true
				desired = h.lv.val
				deciding = h.lane
				ext = h.lv.ext
			}
		case sourcedate.TBA:
			if firstTBA == nil {
				cp := *h
				firstTBA = &cp
			}
		}
	}
	if !foundDated {
		if firstTBA != nil {
			desired = dateVal{empty: true}
			deciding = firstTBA.lane
			ext = firstTBA.lv.ext
		} else {
			st.DatesUnknown++
			return nil
		}
	}

	if desired.equal(current) {
		st.DatesSame++
		return nil
	}
	kind := kindMove
	switch {
	case current.empty:
		kind = kindFill
	case desired.empty:
		kind = kindClear
	}
	if provenance.IsHuman(c.DateSource) {
		st.DatesHuman++
		return nil
	}
	countPlanned(st, deciding, kind)
	collectDate(st, DateSample{
		Lane: deciding, Kind: kind,
		WorkID: c.WorkID, ReleaseID: c.ReleaseID, Ext: ext,
		Old: current.format(), New: desired.format(),
	})
	return w.writeDate(ctx, dateWrite{
		releaseID: c.ReleaseID, workID: c.WorkID,
		lane: deciding, ext: ext, kind: kind,
		oldY: c.ReleasedY, oldM: c.ReleasedM, oldD: c.ReleasedD,
		oldVal: current, newVal: desired,
	})
}

func verdictForLane(lane string, refs []string, vndb map[string]int64, dl, gc map[string]string, eg map[int64]string, bgm map[int64]string, now time.Time, maxYear int) (laneVerdict, bool) {
	for _, ext := range refs {
		switch lane {
		case laneVNDB:
			released, ok := vndb[ext]
			if !ok {
				continue
			}
			return verdictFrom(sourcedate.VNDB(released, maxYear), ext), false
		case laneDL:
			ymd, ok := dl[ext]
			if !ok {
				continue
			}
			return verdictFrom(sourcedate.DLsite(ymd, maxYear), ext), false
		case laneGC:
			s, ok := gc[ext]
			if !ok {
				continue
			}
			return verdictFrom(sourcedate.Getchu(s, maxYear), ext), false
		case laneEG:
			id, err := strconv.ParseInt(ext, 10, 64)
			if err != nil {
				continue
			}
			s, ok := eg[id]
			if !ok {
				continue
			}
			return verdictFrom(sourcedate.EG(s, now), ext), false
		case laneBGM:
			id, err := strconv.ParseInt(ext, 10, 64)
			if err != nil {
				continue
			}
			s, ok := bgm[id]
			if !ok {
				continue
			}
			return verdictFrom(sourcedate.Bangumi(s, maxYear), ext), false
		}
	}
	return laneVerdict{}, true
}

func verdictFrom(v sourcedate.Verdict, ext string) laneVerdict {
	out := laneVerdict{state: v.State, ext: ext, ok: true}
	if v.State == sourcedate.Dated {
		out.val = datedVal(v)
	} else {
		out.val = dateVal{empty: true}
	}
	return out
}

func sortLaneRefs(lane string, refs []string) {
	sort.Slice(refs, func(i, j int) bool {
		if lane == laneDL {
			return refs[i] < refs[j]
		}
		return numericExt(lane, refs[i]) < numericExt(lane, refs[j])
	})
}

func numericExt(lane, ext string) int64 {
	s := ext
	if lane == laneVNDB {
		s = strings.TrimPrefix(s, "r")
		s = strings.TrimPrefix(s, "R")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return n
}

func countMissing(st *Stats, lane string) {
	switch lane {
	case laneVNDB:
		st.VndbMissing++
	case laneDL:
		st.DlMissing++
	case laneGC:
		st.GcMissing++
	case laneEG:
		st.EgMissing++
	case laneBGM:
		st.BgmMissing++
	}
}

func countPlanned(st *Stats, lane, kind string) {
	switch kind {
	case kindFill:
		st.AllFilled++
	case kindMove:
		st.AllMoved++
	case kindClear:
		st.AllCleared++
	}
	switch lane {
	case laneVNDB:
		switch kind {
		case kindFill:
			st.VndbDateFilled++
		case kindMove:
			st.VndbDateMoved++
		case kindClear:
			st.VndbDateCleared++
		}
	case laneDL:
		switch kind {
		case kindFill:
			st.DlDateFilled++
		case kindMove:
			st.DlDateMoved++
		case kindClear:
			st.DlDateCleared++
		}
	case laneGC:
		switch kind {
		case kindFill:
			st.GcDateFilled++
		case kindMove:
			st.GcDateMoved++
		case kindClear:
			st.GcDateCleared++
		}
	case laneEG:
		switch kind {
		case kindFill:
			st.EgDateFilled++
		case kindMove:
			st.EgDateMoved++
		case kindClear:
			st.EgDateCleared++
		}
	case laneBGM:
		switch kind {
		case kindFill:
			st.BgmDateFilled++
		case kindMove:
			st.BgmDateMoved++
		case kindClear:
			st.BgmDateCleared++
		}
	}
}
