package utils

import (
	"encoding/hex"
	"testing"

	stdArgon2 "golang.org/x/crypto/argon2"
)

// Hashes minted by matthewhartstonge/argon2 v1.4.6 (DefaultConfig: m=65536,t=3,p=4)
// and stored in oauth_users.password. VerifyEncoded reads the parameters back out of
// the string, so an upgrade that changes DefaultConfig must still verify these; if it
// ever stops, every existing account fails to log in and the cause is invisible from
// the login handler.
var argon2StoredHashes = map[string]string{
	"correct horse battery staple": "$argon2id$v=19$m=65536,t=3,p=4$A56kLH4j8RclTs3AMRr30g$hxQA3llP7jh0tD4EAAoloHe1kJNw65qNU53eVqQo9KU",
	"hunter2":                      "$argon2id$v=19$m=65536,t=3,p=4$jsFzvT+ygnXyQLYHZoFSOw$hnArR08Z31D8tj3hNunOR6/KF3UKVKtvXeOMwwsP9j8",
	"日本語パスワード":                     "$argon2id$v=19$m=65536,t=3,p=4$CMdgZYFjBNOy9p0uMA8eLA$6cZJDNVgGGU2Z5jejEewJatMxCe10mgKaagyV+ZDfxU",
}

func TestVerifyPasswordAcceptsStoredHashes(t *testing.T) {
	for password, hash := range argon2StoredHashes {
		ok, err := VerifyPassword(password, hash)
		if err != nil {
			t.Fatalf("VerifyPassword(%q): %v", password, err)
		}
		if !ok {
			t.Errorf("VerifyPassword(%q) = false, want true", password)
		}
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash := argon2StoredHashes["hunter2"]
	for _, wrong := range []string{"hunter3", "", "HUNTER2", "hunter2 "} {
		ok, err := VerifyPassword(wrong, hash)
		if err != nil {
			t.Fatalf("VerifyPassword(%q): %v", wrong, err)
		}
		if ok {
			t.Errorf("VerifyPassword(%q) = true, want false", wrong)
		}
	}
}

func TestHashPasswordRoundTrips(t *testing.T) {
	for password := range argon2StoredHashes {
		hash, err := HashPassword(password)
		if err != nil {
			t.Fatalf("HashPassword(%q): %v", password, err)
		}
		if hash == argon2StoredHashes[password] {
			t.Errorf("HashPassword(%q) returned the stored hash: the salt is not random", password)
		}
		ok, err := VerifyPassword(password, hash)
		if err != nil {
			t.Fatalf("VerifyPassword(%q): %v", password, err)
		}
		if !ok {
			t.Errorf("HashPassword(%q) produced a hash VerifyPassword rejects", password)
		}
	}
}

func TestVerifyMoyuPassword(t *testing.T) {
	// moyu stored salt:hex(argon2id(password, salt, t=2, m=8192, p=3, 32 bytes)).
	const salt = "moyusalt"
	const password = "moyu-secret"
	digest := hex.EncodeToString(stdArgon2.IDKey([]byte(password), []byte(salt), 2, 8192, 3, 32))
	stored := salt + ":" + digest

	tests := []struct {
		name     string
		password string
		stored   string
		want     bool
	}{
		{"accepts the stored digest", password, stored, true},
		{"rejects a wrong password", "moyu-secre", stored, false},
		{"rejects a wrong salt", password, "othersalt:" + digest, false},
		{"rejects a missing separator", password, digest, false},
		{"rejects a non-hex digest", password, salt + ":zzzz", false},
		{"rejects an empty string", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyMoyuPassword(tt.password, tt.stored); got != tt.want {
				t.Errorf("VerifyMoyuPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}
