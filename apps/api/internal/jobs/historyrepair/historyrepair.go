// Package historyrepair is the one-off repair for the 2026-11 edit-history
// report: revisions written before the engine recorded prior values, editor
// uploads written before they carried an upload stamp, and the editor uploads
// the 2026-09 wiki-lineage cleanup deleted without a revision.
package historyrepair

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Cut separates the two editing engines' history on catalog.work. Revisions
// before it were written by the retired galgame engine and rekeyed onto catalog
// keys on 2026-07-30; their list fields describe every lane the wiki showed,
// while the catalog loaders read the curated lane alone. The last galgame-era
// revision is 2026-07-31 09:58Z and the first catalog-engine one 2026-08-04.
var Cut = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

type Stats struct {
	Mode     string
	Scanned  int
	Written  int
	Skipped  int
	Works    []int64
	ByField  map[string]int
	Samples  []string
	Applied  bool
	Problems []string
}

func newStats(mode string, apply bool) *Stats {
	return &Stats{Mode: mode, Applied: apply, ByField: map[string]int{}}
}

func (s *Stats) sample(format string, args ...any) {
	if len(s.Samples) < 20 {
		s.Samples = append(s.Samples, fmt.Sprintf(format, args...))
	}
}

func (s *Stats) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode=%s applied=%t scanned=%d written=%d skipped=%d works=%d",
		s.Mode, s.Applied, s.Scanned, s.Written, s.Skipped, len(s.Works))
	keys := make([]string, 0, len(s.ByField))
	for k := range s.ByField {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%d", k, s.ByField[k])
	}
	for _, line := range s.Samples {
		b.WriteString("\n  " + line)
	}
	for _, p := range s.Problems {
		b.WriteString("\n  PROBLEM " + p)
	}
	return b.String()
}
