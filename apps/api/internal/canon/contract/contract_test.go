package contract

import (
	"bytes"
	"flag"
	"os"
	"testing"

	"api/internal/canon/ent/migrate"
	"api/internal/canon/ent/schema"
	"api/internal/canon/registry"
)

var update = flag.Bool("update", false, "rewrite contract.json")

func TestContractIsCurrent(t *testing.T) {
	c, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("contract.json", got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("contract.json is stale; run go test ./internal/canon/contract -run TestContractIsCurrent -update, and bump Version when a published field changes")
	}
}

func TestEveryEntSchemaIsListed(t *testing.T) {
	listed := map[string]bool{}
	for _, s := range schema.Entities {
		listed[tableName(s)] = true
	}
	for _, tbl := range migrate.Tables {
		if !listed[tbl.Name] {
			t.Errorf("table %s has an ent schema missing from schema.Entities", tbl.Name)
		}
	}
}

func TestEveryPropertyIsWellFormed(t *testing.T) {
	c, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	entities := map[string]bool{}
	for _, e := range c.Entities {
		entities[e.Name] = true
	}
	for _, e := range c.Entities {
		for _, p := range e.Properties {
			switch p.Shape {
			case registry.ShapeVocab:
				if _, ok := registry.VocabularyByName(p.Vocab); !ok {
					t.Errorf("%s.%s names unknown vocabulary %q", e.Name, p.Key, p.Vocab)
				}
			case registry.ShapeRef:
				if !entities[p.Ref] {
					t.Errorf("%s.%s refers to %q, not an entity of the contract", e.Name, p.Key, p.Ref)
				}
			}
			if len(p.Tiers) == 0 {
				t.Errorf("%s.%s is agent-writable with no allowed tier", e.Name, p.Key)
			}
			if p.Doc == "" {
				t.Errorf("%s.%s has no doc; the prompt and the editor read it", e.Name, p.Key)
			}
		}
	}
}

func TestRegistryListsHaveNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range registry.RefusalCodes {
		if seen[c.Code] {
			t.Errorf("refusal code %s listed twice", c.Code)
		}
		seen[c.Code] = true
	}
	for _, v := range registry.Vocabularies {
		if seen["vocab:"+v.Name] {
			t.Errorf("vocabulary %s listed twice", v.Name)
		}
		seen["vocab:"+v.Name] = true
		keys := map[string]bool{}
		for _, term := range v.Terms {
			if keys[term.Key] {
				t.Errorf("vocabulary %s lists %s twice", v.Name, term.Key)
			}
			keys[term.Key] = true
		}
	}
}
