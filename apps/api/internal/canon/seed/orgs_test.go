package seed

import (
	"context"
	"fmt"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/ent/orgname"
	"api/internal/canon/identity"
)

func TestTheSeedIsWellFormed(t *testing.T) {
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	keys, names, legacy := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, o := range list {
		if keys[o.Key] || (o.Parent != "" && !keys[o.Parent]) || len(o.Names) == 0 {
			t.Errorf("%s: duplicate key, late parent or no name", o.Key)
		}
		if o.LegacyID != 0 && legacy[o.LegacyID] {
			t.Errorf("%s takes over a legacy id another organization took", o.Key)
		}
		keys[o.Key], legacy[o.LegacyID] = true, true
		for _, n := range o.Names {
			if names[identity.NameKey(n.Text)] {
				t.Errorf("%s: %s is another organization's name", o.Key, n.Text)
			}
			names[identity.NameKey(n.Text)] = true
		}
	}
}

func TestSeedingTwiceWritesOnce(t *testing.T) {
	client, db := canontest.Client(t)
	canontest.Legacy(t, db)
	ctx := context.Background()
	if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s.catalog_label (id, display_name) VALUES (9931, '角川スニーカー文庫')`, identity.LegacySchema)); err != nil {
		t.Fatal(err)
	}
	first, err := Orgs(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Orgs(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(first) != fmt.Sprint(again) {
		t.Errorf("%v then %v", first, again)
	}
	if n, _ := client.OrgName.Query().Where(orgname.Text("富士見ファンタジア文庫")).Count(ctx); n != 1 {
		t.Errorf("%d rows of one name", n)
	}
	sneaker, _ := client.Org.Get(ctx, first["sneaker"])
	if sneaker == nil || sneaker.ParentID == nil || *sneaker.ParentID != first["kadokawa"] {
		t.Errorf("the label belongs to its publisher: %+v", sneaker)
	}
}
