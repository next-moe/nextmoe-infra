package main

import "testing"

// A site_game anchor means different things on different sites. On letmoe it
// is a game id of the site's own -- a number from the product keyspace that may
// collide with an unrelated catalog id, which is why a live claim owning it
// keeps the anchor alive. On moyu (铁律 3) and, since its 2026-09-23 G0
// renumber, on the forum, the same field IS the catalog work id, where that
// exclusion would instead keep a genuinely dead anchor standing.
func TestPlanForSiteGameAnchorIDSpace(t *testing.T) {
	own, err := planFor("letmoe", 1)
	if err != nil {
		t.Fatalf("letmoe site_game: %v", err)
	}
	if !own.claimAware || own.catalogIDs {
		t.Fatalf("letmoe site_game must be claim-aware and not a catalog id space, got %+v", own)
	}

	for _, site := range []string{"moyu", "kungal"} {
		plan, err := planFor(site, 1)
		if err != nil {
			t.Fatalf("%s site_game: %v", site, err)
		}
		if plan.claimAware || !plan.catalogIDs {
			t.Fatalf("%s site_game carries catalog ids, got %+v", site, plan)
		}
	}

	// The catalog anchor kind is the catalog id on every site.
	for _, site := range []string{"kungal", "moyu", "letmoe"} {
		plan, err := planFor(site, 3)
		if err != nil {
			t.Fatalf("%s catalog_work: %v", site, err)
		}
		if plan.claimAware || !plan.catalogIDs {
			t.Fatalf("%s catalog_work carries catalog ids and no claim exclusion, got %+v", site, plan)
		}
	}

	// An unmapped kind is still an error rather than a guess, on every site.
	for _, kind := range []int16{0, 2, 4} {
		if _, err := planFor("moyu", kind); err == nil {
			t.Fatalf("anchor kind %d names no catalog entity and must not be guessed at", kind)
		}
	}
}
