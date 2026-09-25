package main

import "testing"

// A site_game anchor means different things on different sites. On a site that
// numbers its own pages it is a game id from the product keyspace that may
// collide with an unrelated catalog id, which is why a live claim owning it
// keeps the anchor alive. On moyu, kungal and letmoe (铁律 3) the same field IS
// the catalog work id, where that exclusion would instead keep a genuinely dead
// anchor standing.
func TestPlanForSiteGameAnchorIDSpace(t *testing.T) {
	own, err := planFor(testSite, 1)
	if err != nil {
		t.Fatalf("%s site_game: %v", testSite, err)
	}
	if !own.claimAware || own.catalogIDs {
		t.Fatalf("%s site_game must be claim-aware and not a catalog id space, got %+v", testSite, own)
	}

	for _, site := range []string{"moyu", "kungal", "letmoe"} {
		plan, err := planFor(site, 1)
		if err != nil {
			t.Fatalf("%s site_game: %v", site, err)
		}
		if plan.claimAware || !plan.catalogIDs {
			t.Fatalf("%s site_game carries catalog ids, got %+v", site, plan)
		}
	}

	// The catalog anchor kind is the catalog id on every site.
	for _, site := range []string{"kungal", "moyu", "letmoe", testSite} {
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
