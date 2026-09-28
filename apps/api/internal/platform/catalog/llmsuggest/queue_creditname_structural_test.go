package llmsuggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func structuralFacts() creditApplyFacts {
	return creditApplyFacts{SameName: true, ExclusiveName: true, Declared: true}
}

func TestStructuralCreditAcceptNeedsEveryCondition(t *testing.T) {
	for _, v := range []string{VerdictUnsure, VerdictSame, VerdictChainVerified} {
		assert.True(t, structuralCreditAccept(v, structuralFacts()), v)
	}
	assert.False(t, structuralCreditAccept(VerdictDifferent, structuralFacts()), "a judged-different pair waits for a person")

	for name, mutate := range map[string]func(*creditApplyFacts){
		"different spelling": func(f *creditApplyFacts) { f.SameName = false },
		"a third holder":     func(f *creditApplyFacts) { f.ExclusiveName = false },
		"not declared":       func(f *creditApplyFacts) { f.Declared = false },
		"both linked":        func(f *creditApplyFacts) { f.BothLinked = true },
		"company":            func(f *creditApplyFacts) { f.Company = true },
		"contested":          func(f *creditApplyFacts) { f.Contested = true },
		"placeholder":        func(f *creditApplyFacts) { f.Placeholder = true },
	} {
		f := structuralFacts()
		mutate(&f)
		assert.False(t, structuralCreditAccept(VerdictUnsure, f), name)
	}
}

func TestPlanForCreditNameStructuralOverridesOnlyNameGuards(t *testing.T) {
	opts := Options{MinConfidence: 0.9, MinConfidenceReject: 0.8}
	row := QueueVerdict{Lane: LaneGuard, Verdict: VerdictUnsure, Confidence: 0}
	plan := func(f creditApplyFacts) applyPlan {
		return planFor(QueueCreditName, row, nil, opts, false, applyEvidence{Credit: f})
	}

	for _, g := range []string{"", guardShortName, guardNoCareer} {
		f := structuralFacts()
		f.Guard = g
		p := plan(f)
		assert.Equal(t, applyAccept, p.Action, "guard %q", g)
		assert.Equal(t, structuralCreditReason, p.Reason, "guard %q", g)
	}

	company := structuralFacts()
	company.Guard, company.Company = guardShortName, true
	assert.Equal(t, skipHeldByGuard, plan(company).Skip, "a company hidden behind a short-name guard is still held")

	contested := structuralFacts()
	contested.Guard, contested.Contested = guardNoCareer, true
	assert.Equal(t, skipHeldByGuard, plan(contested).Skip, "a contested name hidden behind a no-career guard is still held")

	third := structuralFacts()
	third.ExclusiveName = false
	assert.Equal(t, skipUnsure, plan(third).Skip, "an unsure pair without the structure is not decided")
}

func TestPlaceholderCreditName(t *testing.T) {
	for _, n := range []string{"匿名希望", "？？？", "???", "774", "♪♪♪♪♪", "△○□×", "UNKNOWN", "Anonymous", "未定", "匿名"} {
		assert.True(t, placeholderCreditName(n), n)
	}
	for _, n := range []string{"ささきのぞみ", "5pb.", "9nine", "eco", "金田一", "六月十三", "FictionJunction"} {
		assert.False(t, placeholderCreditName(n), n)
	}
}

func TestCreditNameHoldersFoldSpacingAndWidth(t *testing.T) {
	h := creditNameHolders([]string{"井上 高宏", "井上高宏", "ＡＢＣ", "abc", "a・b・c", "別人"})
	assert.Equal(t, 2, h[foldCreditName("井上高宏")])
	assert.Equal(t, 3, h[foldCreditName("abc")], "width, case and middle dots fold to one key")
	assert.Equal(t, 1, h[foldCreditName("別人")])
}
