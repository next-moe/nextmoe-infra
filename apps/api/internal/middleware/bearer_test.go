package middleware

import (
	stderrors "errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestMalformedTokenIsDistinguishable(t *testing.T) {
	keyfunc := func(*jwt.Token) (any, error) { return []byte("secret"), nil }

	for _, token := range []string{"undefined", "null", "", "not.a.jwt"} {
		_, err := jwt.Parse(token, keyfunc)
		if !stderrors.Is(err, jwt.ErrTokenMalformed) {
			t.Errorf("parse(%q) err = %v, want ErrTokenMalformed", token, err)
		}
	}
}

func TestSplitBearer(t *testing.T) {
	cases := []struct {
		header string
		token  string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"bearer abc", "abc", true},
		{"BEARER abc", "abc", true},
		{"Basic abc", "", false},
		{"abc", "", false},
	}
	for _, c := range cases {
		token, ok := splitBearer(c.header)
		if token != c.token || ok != c.ok {
			t.Errorf("splitBearer(%q) = (%q, %v), want (%q, %v)", c.header, token, ok, c.token, c.ok)
		}
	}
}

func TestPlaceholderToken(t *testing.T) {
	if got := placeholderToken("undefined"); got != "undefined" {
		t.Errorf("placeholderToken(undefined) = %q", got)
	}
	if got := placeholderToken("eyJhbGciOiJFUzI1NiJ9.x.y"); got != "" {
		t.Errorf("a real token must never be echoed, got %q", got)
	}
}
