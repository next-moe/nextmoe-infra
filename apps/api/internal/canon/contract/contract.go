// Package contract builds the ingest contract (plan 05 §16) from the registry
// annotations on the canon ent schema. contract.json is its committed output;
// the ingest repository syncs that file and never edits it.
package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"api/internal/canon/ent/schema"
	"api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"github.com/danielgtaylor/huma/v2"
)

const Version = "0.2.0"

// internalTables are canon's own bookkeeping; ingest never writes them.
var internalTables = map[string]bool{
	"revision": true, "evidence_snapshot": true, "evidence_record": true, "commit_key": true, "fanin_cursor": true,
	"todo": true, "hold": true, "takedown": true, "xref_link": true,
	"redirect": true, "merge_record": true,
	"library_part_progress": true, "library_tombstone": true,
	"identity_key": true, "program_state": true,
}

type Property struct {
	Key      string `json:"key"`
	Optional bool   `json:"optional"`
	registry.Property
}

type Entity struct {
	Name       string     `json:"name"`
	Properties []Property `json:"properties"`
}

type Contract struct {
	Version      string                  `json:"version"`
	Entities     []Entity                `json:"entities"`
	Vocabularies []registry.Vocabulary   `json:"vocabularies"`
	RefusalCodes []registry.Code         `json:"refusal_codes"`
	Sources      []registry.Source       `json:"sources"`
	Programs     []registry.Program      `json:"programs"`
	Pool         map[string]*huma.Schema `json:"pool"`
	PoolAPI      map[string]string       `json:"pool_api"`
	Defs         map[string]*huma.Schema `json:"$defs"`
}

func Build() (Contract, error) {
	c := Contract{Version: Version, Vocabularies: registry.Vocabularies, RefusalCodes: registry.RefusalCodes}
	for _, s := range schema.Entities {
		e, err := entity(s)
		if err != nil {
			return c, err
		}
		if !internalTables[e.Name] {
			c.Entities = append(c.Entities, e)
		}
	}
	sort.Slice(c.Entities, func(i, j int) bool { return c.Entities[i].Name < c.Entities[j].Name })

	reg := huma.NewMapRegistry("#/$defs/", huma.DefaultSchemaNamer)
	c.Pool = map[string]*huma.Schema{
		"outbox":      reg.Schema(reflect.TypeFor[OutboxRow](), true, ""),
		"inbox":       reg.Schema(reflect.TypeFor[InboxRow](), true, ""),
		"outbox_page": reg.Schema(reflect.TypeFor[OutboxPage](), true, ""),
		"snapshot":    reg.Schema(reflect.TypeFor[Snapshot](), true, ""),
		"record":      reg.Schema(reflect.TypeFor[Record](), true, ""),
		"inbox_batch": reg.Schema(reflect.TypeFor[InboxBatch](), true, ""),
		"block_batch": reg.Schema(reflect.TypeFor[BlockBatch](), true, ""),
		"ref_batch":   reg.Schema(reflect.TypeFor[RefBatch](), true, ""),
	}
	c.PoolAPI = map[string]string{
		"outbox":   "GET " + PathOutbox + "?after=<id>&limit=<n>",
		"snapshot": "GET " + PathSnapshot,
		"record":   "GET " + PathRecord,
		"inbox":    "POST " + PathInbox,
		"block":    "POST " + PathBlock,
		"refs":     "POST " + PathRefs,
		"auth":     "Authorization: Bearer <pool token>",
	}
	c.Sources = registry.Sources
	c.Programs = registry.Programs
	c.Defs = reg.Map()
	pinEnums(c.Defs)
	return c, nil
}

func entity(s ent.Interface) (Entity, error) {
	e := Entity{Name: tableName(s)}
	if e.Name == "" {
		return e, fmt.Errorf("%T has no entsql.Table annotation", s)
	}
	for _, f := range s.Fields() {
		d := f.Descriptor()
		for _, a := range d.Annotations {
			p, ok := a.(registry.Property)
			if !ok || !p.Agent {
				continue
			}
			e.Properties = append(e.Properties, Property{Key: d.Name, Optional: d.Optional, Property: p})
		}
	}
	return e, nil
}

func tableName(s ent.Interface) string {
	for _, a := range s.Annotations() {
		switch t := a.(type) {
		case entsql.Annotation:
			if t.Table != "" {
				return t.Table
			}
		case *entsql.Annotation:
			if t.Table != "" {
				return t.Table
			}
		}
	}
	return ""
}

func pinEnums(defs map[string]*huma.Schema) {
	var tiers, codes []any
	for _, t := range registry.FactTiers {
		tiers = append(tiers, string(t))
	}
	for _, c := range registry.RefusalCodes {
		codes = append(codes, c.Code)
	}
	if s := defs["Candidate"]; s != nil {
		s.Properties["tier"].Enum = tiers
	}
	if s := defs["InboxRow"]; s != nil {
		s.Properties["code"].Enum = codes
	}
	if s := defs["OutboxRow"]; s != nil {
		s.Properties["contract_version"].Enum = []any{Version}
	}
}

func Marshal(c Contract) ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Index maps entity → property key → declaration, internal properties
// included; the commit service validates every write against it.
func Index() map[string]map[string]registry.Property {
	out := map[string]map[string]registry.Property{}
	for _, s := range schema.Entities {
		props := map[string]registry.Property{}
		for _, f := range s.Fields() {
			for _, a := range f.Descriptor().Annotations {
				if p, ok := a.(registry.Property); ok {
					props[f.Descriptor().Name] = p
				}
			}
		}
		out[tableName(s)] = props
	}
	return out
}
