package fanin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
	enttodo "api/internal/canon/ent/todo"
)

// group is the question a row asks: one lead's changeset or one field's value.
func (f *Fanin) group(row contract.OutboxRow) string {
	if c := row.Candidate; c != nil {
		return "value:" + c.Target.Entity + ":" + strconv.FormatInt(c.Target.ID, 10) + ":" + c.Property
	}
	return "lead:" + f.Name + ":" + row.Changeset.LeadID
}

// fingerprint is a row's content without what varies between two tellings of
// it, so a rebuilt changeset is the question already answered.
func fingerprint(row contract.OutboxRow) string {
	row.LookupID, row.ContractVersion = "", ""
	b, _ := json.Marshal(row)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func (f *Fanin) subject(row contract.OutboxRow) string { return f.group(row) + ":" + fingerprint(row) }

// alreadySettled is the answer a reviewer gave this very content before.
func (f *Fanin) alreadySettled(ctx context.Context, row contract.OutboxRow) (contract.InboxRow, bool, error) {
	t, err := f.Client.Todo.Query().Where(enttodo.SubjectKey(f.subject(row)), enttodo.StateIn("done", "rejected")).Only(ctx)
	if ent.IsNotFound(err) {
		return contract.InboxRow{}, false, nil
	}
	if err != nil {
		return contract.InboxRow{}, false, err
	}
	return settledAnswer(t), true, nil
}

// file puts a verified row in the review queue. The same content is one todo
// however often it arrives; newer content for the same group replaces the
// open one.
func (f *Fanin) file(ctx context.Context, kind string, rf *refusal, e contract.OutboxEntry, it *item) (contract.InboxRow, error) {
	ans := contract.InboxRow{OutboxID: e.ID, Result: "todo", Code: rf.code, Detail: rf.detail}
	group, subject := f.group(e.Row), f.subject(e.Row)
	was, err := f.Client.Todo.Query().Where(enttodo.SubjectKey(subject)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return ans, err
	}
	if err := f.Client.Todo.Update().Where(enttodo.GroupKey(group), enttodo.State("open"), enttodo.SubjectKeyNEQ(subject)).
		SetState("superseded").SetAnswered(true).Exec(ctx); err != nil {
		return ans, err
	}
	waiting := []string{f.key(e.ID)}
	if was != nil {
		waiting = was.Waiting
		if !slices.Contains(waiting, f.key(e.ID)) {
			waiting = append(slices.Clone(waiting), f.key(e.ID))
		}
	}
	questions := []map[string]any{}
	if err := convert(it.questions, &questions); err != nil {
		return ans, err
	}
	var suggestion, changeset map[string]any
	if err := convert(e.Row, &suggestion); err != nil {
		return ans, err
	}
	if err := convert(it.cs, &changeset); err != nil {
		return ans, err
	}
	detail := rf.code + " " + rf.detail
	if was != nil {
		return ans, f.Client.Todo.UpdateOneID(was.ID).SetState("open").SetKind(kind).SetDetail(detail).SetSource(f.key(e.ID)).
			SetWaiting(waiting).SetSuggestion(suggestion).SetChangeset(changeset).SetQuestions(questions).SetAnswered(false).Exec(ctx)
	}
	return ans, f.Client.Todo.Create().SetKind(kind).SetSubjectKey(subject).SetGroupKey(group).SetProgram(e.Row.Program).
		SetDetail(detail).SetSource(f.key(e.ID)).SetWaiting(waiting).
		SetSuggestion(suggestion).SetChangeset(changeset).SetQuestions(questions).Exec(ctx)
}

// answerSettled tells the pool how the todos its rows opened were settled, by
// answering the same outbox ids again; the pool treats a later answer as final.
func (f *Fanin) answerSettled(ctx context.Context) error {
	ts, err := f.Client.Todo.Query().Where(enttodo.AnsweredEQ(false), enttodo.StateIn("done", "rejected"),
		enttodo.SourceHasPrefix("pool:"+f.Name+":")).All(ctx)
	if err != nil || len(ts) == 0 {
		return err
	}
	var rows []contract.InboxRow
	ids := make([]int, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
		waiting := t.Waiting
		if len(waiting) == 0 {
			waiting = []string{t.Source}
		}
		for _, w := range waiting {
			n, perr := strconv.ParseInt(strings.TrimPrefix(w, "pool:"+f.Name+":"), 10, 64)
			if perr != nil {
				continue
			}
			r := settledAnswer(t)
			r.OutboxID = n
			rows = append(rows, r)
		}
	}
	if err := f.Pool.Answer(ctx, rows); err != nil {
		return err
	}
	return f.Client.Todo.Update().Where(enttodo.IDIn(ids...)).SetAnswered(true).Exec(ctx)
}

func settledAnswer(t *ent.Todo) contract.InboxRow {
	r := contract.InboxRow{Result: "refused", Code: "REJECTED_IN_REVIEW"}
	if res, _ := t.Resolution["result"].(string); res != "written" {
		r.Detail, _ = t.Resolution["detail"].(string)
		return r
	}
	r = contract.InboxRow{Result: "written", IDs: map[string]int64{}}
	ids, _ := t.Resolution["ids"].(map[string]any)
	for k, v := range ids {
		if x, ok := v.(float64); ok {
			r.IDs[k] = int64(x)
		}
	}
	r.EntityID = r.IDs["work"]
	return r
}

func convert(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}
