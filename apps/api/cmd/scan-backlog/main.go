package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func main() {
	baseURL := flag.String("base-url", "", "OpenAI-compatible base URL (required, e.g. http://127.0.0.1:8000/v1)")
	token := flag.String("token", "", "bearer token (optional; a local vLLM usually needs none)")
	modelID := flag.String("model", "", "model id to score with (required)")
	workers := flag.Int("workers", 4, "concurrent scoring workers")
	limit := flag.Int("limit", 0, "score only the first N valid records (0 = all)")
	out := flag.String("out", "", "scored JSONL output path (append-mode, resumable) (required)")
	topN := flag.Int("top", 100, "worklist size: the N highest-scoring items")
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 || *baseURL == "" || *modelID == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: scan-backlog -base-url URL -model ID -out FILE [flags] <input.jsonl>")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if *workers < 1 {
		fmt.Fprintf(os.Stderr, "invalid -workers %d: must be >= 1\n", *workers)
		os.Exit(2)
	}
	if *topN < 0 {
		fmt.Fprintf(os.Stderr, "invalid -top %d: must be >= 0\n", *topN)
		os.Exit(2)
	}

	inFile, err := os.Open(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input %s: %v\n", args[0], err)
		os.Exit(1)
	}
	defer inFile.Close()

	cfg := scanConfig{
		baseURL: *baseURL,
		token:   *token,
		model:   *modelID,
		workers: *workers,
		limit:   *limit,
		outPath: *out,
		topN:    *topN,
	}
	client := newClient(cfg.baseURL, cfg.token, cfg.model)

	res, err := run(context.Background(), cfg, client, inFile, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
		os.Exit(1)
	}
	if res.failed > 0 {
		os.Exit(1)
	}
}
