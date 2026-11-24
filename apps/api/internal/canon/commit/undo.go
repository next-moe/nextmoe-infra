package commit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/ent/commitkey"
	"api/internal/canon/ent/revision"
	enttodo "api/internal/canon/ent/todo"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
	"api/internal/canon/verify"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	actionUndo   = "undo"
	actionRemove = "remove"
)

// Conflicts is why an undo is refused: what happened since that it would
// silently take back too.
type Conflicts []string

func (c Conflicts) Error() string { return "cannot undo: " + strings.Join(c, "; ") }

var ErrNothingToUndo = errors.New("no change to undo: the commit does not exist or was undone already")

// Undo takes back every change of one commit in one transaction (plan 07 §2):
// updated fields return to what they were, created rows go away and their ids
// stay retired. Anything written, referenced or collected by users since is a
// conflict, and then nothing changes. A person's undo of an agent's write locks
// the fields it restores, so the agent may only propose them again (07 §3).
func (s *Service) Undo(ctx context.Context, batch string, by Actor, note string) error {
	if batch == "" {
		return ErrNothingToUndo
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	revs, err := tx.Revision.Query().Where(revision.Batch(batch), revision.ActionIn("create", "update", actionRemove), revision.Undoes(0)).Order(ent.Desc(revision.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	if len(revs) == 0 {
		return ErrNothingToUndo
	}
	if done, err := undone(ctx, tx, revs[0].ID); err != nil {
		return err
	} else if done {
		return ErrNothingToUndo
	}
	u := undoer{s: s, tx: tx, by: by, note: note, batch: batch}
	var conflicts Conflicts
	for _, r := range revs {
		var err error
		switch r.Action {
		case "create":
			err = u.remove(ctx, r)
		case actionRemove:
			err = u.putBack(ctx, r)
		default:
			err = u.restore(ctx, r)
		}
		var c Conflicts
		if errors.As(err, &c) {
			conflicts = append(conflicts, c...)
			break
		}
		if err != nil {
			return err
		}
	}
	if len(conflicts) > 0 {
		return conflicts
	}
	if err := u.unsettle(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if by.Kind == ActorHuman && revs[0].ActorKind == string(ActorAgent) {
		return s.autoStop(ctx, revs[0].ActorKey)
	}
	return nil
}

// Rollback undoes a program's commits since a time, newest first, each on its
// own. The operator is correcting a bad run, not asserting values, so nothing
// is locked and a corrected run may write again.
func (s *Service) Rollback(ctx context.Context, actorKey string, since time.Time, by, note string) (undoneBatches []string, conflicts map[string]error, err error) {
	revs, err := s.client.Revision.Query().Where(revision.ActorKey(actorKey), revision.CreatedAtGTE(since), revision.BatchNEQ(""),
		revision.ActionIn("create", "update")).Order(ent.Desc(revision.FieldID)).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	conflicts = map[string]error{}
	seen := map[string]bool{}
	for _, r := range revs {
		if seen[r.Batch] {
			continue
		}
		seen[r.Batch] = true
		switch err := s.Undo(ctx, r.Batch, Actor{Kind: ActorSystem, Key: by}, note); {
		case err == nil:
			undoneBatches = append(undoneBatches, r.Batch)
		case errors.Is(err, ErrNothingToUndo):
		default:
			var c Conflicts
			if !errors.As(err, &c) {
				return undoneBatches, conflicts, err
			}
			conflicts[r.Batch] = c
		}
	}
	return undoneBatches, conflicts, nil
}

type undoer struct {
	s           *Service
	tx          *ent.Tx
	by          Actor
	note, batch string
}

func (u *undoer) restore(ctx context.Context, r *ent.Revision) error {
	row, found, err := rowOf(ctx, u.tx, r.Entity, r.EntityID)
	if err != nil {
		return err
	}
	if !found {
		return Conflicts{fmt.Sprintf("%s %d no longer exists", r.Entity, r.EntityID)}
	}
	later, err := u.touchedSince(ctx, r)
	if err != nil {
		return err
	}
	state, _ := row["field_state"].(map[string]any)
	m := builders[r.Entity].update(u.tx.Client(), r.EntityID)
	var conflicts Conflicts
	changes := map[string]any{}
	for _, key := range changedKeys(r) {
		c, _ := r.Changes[key].(map[string]any)
		if later[key] || !sameValue(row[key], c["value"]) {
			conflicts = append(conflicts, fmt.Sprintf("%s %d %s was written again since", r.Entity, r.EntityID, key))
			continue
		}
		if c["prior"] == nil {
			if err := m.ClearField(key); err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s %d %s cannot be emptied: %v", r.Entity, r.EntityID, key, err))
				continue
			}
		} else if val, err := restored(u.s.index[r.Entity][key], c["prior"]); err != nil {
			return err
		} else if err := m.SetField(key, val); err != nil {
			return err
		}
		delete(state, key)
		if was, ok := c["prior_state"].(map[string]any); ok {
			state[key] = was
		}
		// Only an agent's value is locked out: undoing a person's own edit would
		// otherwise lock a field nobody meant to decide.
		if u.by.Kind == ActorHuman && r.ActorKind == string(ActorAgent) {
			state[key] = map[string]any{"by": string(ActorHuman), "key": u.by.Key, "at": u.s.now().UTC().Format(time.RFC3339), "undid": r.ID}
		}
		changes[key] = map[string]any{"value": c["prior"], "prior": c["value"]}
	}
	if len(conflicts) > 0 {
		return conflicts
	}
	if err := m.SetField("field_state", state); err != nil {
		return err
	}
	if err := m.SetField("version", intOf(row["version"])+1); err != nil {
		return err
	}
	saved, err := u.tx.Client().Mutate(ctx, m)
	if err != nil {
		return err
	}
	if j, ok := judged[r.Entity]; ok {
		root, id := j.root, r.EntityID
		if root != r.Entity {
			_, id = rootOf(r.Entity, r.EntityID, saved)
		}
		if err := identity.Rekey(ctx, u.tx, root, id); err != nil {
			return err
		}
	}
	return u.record(ctx, r, actionUndo, changes)
}

// remove deletes a row the commit created. Users who already kept the work,
// another commit that points at it, or a later edit all mean it must be merged
// or taken down instead.
func (u *undoer) remove(ctx context.Context, r *ent.Revision) error {
	row, found, err := rowOf(ctx, u.tx, r.Entity, r.EntityID)
	if err != nil {
		return err
	}
	if !found {
		return Conflicts{fmt.Sprintf("%s %d no longer exists", r.Entity, r.EntityID)}
	}
	if later, err := u.touchedSince(ctx, r); err != nil {
		return err
	} else if len(later) > 0 {
		return Conflicts{fmt.Sprintf("%s %d was edited since", r.Entity, r.EntityID)}
	}
	if r.Entity == "work" {
		if kept, err := keptByUsers(ctx, u.tx, r.EntityID); err != nil {
			return err
		} else if kept != "" {
			return Conflicts{fmt.Sprintf("work %d is in %s", r.EntityID, kept)}
		}
	}
	if _, err := u.tx.ExecContext(ctx, `DELETE FROM canon.`+r.Entity+` WHERE id = $1`, r.EntityID); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23503" {
			return Conflicts{fmt.Sprintf("%s %d is referenced by %s", r.Entity, r.EntityID, pg.TableName)}
		}
		return err
	}
	if err := identity.Forget(ctx, u.tx, r.Entity, r.EntityID); err != nil {
		return err
	}
	if j, ok := judged[r.Entity]; ok && j.root != r.Entity && r.RootID != r.EntityID {
		if err := identity.Rekey(ctx, u.tx, j.root, r.RootID); err != nil {
			return err
		}
	}
	return u.record(ctx, r, actionRemove, map[string]any{"row": row})
}

// putBack returns a row a person removed, under its own id.
func (u *undoer) putBack(ctx context.Context, r *ent.Revision) error {
	row, err := json.Marshal(r.Changes["row"])
	if err != nil {
		return err
	}
	if _, err := u.tx.ExecContext(ctx, `INSERT INTO canon.`+r.Entity+` SELECT * FROM jsonb_populate_record(NULL::canon.`+r.Entity+`, $1)`, row); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && strings.HasPrefix(pg.Code, "23") {
			return Conflicts{fmt.Sprintf("%s %d cannot come back: %s", r.Entity, r.EntityID, pg.Message)}
		}
		return err
	}
	if err := identity.Rekey(ctx, u.tx, r.RootEntity, r.RootID); err != nil {
		return err
	}
	return u.record(ctx, r, actionUndo, map[string]any{})
}

func (u *undoer) record(ctx context.Context, r *ent.Revision, action string, changes map[string]any) error {
	return u.tx.Revision.Create().SetEntity(r.Entity).SetEntityID(r.EntityID).SetAction(action).
		SetActorKind(string(u.by.Kind)).SetActorKey(u.by.Key).SetNote(u.note).SetChanges(changes).SetUndoes(r.ID).
		SetRootEntity(r.RootEntity).SetRootID(r.RootID).Exec(ctx)
}

// touchedSince is the fields later commits wrote, leaving out this commit's
// own revisions and those already undone.
func (u *undoer) touchedSince(ctx context.Context, r *ent.Revision) (map[string]bool, error) {
	later, err := u.tx.Revision.Query().Where(revision.Entity(r.Entity), revision.EntityID(r.EntityID), revision.IDGT(r.ID),
		revision.Action("update"), revision.BatchNEQ(u.batch)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range later {
		if done, err := undone(ctx, u.tx, l.ID); err != nil {
			return nil, err
		} else if done {
			continue
		}
		for _, k := range changedKeys(l) {
			out[k] = true
		}
	}
	return out, nil
}

// unsettle lets the pool hear of it: the commit's key is forgotten, and the
// todo that wrote it is rejected, so fanin answers its rows again.
func (u *undoer) unsettle(ctx context.Context) error {
	if _, err := u.tx.CommitKey.Delete().Where(commitkey.Batch(u.batch)).Exec(ctx); err != nil {
		return err
	}
	ts, err := u.tx.Todo.Query().Where(enttodo.State("done")).All(ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if b, _ := t.Resolution["batch"].(string); b != u.batch {
			continue
		}
		if err := u.tx.Todo.UpdateOneID(t.ID).SetState("rejected").SetAnswered(false).
			SetResolution(map[string]any{"result": "refused", "code": "REJECTED_IN_REVIEW", "detail": "undone by " + u.by.Key + ": " + u.note}).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

const (
	stopWindow  = 500
	stopMinimum = 20
	stopRate    = 0.02
)

// autoStop stops a program once people take back more than 2% of its recent
// commits (plan 05 §9); a person starts it again.
func (s *Service) autoStop(ctx context.Context, actorKey string) error {
	rows, err := s.client.QueryContext(ctx, `WITH recent AS (
			SELECT batch, max(id) AS last FROM canon.revision WHERE actor_kind = 'agent' AND actor_key = $1 AND batch <> ''
			GROUP BY batch ORDER BY last DESC LIMIT $2)
		SELECT count(*), count(*) FILTER (WHERE EXISTS (SELECT 1 FROM canon.revision u JOIN canon.revision r ON u.undoes = r.id
			WHERE r.batch = recent.batch AND u.actor_kind = 'human' AND u.action IN ('undo', 'remove'))) FROM recent`, actorKey, stopWindow)
	if err != nil {
		return err
	}
	var total, taken int
	if rows.Next() {
		err = rows.Scan(&total, &taken)
	}
	rows.Close()
	if err != nil {
		return err
	}
	program, ok := strings.CutPrefix(actorKey, "program:")
	if !ok || taken < 2 || total < stopMinimum || float64(taken)/float64(total) <= stopRate {
		return nil
	}
	return s.client.ProgramState.Create().SetID(program).SetMode(registry.ModeStopped).SetSetBy("system:auto-stop").
		SetNote(fmt.Sprintf("people undid %d of its last %d commits", taken, total)).
		OnConflictColumns("id").UpdateNewValues().Exec(ctx)
}

func undone(ctx context.Context, tx *ent.Tx, rev int64) (bool, error) {
	return exists(ctx, tx, `SELECT EXISTS (SELECT 1 FROM canon.revision WHERE undoes = $1)`, rev)
}

func exists(ctx context.Context, tx *ent.Tx, query string, args ...any) (bool, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var ok bool
	if rows.Next() {
		if err := rows.Scan(&ok); err != nil {
			return false, err
		}
	}
	return ok, rows.Err()
}

// keptByUsers names the first user-state table holding a work (10 §3); these
// live with the legacy catalog and carry no foreign key to canon.
func keptByUsers(ctx context.Context, tx *ent.Tx, work int64) (string, error) {
	for _, t := range []string{"catalog_user_folder_item", "catalog_user_work_state", "catalog_user_work_rating", "catalog_user_work_view"} {
		present, err := exists(ctx, tx, `SELECT to_regclass($1) IS NOT NULL`, "public."+t)
		if err != nil {
			return "", err
		}
		if !present {
			continue
		}
		kept, err := exists(ctx, tx, `SELECT EXISTS (SELECT 1 FROM public.`+t+` WHERE work_id = $1)`, work)
		if err != nil {
			return "", err
		}
		if kept {
			return t, nil
		}
	}
	return "", nil
}

func changedKeys(r *ent.Revision) []string {
	keys := make([]string, 0, len(r.Changes))
	for k, v := range r.Changes {
		if _, ok := v.(map[string]any); ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// restored turns a stored value back into the Go type its column takes.
func restored(p registry.Property, stored any) (any, error) {
	b, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	if p.Shape == registry.ShapeMoney {
		b = []byte(strings.Trim(string(b), `"`))
	}
	val, verr := verify.Value(p, b)
	if verr != nil {
		return nil, verr
	}
	return val, nil
}
