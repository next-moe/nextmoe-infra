// Package seed writes what a person settled before collection starts (plan 06
// §5): the publishers and labels of the pilot. A legacy id is the same
// organization by its official site or by the works credited to it.
package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/identity"
)

//go:embed orgs.json
var orgsJSON []byte

type Org struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Parent   string `json:"parent"`
	LegacyID int64  `json:"legacy_id"`
	Names    []struct {
		Text string `json:"text"`
		Lang string `json:"lang"`
	} `json:"names"`
}

func List() ([]Org, error) {
	var out []Org
	return out, json.Unmarshal(orgsJSON, &out)
}

// Orgs writes the seed once; run again it adds what the file gained. In a
// database without the legacy catalog the organizations get ids of their own.
func Orgs(ctx context.Context, client *ent.Client) (map[string]int64, error) {
	list, err := List()
	if err != nil {
		return nil, err
	}
	svc := commit.New(client)
	ids := map[string]int64{}
	for _, o := range list {
		values := map[string]commit.Value{"kind": raw(o.Kind)}
		if o.Parent != "" {
			parent, ok := ids[o.Parent]
			if !ok {
				return ids, fmt.Errorf("%s: parent %s comes later or is missing", o.Key, o.Parent)
			}
			values["parent_id"] = raw(parent)
		}
		adopt := o.LegacyID
		if adopt != 0 {
			if why, err := identity.Adoptable(ctx, client, "org", "", adopt); err != nil {
				return ids, err
			} else if why != "" {
				slog.Warn("seed: the organization gets an id of its own", "key", o.Key, "why", why)
				adopt = 0
			}
		}
		cs := commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "seed:orgs"}, Note: "organization seed " + o.Key, Key: "seed:org:" + o.Key,
			Changes: []commit.Change{{Ref: "org", Entity: "org", AdoptID: adopt, Values: values}}}
		for _, n := range o.Names {
			cs.Changes = append(cs.Changes, commit.Change{Entity: "org_name", Values: map[string]commit.Value{"org_id": raw("ref:org"), "text": raw(n.Text), "lang": raw(n.Lang)}})
		}
		res, err := svc.Apply(ctx, cs)
		if err != nil {
			return ids, fmt.Errorf("%s: %w", o.Key, err)
		}
		ids[o.Key] = res.IDs["org"]
	}
	return ids, nil
}

func raw(v any) commit.Value {
	b, _ := json.Marshal(v)
	return commit.Value{Raw: b}
}
