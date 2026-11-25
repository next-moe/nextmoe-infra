// Package safety judges whether a work's titles may show on a safe-for-work
// page. A work nobody judged is not safe, so a stalled run hides works rather
// than showing them.
package safety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/ent/titleverdict"
	"api/internal/canon/ent/work"
	"api/pkg/llmask"
)

const (
	promptVersion = "canon-title-v1"
	// safeConfidence is the bar of the legacy title gate: below it a "safe" is
	// the model recognising the book, not reading the name.
	safeConfidence = 0.8

	Safe, Suggestive, Explicit = "safe", "suggestive", "explicit"
)

const prompt = `You review the titles of books, comics and games before they appear on a public, search-indexed catalog page that must be safe for work. Every title listed for a work is shown on its page, so judge the WORST title of each work. Judge the words, not what you know of the work.

Classes:
- "safe": no sexual meaning. Romance, love, marriage, harems, girlfriends, violence, horror and dark themes are safe.
- "suggestive": sexual innuendo, fetish or adult-genre wording, or sexualised framing without naming explicit acts. Examples: 人妻, 寝取られ/NTR, 調教, 催眠 used sexually, 淫, 発情, 孕ませ, エロ, breasts as the point of the title, lewd puns.
- "explicit": names sexual acts, genitals, or pornographic terms outright.

Titles are in Japanese, Chinese, English and other languages; judge the meaning. A coined word with no sexual meaning is safe. confidence is your certainty from 0 to 1.

Reply with ONLY one JSON object, no prose and no code fences, with exactly one entry per input id:
{"items":[{"id":1,"class":"safe","confidence":0.97}]}

Works:
`

type set struct {
	work  int64
	hash  string
	names []string
}

type Stats struct{ Judged, Written, Failed int }

// Run judges the title sets with no verdict yet and brings every work's
// display_safety in step with its verdict. A person's choice stands.
func Run(ctx context.Context, client *ent.Client, llm *llmask.Client, limit int) (Stats, error) {
	var st Stats
	sets, err := load(ctx, client)
	if err != nil {
		return st, err
	}
	verdicts, err := known(ctx, client)
	if err != nil {
		return st, err
	}
	var todo []set
	queued := map[string]bool{}
	for _, s := range sets {
		if _, ok := verdicts[s.hash]; !ok && !queued[s.hash] {
			queued[s.hash] = true
			todo = append(todo, s)
		}
	}
	if limit > 0 && len(todo) > limit {
		todo = todo[:limit]
	}
	size := func(s set) int { return len([]rune(strings.Join(s.names, " "))) }
	for _, batch := range llmask.Pack(todo, size, 2500, 40) {
		rows, err := llmask.Bisect(ctx, batch, func(ctx context.Context, b []set) ([]*ent.TitleVerdict, error) {
			return ask(ctx, llm, b)
		}, func(s set) *ent.TitleVerdict {
			return &ent.TitleVerdict{ID: s.hash, Class: Explicit, Confidence: 1, Model: llm.Model(), PromptVersion: promptVersion}
		})
		if err != nil {
			st.Failed += len(batch)
			return st, err
		}
		st.Failed += len(batch) - len(rows)
		for _, v := range rows {
			if err := client.TitleVerdict.Create().SetID(v.ID).SetClass(v.Class).SetConfidence(v.Confidence).SetModel(v.Model).
				SetPromptVersion(v.PromptVersion).OnConflictColumns(titleverdict.FieldID).UpdateNewValues().Exec(ctx); err != nil {
				return st, err
			}
			verdicts[v.ID] = v
			st.Judged++
		}
	}
	for _, s := range sets {
		v, ok := verdicts[s.hash]
		want := "unjudged"
		if ok {
			want = classOf(v)
		}
		wrote, err := write(ctx, client, s.work, want)
		if err != nil {
			return st, fmt.Errorf("work %d: %w", s.work, err)
		}
		if wrote {
			st.Written++
		}
	}
	return st, nil
}

func classOf(v *ent.TitleVerdict) string {
	if v.Class == Safe && v.Confidence < safeConfidence {
		return Suggestive
	}
	return v.Class
}

func load(ctx context.Context, client *ent.Client) ([]set, error) {
	rows, err := client.QueryContext(ctx, `SELECT w.id, json_agg(DISTINCT t.text) FROM canon.work w
		JOIN canon.work_title t ON t.work_id = w.id GROUP BY w.id ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []set
	for rows.Next() {
		var s set
		var raw []byte
		if err := rows.Scan(&s.work, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &s.names); err != nil {
			return nil, err
		}
		slices.Sort(s.names)
		sum := sha256.Sum256([]byte(strings.Join(s.names, "\n")))
		s.hash = hex.EncodeToString(sum[:])
		out = append(out, s)
	}
	return out, rows.Err()
}

func known(ctx context.Context, client *ent.Client) (map[string]*ent.TitleVerdict, error) {
	rows, err := client.TitleVerdict.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*ent.TitleVerdict, len(rows))
	for _, v := range rows {
		out[v.ID] = v
	}
	return out, nil
}

func ask(ctx context.Context, llm *llmask.Client, batch []set) ([]*ent.TitleVerdict, error) {
	var b strings.Builder
	b.WriteString(prompt)
	for i, s := range batch {
		line, _ := json.Marshal(map[string]any{"id": i + 1, "titles": s.names})
		b.Write(line)
		b.WriteByte('\n')
	}
	var reply struct {
		Items []struct {
			ID         int     `json:"id"`
			Class      string  `json:"class"`
			Confidence float64 `json:"confidence"`
		} `json:"items"`
	}
	served, err := llm.Ask(ctx, b.String(), &reply)
	if err != nil {
		return nil, err
	}
	var out []*ent.TitleVerdict
	seen := map[int]bool{}
	for _, it := range reply.Items {
		class := strings.ToLower(strings.TrimSpace(it.Class))
		if it.ID < 1 || it.ID > len(batch) || seen[it.ID] || !slices.Contains([]string{Safe, Suggestive, Explicit}, class) {
			continue
		}
		seen[it.ID] = true
		out = append(out, &ent.TitleVerdict{ID: batch[it.ID-1].hash, Class: class, Confidence: it.Confidence,
			Model: served, PromptVersion: promptVersion, JudgedAt: time.Now()})
	}
	return out, nil
}

func write(ctx context.Context, client *ent.Client, id int64, want string) (bool, error) {
	w, err := client.Work.Query().Where(work.ID(id)).Only(ctx)
	if ent.IsNotFound(err) || err == nil && w.DisplaySafety == want {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st, ok := w.FieldState["display_safety"].(map[string]any); ok && st["by"] == string(commit.ActorHuman) {
		return false, nil
	}
	raw, _ := json.Marshal(want)
	_, err = commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "safety"}, Note: "title safety " + promptVersion,
		Changes: []commit.Change{{Entity: "work", ID: id, ExpectVersion: w.Version, Values: map[string]commit.Value{"display_safety": {Raw: raw}}}}})
	return err == nil, err
}
