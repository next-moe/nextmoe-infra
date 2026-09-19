package vndbtags

import (
	"strings"
	"testing"
)

func TestLogArgsKeys(t *testing.T) {
	args := (&Stats{}).LogArgs()
	if len(args)%2 != 0 {
		t.Fatalf("LogArgs length %d is not key/value pairs", len(args))
	}
	keys := make([]string, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			t.Fatalf("LogArgs[%d] is %T, want string key", i, args[i])
		}
		keys = append(keys, k)
	}
	seen := map[string]int{}
	for _, k := range keys {
		seen[k]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("key %q appears %d times", k, n)
		}
	}
	for i, a := range keys {
		for j, b := range keys {
			if i == j {
				continue
			}
			if strings.HasSuffix(a, b) {
				t.Errorf("key %q is a suffix of %q", b, a)
			}
		}
	}
}
