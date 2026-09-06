package imagegradesync

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

// The only sources whose sexual value must never be refined from image grades:
// vndb rows carry per-image community votes, curated rows are operator-entered,
// user rows are user-declared — all human-authored. censored rows are the
// first-party blurred stand-ins derived FROM explicit art: their sexual=0 is
// true by construction (the bytes are a color-field ghost), and a grader that
// sees through the blur and stamps them explicit would blank the SFW face the
// stand-in exists to fill. EVERY other key in catalog_source — bangumi, dlsite,
// getchu, upscale, steam, and any source added later — is a machine importer
// that stamps one work-level rating on every image it inserts, so it is refined
// from the per-image grade automatically and needs no edit here. Adding a
// machine source to this list silently freezes its rows at the work-level stamp.
var humanAuthoredSources = []string{"vndb", "curated", "user", "censored"}

var mediaTables = []string{"catalog_work_screenshot", "catalog_work_cover"}

type Opts struct {
	DSN       string
	ImagesDSN string
	Apply     bool
	Limit     int
	Batch     int
	Source    string
}

type Stats struct {
	Scanned   int
	Planned   int
	Updated   int
	Unchanged int
	Ungraded  int
	Missing   int
	Errors    int

	changes map[changeKey]int
}

type changeKey struct {
	Source string
	Table  string
	From   int16
	To     int16
}

type Change struct {
	Source string
	Table  string
	From   int16
	To     int16
	Count  int
}

func (s *Stats) note(source, table string, from, to int16) {
	if s.changes == nil {
		s.changes = make(map[changeKey]int)
	}
	s.changes[changeKey{Source: source, Table: table, From: from, To: to}]++
}

func (s *Stats) Changes() []Change {
	out := make([]Change, 0, len(s.changes))
	for k, n := range s.changes {
		out = append(out, Change{Source: k.Source, Table: k.Table, From: k.From, To: k.To, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Source != b.Source:
			return a.Source < b.Source
		case a.Table != b.Table:
			return a.Table < b.Table
		case a.From != b.From:
			return a.From < b.From
		default:
			return a.To < b.To
		}
	})
	return out
}

func (s *Stats) String() string {
	return fmt.Sprintf("scanned=%d planned=%d updated=%d unchanged=%d ungraded=%d missing=%d errors=%d",
		s.Scanned, s.Planned, s.Updated, s.Unchanged, s.Ungraded, s.Missing, s.Errors)
}

func (s *Stats) Matrix() string {
	changes := s.Changes()
	if len(changes) == 0 {
		return "no sexual value would change"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s %-24s %-8s %s\n", "source", "table", "sexual", "rows")
	for _, c := range changes {
		fmt.Fprintf(&b, "%-14s %-24s %d -> %d   %d\n", c.Source, c.Table, c.From, c.To, c.Count)
	}
	return strings.TrimRight(b.String(), "\n")
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.ImagesDSN == "" {
		return nil, fmt.Errorf("--dsn (catalog) and --images-dsn are both REQUIRED; refusing to guess either")
	}
	if opts.Batch <= 0 {
		opts.Batch = 5000
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer closeDB(db)
	idb, err := database.OpenJob(opts.ImagesDSN)
	if err != nil {
		return nil, fmt.Errorf("connect images: %w", err)
	}
	defer closeDB(idb)

	sc, err := resolveScope(ctx, db, opts.Source)
	if err != nil {
		return nil, err
	}
	return sweepAll(ctx, db, idb, sc, opts)
}

type scope struct {
	ids   []int16
	names map[int16]string
}

type sourceRow struct {
	ID  int16  `gorm:"column:id"`
	Key string `gorm:"column:key"`
}

func resolveScope(ctx context.Context, db *gorm.DB, only string) (scope, error) {
	var rows []sourceRow
	if err := db.WithContext(ctx).Raw(`SELECT id, key FROM catalog_source ORDER BY id`).Scan(&rows).Error; err != nil {
		return scope{}, fmt.Errorf("read catalog_source: %w", err)
	}
	if len(rows) == 0 {
		return scope{}, fmt.Errorf("catalog_source is empty — registry not seeded")
	}
	return buildScope(rows, only)
}

func buildScope(rows []sourceRow, only string) (scope, error) {
	human := make(map[string]bool, len(humanAuthoredSources))
	for _, k := range humanAuthoredSources {
		human[k] = true
	}
	sc := scope{names: make(map[int16]string, len(rows))}
	registered := make(map[string]bool, len(rows))
	for _, r := range rows {
		registered[r.Key] = true
		if human[r.Key] {
			continue
		}
		if only != "" && r.Key != only {
			continue
		}
		sc.ids = append(sc.ids, r.ID)
		sc.names[r.ID] = r.Key
	}
	// A key that vanished from catalog_source is a rename (galgame_wiki became
	// curated in wave 161), and a rename that goes unnoticed here silently opens
	// the human lane to machine rewrites.
	for _, k := range humanAuthoredSources {
		if !registered[k] {
			return sc, fmt.Errorf("human-authored source %q is not registered in catalog_source; refusing to run with an exclusion list that no longer matches the registry", k)
		}
	}
	if only != "" {
		if !registered[only] {
			return sc, fmt.Errorf("unknown --source %q: not a catalog_source key", only)
		}
		if human[only] {
			return sc, fmt.Errorf("--source %q is human-authored (%s); those rows are never refined from image grades",
				only, strings.Join(humanAuthoredSources, ", "))
		}
	}
	if len(sc.ids) == 0 {
		return sc, fmt.Errorf("no machine-ingested sources in scope")
	}
	return sc, nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
