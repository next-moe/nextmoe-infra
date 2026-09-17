package orglabels

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
)

func newGrader(workLabels map[int64][]int64, labelNorms map[string][]int64) *grader {
	return &grader{
		workLabels: workLabels,
		labelNorms: labelNorms,
		labelLoose: looseIndex(labelNorms),
		rules:      ruleSet{"coworks", "cowork-name", "name-only", "name-loose", "new"},
	}
}

func TestGrade_ExactByCoworks(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10}, 2: {10}}, nil)
	r := g.grade(&orgRec{extID: "p1", works: []int64{1, 2}})
	assert.Equal(t, resAnchorExisting, r.kind)
	assert.Equal(t, int64(10), r.labelID)
	assert.Equal(t, model.LinkKindExact, r.tier)
	assert.Equal(t, "coworks", r.rule)
	assert.Equal(t, 2, r.share)
}

func TestGrade_ExactByCoworkName(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10}}, map[string][]int64{"age": {10}})
	r := g.grade(&orgRec{extID: "p1", works: []int64{1}, nameNorms: []string{"age"}})
	assert.Equal(t, resAnchorExisting, r.kind)
	assert.Equal(t, int64(10), r.labelID)
	assert.Equal(t, model.LinkKindExact, r.tier)
	assert.Equal(t, "cowork-name", r.rule)
}

func TestGrade_SingleShareNoNameIsUngradeable(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10}}, nil)
	r := g.grade(&orgRec{extID: "p1", works: []int64{1}})
	assert.Equal(t, resSkipUngradeable, r.kind)
}

func TestGrade_NameOnlyProbable(t *testing.T) {
	g := newGrader(nil, map[string][]int64{"age": {10}})
	r := g.grade(&orgRec{extID: "p1", nameNorms: []string{"age"}})
	assert.Equal(t, resAnchorExisting, r.kind)
	assert.Equal(t, int64(10), r.labelID)
	assert.Equal(t, model.LinkKindProbable, r.tier)
	assert.Equal(t, "name-only", r.rule)
}

func TestGrade_NameOnlyAmbiguous(t *testing.T) {
	g := newGrader(nil, map[string][]int64{"age": {10, 11}})
	r := g.grade(&orgRec{extID: "p1", nameNorms: []string{"age"}})
	assert.Equal(t, resSkipAmbiguous, r.kind)
}

func TestGrade_NewLabelWhenWorksButNoMatch(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10}}, nil)
	r := g.grade(&orgRec{extID: "p1", works: []int64{99}, canCreate: true})
	assert.Equal(t, resNewLabel, r.kind)
}

func TestGrade_InTypeNeverMints(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10}}, nil)
	r := g.grade(&orgRec{extID: "p1", works: []int64{99}, canCreate: false})
	assert.Equal(t, resSkipNoMatch, r.kind)
}

func TestGrade_NoWorksNoNameSkips(t *testing.T) {
	g := newGrader(nil, nil)
	r := g.grade(&orgRec{extID: "p1", canCreate: true})
	assert.Equal(t, resSkipNoMatch, r.kind)
}

func TestGrade_PicksHighestShare(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10, 11}, 2: {10}, 3: {10}}, nil)
	r := g.grade(&orgRec{extID: "p1", works: []int64{1, 2, 3}})
	assert.Equal(t, int64(10), r.labelID)
	assert.Equal(t, 3, r.share)
}

func TestGrade_NameBreaksShareTie(t *testing.T) {
	g := newGrader(map[int64][]int64{1: {10, 11}, 2: {10, 11}}, map[string][]int64{"x": {11}})
	r := g.grade(&orgRec{extID: "p1", works: []int64{1, 2}, nameNorms: []string{"x"}})
	assert.Equal(t, int64(11), r.labelID)
	assert.Equal(t, model.LinkKindExact, r.tier)
	assert.Equal(t, "coworks", r.rule)
}

func TestGrade_LooseTwinIsProbableNotNew(t *testing.T) {
	g := newGrader(nil, map[string][]int64{"株式会社アリスソフト": {600}})
	r := g.grade(&orgRec{extID: "50", works: []int64{99}, nameNorms: []string{"アリスソフト"}, canCreate: true})
	assert.Equal(t, resAnchorExisting, r.kind)
	assert.Equal(t, int64(600), r.labelID)
	assert.Equal(t, model.LinkKindProbable, r.tier)
	assert.Equal(t, "name-loose", r.rule)
}

func TestGrade_LooseTwinOfTwoLabelsIsAmbiguous(t *testing.T) {
	g := newGrader(nil, map[string][]int64{"(株)ういんどみる": {1}, "ういんどみる co., ltd.": {2}})
	r := g.grade(&orgRec{extID: "p1", works: []int64{99}, nameNorms: []string{"ういんどみる"}, canCreate: true})
	assert.Equal(t, resSkipAmbiguous, r.kind)
}

func TestGrade_LooseNameNeverAnchorsANonMintingOrg(t *testing.T) {
	g := newGrader(nil, map[string][]int64{"株式会社アリスソフト": {600}})
	r := g.grade(&orgRec{extID: "300", works: []int64{99}, nameNorms: []string{"アリスソフト"}})
	assert.Equal(t, resSkipNoMatch, r.kind)
}

func TestLooseLabelKey(t *testing.T) {
	for in, want := range map[string]string{
		"株式会社アリスソフト":      "アリスソフト",
		"アリスソフト 有限会社":     "アリスソフト",
		"(株)ういんどみる":       "ういんどみる",
		"key co.,ltd.":    "key",
		"key co., ltd":    "key",
		"abc inc.":        "abc",
		"visual art's":    "visualarts",
		"incredible soft": "incrediblesoft",
		"corpse party":    "corpseparty",
		"nitro+":          "nitro",
		"株式会社":            "",
		"x":               "",
		"ltd.":            "",
	} {
		assert.Equal(t, want, looseLabelKey(in), in)
	}
}

func TestBetter(t *testing.T) {
	assert.True(t, better(3, false, 5, 2, false, 4))
	assert.False(t, better(2, false, 5, 3, false, 4))
	assert.True(t, better(2, true, 5, 2, false, 4))
	assert.True(t, better(2, false, 3, 2, false, 4))
	assert.True(t, better(2, false, 9, 0, false, 0))
}

func TestEdgeKindFor(t *testing.T) {
	assert.Equal(t, model.WorkLabelKindBrand, edgeKindFor(model.LabelKindGameBrand))
	assert.Equal(t, model.WorkLabelKindCircle, edgeKindFor(model.LabelKindDoujinCircle))
	assert.Equal(t, model.WorkLabelKindPublisher, edgeKindFor(model.LabelKindPublisher))
	assert.Equal(t, model.WorkLabelKindCircle, edgeKindFor(model.LabelKindGroup))
}
