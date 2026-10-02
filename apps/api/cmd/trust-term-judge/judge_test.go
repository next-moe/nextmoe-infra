package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/llmsuggest"
)

type scriptedLLM struct {
	answers []llmsuggest.ChatResult
	errs    []error
	prompts []string
}

func (s *scriptedLLM) ChatJSON(_ context.Context, _, user, _ string, _ map[string]any, _ int) (llmsuggest.ChatResult, error) {
	i := len(s.prompts)
	s.prompts = append(s.prompts, user)
	if i >= len(s.answers) {
		return llmsuggest.ChatResult{}, fmt.Errorf("unscripted call %d", i)
	}
	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}
	return s.answers[i], err
}

func newJudge(llm chatter, batch int) *judge {
	return &judge{llm: llm, batch: batch, pause: func(int) {}}
}

func answer(content string) llmsuggest.ChatResult {
	return llmsuggest.ChatResult{Content: content, FinishReason: "stop"}
}

var threeTerms = []candidate{{ID: 11, Term: "第一次"}, {ID: 12, Term: "外送茶"}, {ID: 13, Term: "服务器"}}

func TestJudgeBatch_OnlyOrdinaryRetiresAndEveryTermKeepsItsCategory(t *testing.T) {
	llm := &scriptedLLM{answers: []llmsuggest.ChatResult{
		answer(`{"terms":[{"term":"服务器","category":"ordinary"},{"term":"第一次","category":"ordinary"},{"term":"外送茶","category":"solicitation"}]}`),
	}}
	got, err := newJudge(llm, 60).judgeBatch(context.Background(), threeTerms)
	if err != nil {
		t.Fatalf("judgeBatch: %v", err)
	}
	want := []verdict{
		{ID: 11, Term: "第一次", Verdict: verdictRetire, Category: "ordinary"},
		{ID: 12, Term: "外送茶", Verdict: verdictKeep, Category: "solicitation"},
		{ID: 13, Term: "服务器", Verdict: verdictRetire, Category: "ordinary"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("verdict %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if llm.prompts[0] != "第一次\n外送茶\n服务器\n" {
		t.Fatalf("prompt = %q", llm.prompts[0])
	}
}

const allUnknown = `{"terms":[{"term":"第一次","category":"unknown"},{"term":"外送茶","category":"unknown"},{"term":"服务器","category":"unknown"}]}`

func TestJudgeBatch_RetriesAnAnswerThatDoesNotCoverTheBatchExactly(t *testing.T) {
	for name, bad := range map[string]string{
		"not in the batch": `{"terms":[{"term":"第一次","category":"ordinary"},{"term":"外送茶","category":"ordinary"},{"term":"伺服器","category":"ordinary"}]}`,
		"skips a term":     `{"terms":[{"term":"第一次","category":"ordinary"},{"term":"服务器","category":"ordinary"}]}`,
		"named twice":      `{"terms":[{"term":"第一次","category":"ordinary"},{"term":"第一次","category":"insult"},{"term":"外送茶","category":"ordinary"},{"term":"服务器","category":"ordinary"}]}`,
		"no such category": `{"terms":[{"term":"第一次","category":"fine"},{"term":"外送茶","category":"ordinary"},{"term":"服务器","category":"ordinary"}]}`,
		"not json":         `retire 第一次`,
	} {
		llm := &scriptedLLM{answers: []llmsuggest.ChatResult{answer(bad), answer(allUnknown)}}
		got, err := newJudge(llm, 60).judgeBatch(context.Background(), threeTerms)
		if err != nil || len(llm.prompts) != 2 {
			t.Fatalf("%s: err %v after %d calls, want a clean second try", name, err, len(llm.prompts))
		}
		for _, v := range got {
			if v.Verdict != verdictKeep {
				t.Fatalf("%s: the rejected answer leaked into the verdicts: %+v", name, got)
			}
		}
	}
}

func TestJudgeBatch_AGatewayThatNeverAnswersStopsTheRun(t *testing.T) {
	llm := &scriptedLLM{
		answers: make([]llmsuggest.ChatResult, judgeAttempts),
		errs:    []error{errors.New("429"), errors.New("429"), errors.New("429"), errors.New("429"), errors.New("429")},
	}
	if _, err := newJudge(llm, 60).judgeBatch(context.Background(), threeTerms); err == nil {
		t.Fatal("five failed calls produced verdicts")
	}
	if len(llm.prompts) != judgeAttempts {
		t.Fatalf("%d calls, want %d and no narrowing: a dead gateway is not the batch's fault", len(llm.prompts), judgeAttempts)
	}
}

func TestJudgeBatch_ATermTheModelCannotEchoIsKeptAndTheRestAreJudged(t *testing.T) {
	terms := []candidate{{ID: 21, Term: `私\\服`}, {ID: 22, Term: "总人数"}}
	halved := answer(`{"terms":[{"term":"私\\服","category":"commerce"},{"term":"总人数","category":"ordinary"}]}`)
	aloneHalved := answer(`{"terms":[{"term":"私\\服","category":"commerce"}]}`)
	llm := &scriptedLLM{answers: []llmsuggest.ChatResult{
		halved, halved, halved, halved, halved,
		aloneHalved, aloneHalved, aloneHalved, aloneHalved, aloneHalved,
		answer(`{"terms":[{"term":"总人数","category":"ordinary"}]}`),
	}}
	got, err := newJudge(llm, 60).judgeBatch(context.Background(), terms)
	if err != nil {
		t.Fatalf("judgeBatch: %v", err)
	}
	want := []verdict{
		{ID: 21, Term: `私\\服`, Verdict: verdictKeep, Category: categoryUnjudged},
		{ID: 22, Term: "总人数", Verdict: verdictRetire, Category: categoryOrdinary},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("verdicts = %+v, want %+v", got, want)
	}
}

func TestJudgeBatch_ATruncatedAnswerIsAskedAgainInHalves(t *testing.T) {
	llm := &scriptedLLM{answers: []llmsuggest.ChatResult{
		{Content: `{"terms":[{"term":"第一次","cat`, FinishReason: "length"},
		answer(`{"terms":[{"term":"第一次","category":"ordinary"}]}`),
		answer(`{"terms":[{"term":"外送茶","category":"solicitation"},{"term":"服务器","category":"ordinary"}]}`),
	}}
	got, err := newJudge(llm, 60).judgeBatch(context.Background(), threeTerms)
	if err != nil {
		t.Fatalf("judgeBatch: %v", err)
	}
	if len(got) != 3 || got[0].Verdict != verdictRetire || got[1].Verdict != verdictKeep || got[2].Verdict != verdictRetire {
		t.Fatalf("verdicts after the split = %+v", got)
	}
	if llm.prompts[1] != "第一次\n" || llm.prompts[2] != "外送茶\n服务器\n" {
		t.Fatalf("halves = %q", llm.prompts[1:])
	}
}

func TestRun_ResumesWithoutJudgingATermTwice(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.jsonl"), filepath.Join(dir, "out.jsonl")
	if err := os.WriteFile(in, []byte(`{"id":11,"term":"第一次"}
{"id":12,"term":"外送茶"}
{"id":13,"term":"服务器"}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	first := &scriptedLLM{
		answers: make([]llmsuggest.ChatResult, 1+judgeAttempts),
		errs:    []error{nil, errors.New("down"), errors.New("down"), errors.New("down"), errors.New("down"), errors.New("down")},
	}
	first.answers[0] = answer(`{"terms":[{"term":"第一次","category":"ordinary"},{"term":"外送茶","category":"solicitation"}]}`)
	if err := newJudge(first, 2).run(context.Background(), in, out); err == nil {
		t.Fatal("a batch that never answered must stop the run")
	}

	second := &scriptedLLM{answers: []llmsuggest.ChatResult{answer(`{"terms":[{"term":"服务器","category":"unknown"}]}`)}}
	if err := newJudge(second, 2).run(context.Background(), in, out); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(second.prompts) != 1 || second.prompts[0] != "服务器\n" {
		t.Fatalf("the resumed run asked %q, want only the unjudged term", second.prompts)
	}
	got, err := readJSONL[verdict](out)
	if err != nil || len(got) != 3 {
		t.Fatalf("verdict file holds %d rows (err %v), want one per term", len(got), err)
	}
	if got[0].Verdict != verdictRetire || got[1].Category != "solicitation" || got[2].ID != 13 || got[2].Verdict != verdictKeep {
		t.Fatalf("verdict file = %+v", got)
	}
}
