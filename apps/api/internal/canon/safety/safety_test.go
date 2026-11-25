package safety

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/pkg/llmask"
)

func newWork(t *testing.T, client *ent.Client, title string) int64 {
	t.Helper()
	raw := func(v any) commit.Value { b, _ := json.Marshal(v); return commit.Value{Raw: b} }
	res, err := commit.New(client).Apply(context.Background(), commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": raw("novel")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": raw("ref:w"), "text": raw(title), "lang": raw("ja"), "kind": raw("official")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return res.IDs["w"]
}

// judge classes a title by the marker the test put in it.
func judge(t *testing.T, calls *int) *llmask.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var items []map[string]any
		_, list, _ := strings.Cut(req.Messages[0].Content, "Works:\n")
		for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
			var in struct {
				ID     int      `json:"id"`
				Titles []string `json:"titles"`
			}
			if json.Unmarshal([]byte(line), &in) != nil {
				continue
			}
			class, conf := Safe, 0.99
			switch all := strings.Join(in.Titles, " "); {
			case strings.Contains(all, "露骨"):
				class = Explicit
			case strings.Contains(all, "曖昧"):
				conf = 0.6
			case strings.Contains(all, "無回答"):
				continue
			}
			items = append(items, map[string]any{"id": in.ID, "class": class, "confidence": conf})
		}
		reply, _ := json.Marshal(map[string]any{"items": items})
		out, _ := json.Marshal(map[string]any{"model": "judge-1", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": string(reply)}, "finish_reason": "stop"}}})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return llmask.New(srv.URL, "k", "scene")
}

func TestTitlesAreJudgedOnceAndAPersonsChoiceStands(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	calls := 0
	llm := judge(t, &calls)
	ids := map[string]int64{}
	for _, mark := range []string{"平和", "露骨", "曖昧", "無回答"} {
		ids[mark] = newWork(t, client, canontest.Title(mark+"の庭"))
	}
	want := func(step string, m map[string]string) {
		t.Helper()
		for mark, class := range m {
			if w, _ := client.Work.Get(ctx, ids[mark]); w.DisplaySafety != class {
				t.Errorf("%s: %s is %s, want %s", step, mark, w.DisplaySafety, class)
			}
		}
	}
	want("before", map[string]string{"平和": "unjudged"})
	if _, err := Run(ctx, client, llm, 0); err != nil {
		t.Fatal(err)
	}
	want("judged", map[string]string{"平和": Safe, "露骨": Explicit, "曖昧": Suggestive, "無回答": "unjudged"})

	raw, _ := json.Marshal(Safe)
	if _, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorHuman, Key: "user:1"}, Changes: []commit.Change{
		{Entity: "work", ID: ids["曖昧"], Values: map[string]commit.Value{"display_safety": {Raw: raw}}}}}); err != nil {
		t.Fatal(err)
	}
	title, _ := json.Marshal(canontest.Title("露骨な別名"))
	if _, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": {Raw: []byte(fmt.Sprint(ids["平和"]))}, "text": {Raw: title}, "lang": {Raw: []byte(`"ja"`)}, "kind": {Raw: []byte(`"alternative"`)}}}}}); err != nil {
		t.Fatal(err)
	}
	st, err := Run(ctx, client, llm, 0)
	if err != nil {
		t.Fatal(err)
	}
	want("a new title", map[string]string{"平和": Explicit, "曖昧": Safe})
	if st.Judged != 1 {
		t.Errorf("judged %d sets again, want the changed one", st.Judged)
	}
}
