package service

import "testing"

const bangumiTestSourceID = int16(3)

func bangumiSvc() *PublicService {
	svc := choiceSvc()
	svc.sources[bangumiTestSourceID] = "bangumi"
	return svc
}

func TestSexualReaderGetsTheVNDBCoverOverBangumi(t *testing.T) {
	svc := bangumiSvc()
	vndb, bgm := choiceRow("7d01", "main", vndbTestSourceID), choiceRow("b901", "main", bangumiTestSourceID)
	vndb.Sexual, bgm.PortraitPinned = 2, true
	meta := map[string]ImageMeta{
		vndb.ImageHash: {Width: 320, Height: 404},
		bgm.ImageHash:  {Width: 800, Height: 1130},
	}
	rows := []WorkCoverRow{bgm, vndb}
	wantPortrait(t, svc, rows, meta, true, vndb, "bangumi's mosaicked cover yields to vndb for a sexual reader")
	wantPortrait(t, svc, rows, meta, false, bgm, "a reader without sexual art keeps the safe bangumi cover")
}

func TestBangumiCoverStaysWithoutAVNDBCover(t *testing.T) {
	svc := bangumiSvc()
	bgm, pkg := choiceRow("b902", "main", bangumiTestSourceID), choiceRow("7d02", "pkgfront", vndbTestSourceID)
	meta := map[string]ImageMeta{
		bgm.ImageHash: {Width: 800, Height: 1130},
		pkg.ImageHash: {Width: 400, Height: 560},
	}
	wantPortrait(t, svc, []WorkCoverRow{bgm, pkg}, meta, true, bgm, "a box scan is not a vndb cover")
}

func TestEditorPinnedBangumiCoverBeatsVNDB(t *testing.T) {
	svc := bangumiSvc()
	vndb, bgm := choiceRow("7d03", "main", vndbTestSourceID), choiceRow("b903", "main", bangumiTestSourceID)
	bgm.PortraitPinned, bgm.HumanPinned = true, true
	meta := map[string]ImageMeta{
		vndb.ImageHash: {Width: 320, Height: 404},
		bgm.ImageHash:  {Width: 800, Height: 1130},
	}
	wantPortrait(t, svc, []WorkCoverRow{vndb, bgm}, meta, true, bgm, "an editor's pin outranks the source preference")
}
