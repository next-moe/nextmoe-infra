package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/workaliases"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; hosts src_bangumi)")
	dlsiteDSN := flag.String("dlsite-dsn", "", "dlsite mirror DSN (REQUIRED for dlsite-kana/all)")
	source := flag.String("source", "all", "lane: bgm | dlsite-kana | all")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	flag.Parse()

	logger.Init("development")
	st, err := workaliases.Run(context.Background(), workaliases.Opts{
		Apply: *apply, DSN: *dsn, DlsiteDSN: *dlsiteDSN, Source: *source,
	})
	if err != nil {
		slog.Error("import-work-aliases failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== import-work-aliases %s ===\n", mode(*apply))
	fmt.Printf("bgm: works=%d planned=%d skipped_dup=%d written=%d conflict=%d\n",
		st.BgmWorks, st.BgmPlanned, st.BgmSkippedDup, st.BgmWritten, st.BgmConflict)
	fmt.Printf("kana: works=%d bundle=%d no_kana=%d planned=%d written=%d\n",
		st.KanaWorks, st.KanaBundle, st.KanaNoKana, st.KanaPlanned, st.KanaWritten)
	fmt.Printf("errors=%d\n", st.Errors)
	if st.Errors > 0 {
		os.Exit(1)
	}
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}
