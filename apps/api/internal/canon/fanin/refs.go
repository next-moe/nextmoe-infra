package fanin

import (
	"context"
	"encoding/json"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/orgname"
)

// pushRefs tells the pool what canon calls the organizations, so lane A can
// cite a publisher or a label. The direction stays canon → pool (plan 05 §15);
// a held or taken-down one is left out.
func (f *Fanin) pushRefs(ctx context.Context) error {
	names, err := f.Client.OrgName.Query().Order(ent.Asc(orgname.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	held, err := f.heldOrgs(ctx)
	if err != nil {
		return err
	}
	batch := contract.RefBatch{Kind: "org", Items: []contract.RefItem{}}
	for _, n := range names {
		if !held[n.OrgID] {
			batch.Items = append(batch.Items, contract.RefItem{Name: n.Text, ID: n.OrgID})
		}
	}
	b, _ := json.Marshal(batch)
	if string(b) == f.pushed {
		return nil
	}
	if err := f.Pool.Refs(ctx, batch); err != nil {
		return err
	}
	f.pushed = string(b)
	return nil
}

func (f *Fanin) heldOrgs(ctx context.Context) (map[int64]bool, error) {
	rows, err := f.Client.QueryContext(ctx, `SELECT entity_id FROM canon.hold WHERE entity = 'org' AND released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
