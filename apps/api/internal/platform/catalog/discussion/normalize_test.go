package discussion

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeBody(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		err  error
	}{
		{"  好的\r\n谢谢  ", "好的\n谢谢", nil},
		{" \n\t ", "", ErrEmptyBody},
		{strings.Repeat("字", MaxBodyRunes), strings.Repeat("字", MaxBodyRunes), nil},
		{strings.Repeat("字", MaxBodyRunes+1), "", ErrBodyTooLong},
	}
	for _, c := range cases {
		got, err := NormalizeBody(c.raw)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("NormalizeBody(%q) = %q, %v; want %q, %v", c.raw, got, err, c.want, c.err)
		}
	}
}

func TestNormalizeMentionsDropsAuthorDuplicatesAndBadIDs(t *testing.T) {
	got, err := normalizeMentions([]int64{5, 7, 5, 0, -2, 9}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 5 || got[1] != 9 {
		t.Fatalf("got %v, want [5 9]", got)
	}
	many := make([]int64, MaxMentions+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	if _, err := normalizeMentions(many, 0); !errors.Is(err, ErrTooManyMentions) {
		t.Fatalf("err = %v, want ErrTooManyMentions", err)
	}
}
