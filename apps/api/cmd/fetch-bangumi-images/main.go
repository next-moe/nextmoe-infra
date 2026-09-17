package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"api/internal/jobs/bgmimages"
)

const userAgent = "nextmoe-infra/fetch-bangumi-images (+https://www.kungal.com)"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fetch-bangumi-images", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kind := fs.String("kind", "", "covers (subject ids) | persons (person ids, company logos included)")
	idsFile := fs.String("ids-file", "", "ids to fetch, one per line")
	out := fs.String("out", "", "mirror root: <out>/<id>/cover.jpg or <out>/<id>/logo.<ext>, plus <out>/dims.jsonl")
	rate := fs.Float64("rate", 0, "requests per second to each host (default 2 for covers, 1 for persons)")
	concurrency := fs.Int("concurrency", 0, "ids in flight (default 3 for covers, 2 for persons)")
	limit := fs.Int("limit", 0, "fetch only the first N ids (0 = all)")
	apiBase := fs.String("api-base", "https://api.bgm.tv", "Bangumi API root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	k := bgmimages.Kind(*kind)
	if k != bgmimages.KindCovers && k != bgmimages.KindPersons {
		fmt.Fprintf(stderr, "fetch-bangumi-images: --kind must be covers or persons, got %q\n", *kind)
		return 2
	}
	if *idsFile == "" || *out == "" {
		fmt.Fprintln(stderr, "fetch-bangumi-images: --ids-file and --out are required")
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	*rate, *concurrency = pace(k, set, *rate, *concurrency)

	ids, err := bgmimages.LoadIDs(*idsFile)
	if err != nil {
		fmt.Fprintf(stderr, "fetch-bangumi-images: read ids: %v\n", err)
		return 1
	}
	if *limit > 0 && *limit < len(ids) {
		ids = ids[:*limit]
	}

	st, err := bgmimages.Run(ctx, bgmimages.Opts{
		Kind:        k,
		Out:         *out,
		APIBase:     *apiBase,
		UserAgent:   userAgent,
		Token:       getenv("KUN_BANGUMI_TOKEN"),
		Rate:        *rate,
		Concurrency: *concurrency,
		MaxRetries:  4,
	}, ids)
	fmt.Fprintln(stdout, st.Line())
	if err != nil {
		fmt.Fprintf(stderr, "fetch-bangumi-images: %v\n", err)
		return 1
	}
	return 0
}

func pace(k bgmimages.Kind, set map[string]bool, rate float64, concurrency int) (float64, int) {
	if !set["rate"] {
		rate = map[bgmimages.Kind]float64{bgmimages.KindCovers: 2, bgmimages.KindPersons: 1}[k]
	}
	if !set["concurrency"] {
		concurrency = map[bgmimages.Kind]int{bgmimages.KindCovers: 3, bgmimages.KindPersons: 2}[k]
	}
	return rate, concurrency
}
