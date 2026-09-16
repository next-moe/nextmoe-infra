package main

import (
	"testing"
	"time"
)

func TestParseEditReadsBothOfMoyusFormats(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"rfc3339", "2026-06-06T10:34:59Z", time.Date(2026, 6, 6, 10, 34, 59, 0, time.UTC), true},
		{"epoch millis", "1733996767277", time.UnixMilli(1733996767277).UTC(), true},
		{"empty", "", time.Time{}, false},
		{"prose", "edited by hand", time.Time{}, false},
		{"partial date", "2026-06-06", time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseEdit(c.in)
			if ok != c.ok {
				t.Fatalf("parseEdit(%q) ok = %v, want %v", c.in, ok, c.ok)
			}
			if ok && !got.Equal(c.want) {
				t.Fatalf("parseEdit(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
