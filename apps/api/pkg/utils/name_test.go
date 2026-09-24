package utils

import "testing"

// The eraser names a deleted account 已注销#<id>. A live account holding that
// name made the eraser's UPDATE violate idx_users_name for that id and stall
// every deletion due after it (review of #305).
func TestTheDeletedAccountPrefixIsReserved(t *testing.T) {
	for _, name := range []string{"已注销#4242", "已注销", "已注销abc"} {
		if IsValidName(name) {
			t.Fatalf("%q must be refused", name)
		}
	}
	if !IsValidName("注销了") {
		t.Fatal("only the prefix is reserved")
	}
}
