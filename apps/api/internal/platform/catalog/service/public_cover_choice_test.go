package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

const (
	vndbTestSourceID    = int16(2)
	curatedTestSourceID = int16(12)
	upscaleTestSourceID = int16(13)
)

func choiceSvc() *PublicService {
	return &PublicService{cdnBase: testCDNBase, sources: map[int16]string{
		vndbTestSourceID: "vndb", curatedTestSourceID: "curated", upscaleTestSourceID: "upscale",
		censoredTestSourceID: "censored",
	}}
}

func choiceRow(seed, kind string, source int16) WorkCoverRow {
	return WorkCoverRow{ID: int64(len(seed)) + int64(seed[0]), ImageHash: hash64(seed), Kind: kind, SourceID: source}
}

func wantPortrait(t *testing.T, svc *PublicService, rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool, want WorkCoverRow, why string) {
	t.Helper()
	slots := svc.pickCoverSlots(rows, meta, allowSexual)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(want.ImageHash) {
		t.Fatalf("portrait = %+v, want %s: %s", portraitOf(slots), want.ImageHash[:8], why)
	}
	if got := svc.pickListCover(rows, allowSexual); got != svc.imageURL(want.ImageHash) {
		t.Fatalf("list cover = %q, want %s: %s", got, want.ImageHash[:8], why)
	}
}

func TestMostVotedCoverBeatsTheMachinePinAndTheSharperCover(t *testing.T) {
	svc := choiceSvc()
	pinned, sharp, voted := choiceRow("91a1", "main", vndbTestSourceID), choiceRow("5a1a", "dig", vndbTestSourceID), choiceRow("707e", "main", vndbTestSourceID)
	pinned.PortraitPinned = true
	pinned.Votes, voted.Votes = 1, 2
	meta := map[string]ImageMeta{
		pinned.ImageHash: {Width: 256, Height: 360},
		sharp.ImageHash:  {Width: 1000, Height: 1400},
		voted.ImageHash:  {Width: 1920, Height: 1080},
	}
	wantPortrait(t, svc, []WorkCoverRow{pinned, sharp, voted}, meta, false, voted,
		"the strict vote leader takes the portrait whatever its shape")
}

func TestTiedVotesFallBackToTheLadder(t *testing.T) {
	svc := choiceSvc()
	a, b, sharp := choiceRow("a0a0", "main", vndbTestSourceID), choiceRow("b0b0", "main", vndbTestSourceID), choiceRow("5a1b", "dig", vndbTestSourceID)
	a.Votes, b.Votes = 3, 3
	meta := map[string]ImageMeta{
		a.ImageHash:     {Width: 256, Height: 360},
		b.ImageHash:     {Width: 256, Height: 360},
		sharp.ImageHash: {Width: 1000, Height: 1400},
	}
	slots := svc.pickCoverSlots([]WorkCoverRow{a, b, sharp}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(sharp.ImageHash) {
		t.Fatalf("portrait = %+v, want the sharpest cover: a tie at the top elects nobody", portraitOf(slots))
	}
}

func TestVoteLeaderIsCountedAmongTheCoversTheViewerMaySee(t *testing.T) {
	svc := choiceSvc()
	explicit, safe, sharp := choiceRow("e0e0", "main", vndbTestSourceID), choiceRow("5afa", "main", vndbTestSourceID), choiceRow("5a1c", "dig", vndbTestSourceID)
	explicit.Sexual, explicit.Votes, safe.Votes = 2, 5, 1
	meta := map[string]ImageMeta{
		explicit.ImageHash: {Width: 700, Height: 1000},
		safe.ImageHash:     {Width: 256, Height: 360},
		sharp.ImageHash:    {Width: 1000, Height: 1400},
	}
	rows := []WorkCoverRow{explicit, safe, sharp}
	wantPortrait(t, svc, rows, meta, true, explicit, "an R18 viewer sees the overall leader")
	wantPortrait(t, svc, rows, meta, false, safe, "without permission the leader is the most voted visible cover")
}

func TestEditorPinBeatsVotes(t *testing.T) {
	svc := choiceSvc()
	pin, voted := choiceRow("9e9e", "", curatedTestSourceID), choiceRow("7070", "main", vndbTestSourceID)
	pin.PortraitPinned, pin.HumanPinned, voted.Votes = true, true, 9
	meta := map[string]ImageMeta{
		pin.ImageHash:   {Width: 600, Height: 900},
		voted.ImageHash: {Width: 600, Height: 900},
	}
	wantPortrait(t, svc, []WorkCoverRow{voted, pin}, meta, false, pin, "an editor's pin outranks every vote")
}

func TestOfficialArtReplacesUploadedCovers(t *testing.T) {
	svc := choiceSvc()
	legacy, upscale, official := choiceRow("1e9a", "", curatedTestSourceID), choiceRow("05ca", "", upscaleTestSourceID), choiceRow("0ff1", "main", vndbTestSourceID)
	upscale.PortraitPinned = true
	legacyBanner := choiceRow("1eba", "", curatedTestSourceID)
	official.Sexual = 2
	meta := map[string]ImageMeta{
		legacy.ImageHash:       {Width: 774, Height: 1080},
		upscale.ImageHash:      {Width: 1080, Height: 1500},
		official.ImageHash:     {Width: 256, Height: 360},
		legacyBanner.ImageHash: {Width: 1920, Height: 1080},
	}
	rows := []WorkCoverRow{legacy, upscale, official, legacyBanner}
	wantPortrait(t, svc, rows, meta, true, official,
		"a smaller official cover beats a sharper or machine-pinned upload")
	slots := svc.pickCoverSlots(rows, meta, true)
	if bannerOf(slots) != nil {
		t.Fatalf("banner = %+v, want null: an uploaded banner is replaced too", slots.Banner)
	}
	if got := svc.pickCoverSlots(rows, meta, false); portraitOf(got) != nil {
		t.Fatalf("SFW portrait = %+v, want null: an explicit official cover is not swapped for an upload", portraitOf(got))
	}
}

func TestUploadedCoverStaysWithoutOfficialArt(t *testing.T) {
	svc := choiceSvc()
	legacy, box := choiceRow("1e9b", "", curatedTestSourceID), choiceRow("b0c5", "pkgfront", vndbTestSourceID)
	meta := map[string]ImageMeta{
		legacy.ImageHash: {Width: 600, Height: 900},
		box.ImageHash:    {Width: 1000, Height: 1400},
	}
	wantPortrait(t, svc, []WorkCoverRow{legacy, box}, meta, false, legacy,
		"a box scan is not official cover art, so the upload is all the work has")
}

func TestVotedUploadStillWins(t *testing.T) {
	svc := choiceSvc()
	legacy, official := choiceRow("1e9c", "", curatedTestSourceID), choiceRow("0ff2", "main", vndbTestSourceID)
	legacy.Votes = 1
	meta := map[string]ImageMeta{
		legacy.ImageHash:   {Width: 600, Height: 900},
		official.ImageHash: {Width: 600, Height: 900},
	}
	wantPortrait(t, svc, []WorkCoverRow{legacy, official}, meta, false, legacy, "votes outrank the official-first rule")
}

func TestVotesOnMergedInCoversDoNotElect(t *testing.T) {
	svc := choiceSvc()
	own, carried := choiceRow("0a0a", "main", vndbTestSourceID), choiceRow("ca1a", "main", vndbTestSourceID)
	carried.MergedIn, carried.Votes = true, 4
	meta := map[string]ImageMeta{
		own.ImageHash:     {Width: 600, Height: 900},
		carried.ImageHash: {Width: 1000, Height: 1400},
	}
	wantPortrait(t, svc, []WorkCoverRow{own, carried}, meta, false, own, "a merge keeps the survivor's face")
}

func TestCoverLoaderCarriesVotesAndEditorPins(t *testing.T) {
	ctx := context.Background()
	w := createWork(t, "封面投票ローダー")
	mk := func(seed string, source int16, pinned bool, prov string) int64 {
		c := &model.CatalogWorkCover{WorkID: w.ID, ImageHash: hash64(seed), SourceID: source,
			PortraitPinned: pinned, FieldProvenance: datatypes.JSON(prov)}
		require.NoError(t, testDB.Create(c).Error)
		return c.ID
	}
	curatedPin := mk("c9a1", curatedTestSourceID, true, `{}`)
	machinePin := mk("3ac1", vndbTestSourceID, true, `{}`)
	editedPin := mk("ed17", vndbTestSourceID, true, `{"portrait_pinned":[{"source":"user"}]}`)
	voted := mk("707f", vndbTestSourceID, false, `{}`)
	for uid := int64(1); uid <= 2; uid++ {
		require.NoError(t, testDB.Create(&model.CatalogCoverVote{WorkID: w.ID, CoverID: voted, ActorUID: 9100 + uid, Site: "kungal"}).Error)
	}

	got, err := NewReadService(testDB).loadWorkCovers(ctx, []claimSubject{{WorkID: w.ID}})
	require.NoError(t, err)
	byID := map[int64]WorkCoverRow{}
	for _, r := range got[w.ID] {
		byID[r.ID] = r
	}
	assert.True(t, byID[curatedPin].HumanPinned, "a pin in the curated lane is an editor's")
	assert.False(t, byID[machinePin].HumanPinned, "a pin with no human provenance is a machine pin")
	assert.True(t, byID[editedPin].HumanPinned, "an upstream pin an editor set carries human provenance")
	assert.Equal(t, 2, byID[voted].Votes)
	assert.Equal(t, 0, byID[machinePin].Votes)
}
