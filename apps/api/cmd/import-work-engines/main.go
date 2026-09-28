package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/jobs/workengines"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; also hosts src_vndb and src_bangumi)")
	lane := flag.String("lane", workengines.LaneAll, "vndb | bgm | all")
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	flag.Parse()

	logger.Init("development")
	st, err := workengines.Run(context.Background(), workengines.Opts{Apply: *apply, DSN: *dsn, Lane: *lane})
	if err != nil {
		slog.Error("import-work-engines failed", "error", err)
		os.Exit(1)
	}
	mode := "DRY"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== import-work-engines %s (lane=%s) ===\n", mode, *lane)
	fmt.Printf("engines: created=%d linked_to_vndb=%d\n", st.EnginesCreated, st.EnginesLinked)
	fmt.Printf("releases: vndb_anchored=%d same=%d human=%d filled=%d changed=%d cleared=%d written=%d lost=%d unknown_vndb_engine=%d\n",
		st.Releases, st.ReleasesSame, st.ReleasesHuman, st.ReleasesFilled, st.ReleasesChanged,
		st.ReleasesCleared, st.ReleasesWritten, st.ReleasesLost, st.UnknownVNDBEngine)
	fmt.Printf("bangumi: stated=%d covered_by_other_sources=%d works=%d add=%d drop=%d unmapped_values=%d\n",
		st.BgmStated, st.BgmCovered, st.BgmWorks, st.BgmAdd, st.BgmDrop, len(st.Unmapped))
	if top := st.TopUnmapped(40); len(top) > 0 {
		fmt.Printf("unmapped e.g. %s\n", strings.Join(top, ", "))
	}
}
