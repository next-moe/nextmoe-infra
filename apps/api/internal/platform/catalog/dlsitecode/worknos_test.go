package dlsitecode

import (
	"slices"
	"testing"
)

func TestWorknos(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{
			name: "store url",
			in:   "https://www.dlsite.com/maniax/work/=/product_id/RJ01225370.html",
			want: []string{"RJ01225370"},
		},
		{
			name: "bare lowercase",
			in:   "rj012253",
			want: []string{"RJ012253"},
		},
		{
			name: "seven digit run dropped",
			in:   "RJ0122537",
			want: nil,
		},
		{
			name: "nine digit run dropped",
			in:   "RJ012253701",
			want: nil,
		},
		{
			name: "VJ and BJ",
			in:   "see VJ015428 and BJ123456",
			want: []string{"VJ015428", "BJ123456"},
		},
		{
			name: "duplicates collapsed",
			in:   "RJ012253 rj012253 RJ012253",
			want: []string{"RJ012253"},
		},
		{
			name: "none",
			in:   "no store code here",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Worknos(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Worknos(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
