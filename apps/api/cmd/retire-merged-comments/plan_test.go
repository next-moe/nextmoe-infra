package main

import "testing"

// A site_game anchor means different things on different sites. On the forum it
// is a gid of the site's own — a number from the product keyspace that may
// collide with an unrelated catalog id, which is why a live claim owning it
// keeps the anchor alive. On moyu the same field IS the catalog work id (铁律
// 3), where that exclusion would instead keep a genuinely dead anchor standing.
func TestPlanForSiteGameAnchorIDSpace(t *testing.T) {
	forum, err := planFor("kungal", 1)
	if err != nil {
		t.Fatalf("kungal site_game: %v", err)
	}
	if !forum.claimAware || forum.catalogIDs {
		t.Fatalf("kungal site_game must be claim-aware and not a catalog id space, got %+v", forum)
	}

	moyu, err := planFor("moyu", 1)
	if err != nil {
		t.Fatalf("moyu site_game: %v", err)
	}
	if moyu.claimAware || !moyu.catalogIDs {
		t.Fatalf("moyu site_game carries catalog ids, got %+v", moyu)
	}

	// The catalog anchor kind is the same on every site.
	for _, site := range []string{"kungal", "moyu", "letmoe"} {
		plan, err := planFor(site, 3)
		if err != nil {
			t.Fatalf("%s catalog_work: %v", site, err)
		}
		if plan.claimAware || plan.catalogIDs {
			t.Fatalf("%s catalog_work must be plain, got %+v", site, plan)
		}
	}

	// An unmapped kind is still an error rather than a guess, on every site.
	for _, kind := range []int16{0, 2, 4} {
		if _, err := planFor("moyu", kind); err == nil {
			t.Fatalf("anchor kind %d names no catalog entity and must not be guessed at", kind)
		}
	}
}
