package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/orglabels"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	source := flag.String("source", "all", "vndb | bangumi | eg | all")
	apply := flag.Bool("apply", false, "write (default: dry run — plan counts only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED (also hosts src_vndb / src_bangumi)")
	egDSN := flag.String("eg-dsn", "", "erogamescape DSN (default: catalog DSN with dbname=erogamescape)")
	limit := flag.Int("limit", 0, "cap orgs processed per source (0 = all); debugging aid")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := orglabels.RunAnchor(context.Background(), orglabels.Opts{
		Apply: *apply, DSN: *dsn, EGDSN: *egDSN, Source: *source, Limit: *limit,
	})
	if err != nil {
		slog.Error("reconcile-org-labels failed", "error", err)
		os.Exit(1)
	}
	slog.Info("reconcile-org-labels summary",
		"source", *source, "apply", *apply,
		"orgs", st.Orgs, "already", st.Already,
		"anchors_exact", st.AnchorsExact, "anchors_probable", st.AnchorsProbable,
		"new_labels", st.NewLabels, "new_edges", st.NewEdges,
		"conflict", st.Conflict, "skip_no_match", st.SkipNoMatch,
		"skip_ambiguous", st.SkipAmbiguous, "skip_ungradeable", st.SkipUngradeable,
		"skip_rejected", st.SkipRejected, "skip_deferred", st.SkipDeferred,
		"vndb_in_anchored", st.VNDBInAnchored, "errors", st.Errors)
	slog.Info("reconcile-org-labels spine summary",
		"considered", st.Spine.Considered, "minted", st.Spine.Minted,
		"anchored", st.Spine.Anchored, "candidates", st.Spine.Candidates,
		"skip_claimed", st.Spine.SkipClaimed, "skip_edgeless", st.Spine.SkipEdgeless,
		"skip_alias_only", st.Spine.SkipAliasOnly, "skip_loose_twin", st.Spine.SkipLooseTwin,
		"skip_deferred", st.Spine.SkipDeferred, "errors", st.Spine.Errors)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors+st.Spine.Errors > 0 {
		os.Exit(1)
	}
}
