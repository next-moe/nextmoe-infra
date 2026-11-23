package fanin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"api/internal/canon/contract"
	entodo "api/internal/canon/ent/todo"
	"api/internal/canon/registry"
)

// todoSubject decides which refusals are a person's question rather than a
// bad row (plan 07 §4); the subject key keeps one todo per question.
func todoSubject(rf *refusal, row contract.OutboxRow) (string, bool) {
	if rf.subject != "" {
		return rf.subject, true
	}
	switch rf.code {
	case "HUMAN_LOCKED":
		if c := row.Candidate; c != nil {
			return registry.TodoAgentOnHuman + ":" + c.Target.Entity + ":" + strconv.FormatInt(c.Target.ID, 10) + ":" + c.Property, true
		}
	}
	return "", false
}

func (f *Fanin) fileTodo(ctx context.Context, rf *refusal, subject string, e contract.OutboxEntry) error {
	var suggestion map[string]any
	b, _ := json.Marshal(e.Row)
	_ = json.Unmarshal(b, &suggestion)
	kind, _, _ := strings.Cut(subject, ":")
	err := f.Client.Todo.Create().SetKind(kind).SetSubjectKey(subject).SetDetail(rf.code + " " + rf.detail).
		SetSource("pool:" + f.Name + ":" + strconv.FormatInt(e.ID, 10)).SetSuggestion(suggestion).
		OnConflictColumns("subject_key").DoNothing().Exec(ctx)
	// ent reports DO NOTHING on an existing subject as sql.ErrNoRows: the
	// question is already filed, which is the point.
	if errors.Is(err, errNoRows) {
		return nil
	}
	return err
}

// answerSettled tells the pool how people settled todos its rows opened, by
// answering the same outbox id again; the pool treats a later answer as final.
func (f *Fanin) answerSettled(ctx context.Context) error {
	ts, err := f.Client.Todo.Query().Where(entodo.AnsweredEQ(false), entodo.StateIn("done", "rejected"),
		entodo.SourceHasPrefix("pool:"+f.Name+":")).All(ctx)
	if err != nil || len(ts) == 0 {
		return err
	}
	rows := make([]contract.InboxRow, 0, len(ts))
	for _, t := range ts {
		n, perr := strconv.ParseInt(strings.TrimPrefix(t.Source, "pool:"+f.Name+":"), 10, 64)
		if perr != nil {
			continue
		}
		r := contract.InboxRow{OutboxID: n, Result: "refused", Code: "REJECTED_IN_REVIEW"}
		if res, _ := t.Resolution["result"].(string); res == "written" {
			r = contract.InboxRow{OutboxID: n, Result: "written", IDs: map[string]int64{}}
			ids, _ := t.Resolution["ids"].(map[string]any)
			for k, v := range ids {
				if x, ok := v.(float64); ok {
					r.IDs[k] = int64(x)
				}
			}
			r.EntityID = r.IDs["work"]
		} else if d, _ := t.Resolution["detail"].(string); d != "" {
			r.Detail = d
		}
		rows = append(rows, r)
	}
	if err := f.Pool.Answer(ctx, rows); err != nil {
		return err
	}
	ids := make([]int, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	return f.Client.Todo.Update().Where(entodo.IDIn(ids...)).SetAnswered(true).Exec(ctx)
}
