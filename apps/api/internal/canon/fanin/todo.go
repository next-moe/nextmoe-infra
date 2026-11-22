package fanin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"api/internal/canon/contract"
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
			return "agent_on_human:" + c.Target.Entity + ":" + strconv.FormatInt(c.Target.ID, 10) + ":" + c.Property, true
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
