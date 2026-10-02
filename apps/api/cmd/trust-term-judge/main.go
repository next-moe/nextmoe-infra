package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/llmsuggest"
	"api/pkg/config"
	"api/pkg/logger"
)

const usage = `trust-term-judge retires ordinary words from the Trust & Safety suspect lexicon on an LLM's verdict.

  -mode judge   no database: read candidate terms (JSONL: id, term), ask the model which are
                ordinary words, append one verdict per term to -out. Resumable: terms already
                in -out are skipped. The key comes from KUN_LLM_API_KEY.
  -mode apply   read the verdicts and deprecate the retired terms that are still active
                abuse-purpose suspect terms. Dry-run unless -apply, which requires -backup.
`

func main() {
	mode := flag.String("mode", "", "judge | apply")
	in := flag.String("in", "", "judge: candidate terms, JSONL")
	out := flag.String("out", "", "judge: verdicts, JSONL (appended)")
	llmBase := flag.String("llm-base", "", "judge: OpenAI-compatible base URL")
	modelID := flag.String("model", "", "judge: model id; apply: named in the audit row")
	batch := flag.Int("batch", 60, "judge: terms per request")
	verdicts := flag.String("verdicts", "", "apply: verdicts JSONL from -mode judge")
	backup := flag.String("backup", "", "apply: write the retired set to this JSON file (required with -apply)")
	apply := flag.Bool("apply", false, "apply: write to the database (default: dry-run report)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage); flag.PrintDefaults() }
	flag.Parse()

	switch *mode {
	case "judge":
		if *in == "" || *out == "" || *llmBase == "" || *modelID == "" || *batch < 1 {
			flag.Usage()
			os.Exit(2)
		}
		j := &judge{llm: llmsuggest.NewClient(*llmBase, *modelID), batch: *batch}
		if err := j.run(context.Background(), *in, *out); err != nil {
			fmt.Fprintln(os.Stderr, "judge:", err)
			os.Exit(1)
		}
	case "apply":
		if *verdicts == "" {
			flag.Usage()
			os.Exit(2)
		}
		if *apply && *backup == "" {
			fmt.Fprintln(os.Stderr, "-apply requires -backup: deprecation must be recoverable")
			os.Exit(2)
		}
		cfg, err := config.Load()
		if err != nil {
			slog.Error("failed to load config", "error", err)
			os.Exit(1)
		}
		logger.Init(cfg.Server.Env)
		db, err := database.NewPostgresDB(cfg.TrustDatabase)
		if err != nil {
			slog.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()
		if err := runApply(db.DB(), *verdicts, *backup, *modelID, *apply); err != nil {
			slog.Error("apply", "error", err)
			os.Exit(1)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}
