package search

import "testing"

func TestCharacterDocIDRoundTrip(t *testing.T) {
	id, ok := CharacterDocIDToID(CharacterDocID(42))
	if !ok || id != 42 {
		t.Fatalf("got %d %v", id, ok)
	}
	for _, bad := range []string{"", "c", "w1", "c0", "c-1", "cx"} {
		if _, ok := CharacterDocIDToID(bad); ok {
			t.Fatalf("%q parsed", bad)
		}
	}
}
