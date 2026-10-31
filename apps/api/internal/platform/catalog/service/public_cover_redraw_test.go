package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func redrawSvc() *PublicService {
	svc := bangumiSvc()
	svc.sources[model.SourceAIRedraw] = "ai_redraw"
	return svc
}

func wantSlotPortrait(t *testing.T, svc *PublicService, rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool, want WorkCoverRow, why string) {
	t.Helper()
	slots := svc.pickCoverSlots(rows, meta, allowSexual)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(want.ImageHash) {
		t.Fatalf("portrait = %+v, want %s: %s", portraitOf(slots), want.ImageHash[:8], why)
	}
}

func TestRedrawReplacesALandscapeOrSmallOfficialCover(t *testing.T) {
	svc := redrawSvc()
	wide, small := choiceRow("1a01", "main", vndbTestSourceID), choiceRow("1a02", "main", bangumiTestSourceID)
	redraw := choiceRow("ad01", "main", model.SourceAIRedraw)
	redraw.PortraitPinned = true
	meta := map[string]ImageMeta{
		wide.ImageHash:   {Width: 1920, Height: 1080},
		small.ImageHash:  {Width: 600, Height: 900},
		redraw.ImageHash: {Width: 832, Height: 1248},
	}
	wantPortrait(t, svc, []WorkCoverRow{wide, small, redraw}, meta, false, redraw, "the pinned redraw beats art below the target edge")
}

func TestRedrawYieldsToAnOfficialPortraitAtTheTarget(t *testing.T) {
	svc := redrawSvc()
	official, redraw := choiceRow("1b01", "dig", vndbTestSourceID), choiceRow("ad02", "main", model.SourceAIRedraw)
	redraw.PortraitPinned = true
	meta := map[string]ImageMeta{
		official.ImageHash: {Width: 720, Height: 1080},
		redraw.ImageHash:   {Width: 832, Height: 1248},
	}
	wantSlotPortrait(t, svc, []WorkCoverRow{official, redraw}, meta, false, official, "a later official portrait takes the slot back")
}

func TestRedrawStaysWhenTheOfficialPortraitIsHiddenFromTheReader(t *testing.T) {
	svc := redrawSvc()
	official, redraw := choiceRow("1c01", "main", vndbTestSourceID), choiceRow("ad03", "main", model.SourceAIRedraw)
	official.Sexual, redraw.PortraitPinned = 2, true
	meta := map[string]ImageMeta{
		official.ImageHash: {Width: 1000, Height: 1500},
		redraw.ImageHash:   {Width: 832, Height: 1248},
	}
	wantSlotPortrait(t, svc, []WorkCoverRow{official, redraw}, meta, false, redraw, "an explicit portrait the reader cannot see does not evict the redraw")
	wantSlotPortrait(t, svc, []WorkCoverRow{official, redraw}, meta, true, official, "a sexual reader gets the official portrait")
}

func TestEditorPinnedRedrawStaysOverAnOfficialPortrait(t *testing.T) {
	svc := redrawSvc()
	official, redraw := choiceRow("1d01", "dig", vndbTestSourceID), choiceRow("ad04", "main", model.SourceAIRedraw)
	redraw.PortraitPinned, redraw.HumanPinned = true, true
	meta := map[string]ImageMeta{
		official.ImageHash: {Width: 1000, Height: 1500},
		redraw.ImageHash:   {Width: 832, Height: 1248},
	}
	wantSlotPortrait(t, svc, []WorkCoverRow{official, redraw}, meta, false, redraw, "an editor's pin outranks the yield")
}
