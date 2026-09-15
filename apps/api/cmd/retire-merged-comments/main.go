// Retires the comments threads whose catalog anchor a merge took away.
//
// A merge retires the source work, and every site page that named it 404s. The
// conversation under that page does not move: community stores an opaque
// anchor and has no way to learn that the id behind it is gone, and the catalog
// has no reach into another service's database. Until this existed the forum's
// merge sync could only say so in a log line nobody reads --
// "被合并条目的评论区留在了原锚点, 需要 infra 侧改锚".
//
// The ruling (2026-09-15) is to retire them rather than re-anchor them. Moving
// a thread needs a proof in the SITE's id space -- old gid to new gid -- and
// that proof resolved for 133 of the 2,950 dead anchors in production, because
// the forum's fold only looks at gids it holds a local row for. Retiring needs
// only the one-sided fact that the anchor is dead, which the catalog states for
// all of them. The two errors are not symmetric either: a wrong retire hides a
// conversation and a wrong re-anchor files it under an unrelated game, where it
// looks correct.
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

// exitFound reports that the run has something for a person to see: threads it
// would retire, or threads it did retire. A deletion is announced every time.
const exitFound = 3

func main() {
	catalogDSN := flag.String("catalog-dsn", "", "catalog DSN (read only); env fallback KUN_CATALOG_DSN")
	communityDSN := flag.String("community-dsn", "", "kun_community DSN; env fallback KUN_COMMUNITY_DSN, then KUN_COMMUNITY_* config")
	sites := flag.String("sites", "kungal,letmoe", "comma-separated community sites whose site_game anchor carries a catalog work id")
	anchorKind := flag.Int("anchor-kind", 1, "anchor kind to sweep (1=site_game)")
	limit := flag.Int("limit", 200, "retire at most this many threads per run (0 = no cap)")
	apply := flag.Bool("apply", false, "perform the retirement; default is a report only")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	catDSN := firstNonEmpty(*catalogDSN, os.Getenv("KUN_CATALOG_DSN"), cfg.CatalogDatabase.DSN())
	comDSN := firstNonEmpty(*communityDSN, os.Getenv("KUN_COMMUNITY_DSN"), cfg.CommunityDatabase.DSN())

	cat, err := database.OpenJob(catDSN)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	com, err := database.OpenJob(comDSN)
	if err != nil {
		slog.Error("community db connect", "error", err)
		os.Exit(1)
	}

	found, retired, err := run(cat, com, strings.Split(*sites, ","), int16(*anchorKind), *limit, *apply, os.Stdout)
	if err != nil {
		slog.Error("sweep failed", "error", err)
		os.Exit(1)
	}
	if found > 0 || retired > 0 {
		os.Exit(exitFound)
	}
}

func run(cat, com *gorm.DB, sites []string, anchorKind int16, limit int, apply bool, out io.Writer) (found, retired int, err error) {
	for _, site := range sites {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}
		live, err := liveAnchors(com, sweepOpts{Site: site, AnchorKind: anchorKind})
		if err != nil {
			return found, retired, fmt.Errorf("%s: read anchors: %w", site, err)
		}
		rows, err := strandedAmong(cat, site, live)
		if err != nil {
			return found, retired, fmt.Errorf("%s: classify: %w", site, err)
		}
		if len(rows) == 0 {
			fmt.Fprintf(out, "%s: %d live comments threads, none on a merged-away work\n", site, len(live))
			continue
		}
		if limit > 0 && len(rows) > limit {
			fmt.Fprintf(out, "%s: %d stranded, capped at %d for this run\n", site, len(rows), limit)
			rows = rows[:limit]
		}
		found += len(rows)
		report(out, site, rows, apply)
		if !apply {
			continue
		}
		n, err := retire(com, rows)
		if err != nil {
			return found, retired, fmt.Errorf("%s: retire: %w", site, err)
		}
		retired += int(n)
		fmt.Fprintf(out, "%s: retired %d thread(s)\n", site, n)
	}
	return found, retired, nil
}

// The undo is the thread ids, printed on every run that retires: the rows keep
// every post and `UPDATE community_thread SET status = 0 WHERE id IN (...)`
// puts them back. It holds only while no live thread has appeared on the same
// anchor, which for a merged-away anchor means never -- its page 404s, so
// nobody can open it and nothing can mint one.
func report(out io.Writer, site string, rows []stranded, apply bool) {
	verb := "would retire"
	if apply {
		verb = "retiring"
	}
	fmt.Fprintf(out, "%s: %s %d comments thread(s) on merged-away works\n", site, verb, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		survivor := fmt.Sprintf("work %d", r.Survivor)
		if r.SurvivorAnchor != "" {
			survivor += fmt.Sprintf(" (anchor %s)", r.SurvivorAnchor)
		}
		fmt.Fprintf(out, "  thread %d  anchor %s  posts %d  ->  %s\n",
			r.ThreadID, r.AnchorID, r.Posts, survivor)
		ids = append(ids, fmt.Sprintf("%d", r.ThreadID))
	}
	if apply {
		fmt.Fprintf(out, "  undo: UPDATE community_thread SET status = 0 WHERE id IN (%s);\n",
			strings.Join(ids, ", "))
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
