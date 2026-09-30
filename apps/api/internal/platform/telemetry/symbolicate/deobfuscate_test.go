package symbolicate

import "testing"

func TestDeobfuscateType(t *testing.T) {
	m := map[string]string{"Xy": "Original", "Zw": "Other"}
	got := DeobfuscateType("_Foo<Xy>", m)
	if got != "_Foo<Original>" {
		t.Errorf("got %q", got)
	}
	if DeobfuscateType("Xy", m) != "Original" {
		t.Errorf("plain")
	}
	if DeobfuscateType("Nope", m) != "Nope" {
		t.Errorf("missing")
	}
}

func TestDeobfuscateMessageOnlyInQuotes(t *testing.T) {
	m := map[string]string{"Xy": "Foo", "type": "Nope", "is": "nope"}
	in := "type 'Xy' is not a subtype of type 'Zw'"
	got := DeobfuscateMessage(in, m)
	want := "type 'Foo' is not a subtype of type 'Zw'"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestObfuscationPairsOrderAndPrivateSuffix(t *testing.T) {
	m := ObfuscationPairs([]string{
		"ListUserWallCommentsResponse401", "Obc",
		"", "",
		"_KunChatQuickReaction@202143710", "_UFa@202143710",
	})
	if got := DeobfuscateType("Obc", m); got != "ListUserWallCommentsResponse401" {
		t.Errorf("public name: got %q", got)
	}
	if got := DeobfuscateType("_UFa", m); got != "_KunChatQuickReaction" {
		t.Errorf("private name: got %q", got)
	}
	if _, ok := m["ListUserWallCommentsResponse401"]; ok {
		t.Error("original name must not be a key")
	}
	if len(m) != 2 {
		t.Errorf("len = %d, want 2", len(m))
	}
}
