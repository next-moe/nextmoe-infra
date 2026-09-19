package releasemeta

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/sourcedate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDateLostAndDSNRequired(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium := galgameMedium(t)

	wDated := mkWork(t, medium, "guard-dated", nil, nil, 0)
	relDated := mkRelease(t, wDated, 2001, 2, 3)
	wRated := mkWork(t, medium, "guard-rated", nil, nil, model.ContentRatingSensitive)

	staleY, staleM, staleD := int16(1999), int16(1), int16(1)
	newM, newD := int16(6), int16(7)
	w := &writer{db: testDB, stats: &Stats{}, apply: true}
	err := w.writeDate(ctx, dateWrite{
		releaseID: relDated, workID: wDated,
		lane: laneDL, ext: "RJX", kind: kindMove,
		oldY: &staleY, oldM: &staleM, oldD: &staleD,
		oldVal: normalizeDate(&staleY, &staleM, &staleD),
		newVal: datedVal(sourcedate.Verdict{State: sourcedate.Dated, Y: 2020, M: &newM, D: &newD}),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, w.stats.DatesLost)
	assert.Zero(t, w.stats.DatesWritten)
	assertDate(t, relDated, 2001, 2, 3)

	wEdited := mkWork(t, medium, "guard-edited-after-read", nil, nil, 0)
	relEdited := mkRelease(t, wEdited, 2001, 2, 3)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_release SET field_provenance =
		 '{"released_y":[{"source":"curated","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`, relEdited).Error)
	curY, curM, curD := int16(2001), int16(2), int16(3)
	require.NoError(t, w.writeDate(ctx, dateWrite{
		releaseID: relEdited, workID: wEdited,
		lane: laneDL, ext: "RJY", kind: kindMove,
		oldY: &curY, oldM: &curM, oldD: &curD,
		oldVal: normalizeDate(&curY, &curM, &curD),
		newVal: datedVal(sourcedate.Verdict{State: sourcedate.Dated, Y: 2020, M: &newM, D: &newD}),
	}))
	assert.Equal(t, 2, w.stats.DatesLost, "a human stamp landing after the read still refuses the write")
	assert.Zero(t, w.stats.DatesWritten)
	assertDate(t, relEdited, 2001, 2, 3)

	w.fillRating(ctx, wRated, model.ContentRatingR18, true)
	assert.Zero(t, w.stats.RatingFilled)
	assert.Equal(t, 1, w.stats.RatingSkippedNonEmpty, "non-zero rating refused at write time")
	assert.Equal(t, model.ContentRatingSensitive, workRating(t, wRated))

	for _, opts := range []Opts{
		{DlsiteDSN: testDSN, EGDSN: testDSN, GetchuDSN: testDSN},
		{DSN: testDSN, EGDSN: testDSN, GetchuDSN: testDSN},
		{DSN: testDSN, DlsiteDSN: testDSN, GetchuDSN: testDSN},
		{DSN: testDSN, DlsiteDSN: testDSN, EGDSN: testDSN},
	} {
		_, err := Run(context.Background(), opts)
		require.Error(t, err)
	}
}

func TestDateReceipts(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	wFill := mkDateWork(t, medium, "receipt-fill")
	relFill := mkReleaseYMD(t, wFill, nil, nil, nil)
	mkReleaseAnchor(t, relFill, "RJ020001", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ020001", "2020-05-01 00:00:00", "2020-05-01 00:00:00+00", "")

	wMove := mkDateWork(t, medium, "receipt-move")
	relMove := mkReleaseYMD(t, wMove, pi16(2026), pi16(7), pi16(10))
	mkReleaseAnchor(t, relMove, "r2001", reg.vndbSource)
	mkVndbReleaseAt(t, "v2001", "r2001", 20260814, nil, false, false)

	wClear := mkDateWork(t, medium, "receipt-clear")
	relClear := mkReleaseYMD(t, wClear, pi16(2026), pi16(8), pi16(27))
	mkReleaseAnchor(t, relClear, "r2002", reg.vndbSource)
	mkVndbReleaseAt(t, "v2002", "r2002", 99999999, nil, false, false)

	dryPath := filepath.Join(t.TempDir(), "dry.jsonl")
	dryOpts := runOpts(false)
	dryOpts.Receipts = dryPath
	_, err = Run(ctx, dryOpts)
	require.NoError(t, err)
	_, statErr := os.Stat(dryPath)
	assert.True(t, os.IsNotExist(statErr), "dry run with Receipts creates no file")

	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	opts := runOpts(true)
	opts.Receipts = path
	st, err := Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 3, st.DatesWritten)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := splitNonEmpty(string(body))
	require.Len(t, lines, 3)

	byRelease := map[int64]dateReceipt{}
	for _, line := range lines {
		var rec dateReceipt
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		byRelease[rec.ReleaseID] = rec
	}

	fill := byRelease[relFill]
	assert.Equal(t, wFill, fill.WorkID)
	assert.Equal(t, laneDL, fill.Lane)
	assert.Equal(t, "RJ020001", fill.Ext)
	assert.Equal(t, kindFill, fill.Kind)
	assert.Nil(t, fill.Old.Y)
	assert.Nil(t, fill.Old.M)
	assert.Nil(t, fill.Old.D)
	require.NotNil(t, fill.New.Y)
	assert.Equal(t, int16(2020), *fill.New.Y)
	require.NotNil(t, fill.New.M)
	assert.Equal(t, int16(5), *fill.New.M)
	require.NotNil(t, fill.New.D)
	assert.Equal(t, int16(1), *fill.New.D)

	move := byRelease[relMove]
	assert.Equal(t, wMove, move.WorkID)
	assert.Equal(t, laneVNDB, move.Lane)
	assert.Equal(t, "r2001", move.Ext)
	assert.Equal(t, kindMove, move.Kind)
	require.NotNil(t, move.Old.Y)
	assert.Equal(t, int16(2026), *move.Old.Y)
	require.NotNil(t, move.Old.M)
	assert.Equal(t, int16(7), *move.Old.M)
	require.NotNil(t, move.Old.D)
	assert.Equal(t, int16(10), *move.Old.D)
	require.NotNil(t, move.New.Y)
	assert.Equal(t, int16(2026), *move.New.Y)
	require.NotNil(t, move.New.M)
	assert.Equal(t, int16(8), *move.New.M)
	require.NotNil(t, move.New.D)
	assert.Equal(t, int16(14), *move.New.D)

	clr := byRelease[relClear]
	assert.Equal(t, wClear, clr.WorkID)
	assert.Equal(t, laneVNDB, clr.Lane)
	assert.Equal(t, "r2002", clr.Ext)
	assert.Equal(t, kindClear, clr.Kind)
	require.NotNil(t, clr.Old.Y)
	assert.Equal(t, int16(2026), *clr.Old.Y)
	assert.Nil(t, clr.New.Y)
	assert.Nil(t, clr.New.M)
	assert.Nil(t, clr.New.D)
}

func splitNonEmpty(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func TestDateLaneGates(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	wiki := "galgame_wiki"

	wClaimed := mkWork(t, medium, "claimed-dl-undated-eg-dated", str(wiki), i64(9201), model.ContentRatingSensitive)
	relClaimed := mkReleaseYMD(t, wClaimed, nil, nil, nil)
	mkReleaseAnchor(t, relClaimed, "RJ040001", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ040001", "", "", "")
	mkWorkAnchor(t, wClaimed, "851", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 851, "2011-01-01")

	wTwoEg := mkDateWork(t, medium, "two-releases-dl-undated-eg-dated")
	relTwoEg := mkReleaseYMD(t, wTwoEg, nil, nil, nil)
	mkReleaseYMD(t, wTwoEg, nil, nil, nil)
	mkReleaseAnchor(t, relTwoEg, "RJ040002", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ040002", "", "", "")
	mkWorkAnchor(t, wTwoEg, "852", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 852, "2012-02-02")

	wTwoBgm := mkDateWork(t, medium, "two-releases-dl-undated-bgm-dated")
	relTwoBgm := mkReleaseYMD(t, wTwoBgm, nil, nil, nil)
	mkReleaseYMD(t, wTwoBgm, nil, nil, nil)
	mkReleaseAnchor(t, relTwoBgm, "RJ040003", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ040003", "", "", "")
	mkWorkAnchor(t, wTwoBgm, "951", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 951, "2013-03-03", false)

	wDead := mkDateWork(t, medium, "dead-vndb-live-dl")
	relDead := mkReleaseYMD(t, wDead, nil, nil, nil)
	mkReleaseAnchor(t, relDead, "r3001", reg.vndbSource)
	mkVndbReleaseAt(t, "v3001", "r3001", 20200101, nil, false, false)
	require.NoError(t, testDB.Exec(`UPDATE catalog_external_ref SET dead_at = now()
		WHERE entity_type = ? AND entity_id = ? AND external_id = 'r3001'`, model.EntityTypeRelease, relDead).Error)
	mkReleaseAnchor(t, relDead, "RJ040004", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ040004", "2021-07-15 00:00:00", "", "")

	wZero := mkDateWork(t, medium, "zero-month-year-only")
	relZero := mkReleaseYMD(t, wZero, pi16(2026), pi16(0), pi16(0))
	mkReleaseAnchor(t, relZero, "r3002", reg.vndbSource)
	mkVndbReleaseAt(t, "v3002", "r3002", 20269999, nil, false, false)

	wTwoRefs := mkDateWork(t, medium, "vndb-first-ref-unmirrored")
	relTwoRefs := mkReleaseYMD(t, wTwoRefs, nil, nil, nil)
	mkReleaseAnchor(t, relTwoRefs, "r3003", reg.vndbSource)
	mkReleaseAnchor(t, relTwoRefs, "r3004", reg.vndbSource)
	mkVndbReleaseAt(t, "v3004", "r3004", 20230303, nil, false, false)

	st, err := Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Zero(t, st.Errors+st.DatesLost)
	assert.Equal(t, 3, st.DatesUnknown, "the three undated DLsite releases get nothing from a work-level lane that does not apply")
	assert.Equal(t, 1, st.DatesSame, "2026 with zero month and day is the year-only date VNDB gives")
	assert.Equal(t, 2, st.AllFilled)
	assert.Zero(t, st.AllMoved+st.AllCleared+st.EgDateFilled+st.BgmDateFilled)

	assertNoDate(t, relClaimed)
	assertNoDate(t, relTwoEg)
	assertNoDate(t, relTwoBgm)
	assertDate(t, relDead, 2021, 7, 15)
	gy, gm, gd := relDate(t, relZero)
	require.NotNil(t, gy)
	assert.Equal(t, int16(2026), *gy)
	require.NotNil(t, gm)
	assert.Equal(t, int16(0), *gm, "a same verdict writes nothing, so the stored zero stays")
	require.NotNil(t, gd)
	assert.Equal(t, int16(0), *gd)
	assertDate(t, relTwoRefs, 2023, 3, 3)
}
