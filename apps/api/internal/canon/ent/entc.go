//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

func main() {
	if err := entc.Generate("./schema", &gen.Config{
		Features: []gen.Feature{gen.FeatureVersionedMigration, gen.FeatureLock, gen.FeatureExecQuery, gen.FeatureUpsert},
	}); err != nil {
		log.Fatal(err)
	}
}
