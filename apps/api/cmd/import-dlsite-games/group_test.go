package main

import (
	"reflect"
	"testing"

	"api/internal/platform/catalog/importer"
)

func TestUnionFindJoinsEditionsBothWays(t *testing.T) {
	forward := importer.DLsiteUnionFind([]string{"RJA", "RJB"}, [][2]string{{"RJA", "RJB"}})
	if !sameGroups(forward, [][]string{{"RJA", "RJB"}}) {
		t.Fatalf("A listing B: got %v", forward)
	}
	back := importer.DLsiteUnionFind([]string{"RJA", "RJB"}, [][2]string{{"RJB", "RJA"}})
	if !sameGroups(back, [][]string{{"RJA", "RJB"}}) {
		t.Fatalf("B listing A: got %v", back)
	}
	outside := importer.DLsiteUnionFind([]string{"RJA"}, [][2]string{{"RJA", "RJC"}})
	if !sameGroups(outside, [][]string{{"RJA", "RJC"}}) {
		t.Fatalf("unknown edition target still joins: got %v", outside)
	}
}

func TestPrimaryGroupOrder(t *testing.T) {
	jpn := importer.DLsitePrimaryKey{HasJPN: true, YMD: "2020-02-01", Workno: "RJ9"}
	early := importer.DLsitePrimaryKey{HasJPN: false, YMD: "2010-01-01", Workno: "RJ1"}
	if !importer.DLsitePrimaryLess(jpn, early) {
		t.Fatal("Japanese edition ranks before an earlier non-Japanese product")
	}
	a := importer.DLsitePrimaryKey{YMD: "2010-01-01", Workno: "RJ2"}
	b := importer.DLsitePrimaryKey{YMD: "2011-01-01", Workno: "RJ1"}
	if !importer.DLsitePrimaryLess(a, b) {
		t.Fatal("earlier regist_date ranks before a lower workno")
	}
	c := importer.DLsitePrimaryKey{YMD: "2010-01-01", Workno: "RJ1"}
	d := importer.DLsitePrimaryKey{YMD: "2010-01-01", Workno: "RJ2"}
	if !importer.DLsitePrimaryLess(c, d) {
		t.Fatal("lowest workno breaks a date tie")
	}
	dated := importer.DLsitePrimaryKey{YMD: "2020-01-01", Workno: "RJ9"}
	empty := importer.DLsitePrimaryKey{Workno: "RJ0"}
	if !importer.DLsitePrimaryLess(dated, empty) {
		t.Fatal("a dated product ranks before a missing regist_date")
	}
}

func TestSetsAreTransitive(t *testing.T) {
	got := importer.DLsiteTransitiveSets([][]string{
		{"k1"},
		{"k1", "k2"},
		{"k2"},
		{"k3"},
	}, func(int, int) bool { return true })
	want := [][]int{{0, 1, 2}, {3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DLsiteTransitiveSets = %v, want %v", got, want)
	}
}

func TestSetsJoinOnlyCompatibleGroups(t *testing.T) {
	got := importer.DLsiteTransitiveSets([][]string{{"k"}, {"k"}, {"k"}}, func(i, j int) bool {
		return (i == 2 && j == 0) || (i == 0 && j == 2)
	})
	want := [][]int{{0, 2}, {1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DLsiteTransitiveSets = %v, want %v", got, want)
	}
}

func sameGroups(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			return false
		}
	}
	return true
}
