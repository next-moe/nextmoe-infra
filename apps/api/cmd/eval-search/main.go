package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"

	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
	"api/pkg/config"
	"api/pkg/logger"
)

type goldRow struct {
	Query  string  `json:"query"`
	Lang   string  `json:"lang"`
	Kind   string  `json:"kind"`
	Tier   string  `json:"tier"`
	Expect []int64 `json:"expect"`
}

type tally struct {
	n      int
	recall float64
	mrr    float64
	ndcg   float64
}

func (t *tally) add(ranked, expect []int64, k int) {
	t.n++
	want := map[int64]bool{}
	for _, id := range expect {
		want[id] = true
	}
	found, dcg, firstHit := 0, 0.0, 0
	for i, id := range ranked {
		if i >= k || !want[id] {
			continue
		}
		found++
		if firstHit == 0 {
			firstHit = i + 1
		}
		if i < 10 {
			dcg += 1 / math.Log2(float64(i+2))
		}
	}
	t.recall += float64(found) / float64(len(expect))
	if firstHit > 0 && firstHit <= 10 {
		t.mrr += 1 / float64(firstHit)
	}
	idcg := 0.0
	for i := 0; i < min(len(expect), 10); i++ {
		idcg += 1 / math.Log2(float64(i+2))
	}
	t.ndcg += dcg / idcg
}

func (t *tally) String() string {
	if t.n == 0 {
		return "n=0"
	}
	n := float64(t.n)
	return fmt.Sprintf("n=%-4d recall@k=%.3f  mrr@10=%.3f  ndcg@10=%.3f", t.n, t.recall/n, t.mrr/n, t.ndcg/n)
}

func main() {
	goldPath := flag.String("goldset", "internal/platform/catalog/search/testdata/goldset_nl.jsonl", "goldset JSONL")
	k := flag.Int("k", 20, "cutoff for recall@k")
	depth := flag.Int("depth", 50, "candidates fetched per retriever")
	rrfK := flag.Int("rrf-k", catalogSearch.DefaultRRFK, "RRF k constant")
	bm25Weight := flag.Float64("bm25-weight", 1, "RRF weight of the BM25 list (the kNN list weighs 1)")
	model := flag.String("model", catalogSearch.DefaultEmbedModel, "embedding model id (must match the indexed vectors)")
	vectorIndex := flag.String("vector-index", catalogSearch.IndexWorkVectors, "work vector index uid to query")
	instruct := flag.Bool("instruct", true, "prefix queries with the retrieval instruction")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)
	rows, err := readGold(*goldPath)
	if err != nil {
		slog.Error("read goldset", "error", err)
		os.Exit(1)
	}
	idx, _, err := catalogSearch.NewIndexerFromConfig(cfg)
	if err != nil {
		slog.Error("search indexer", "error", err)
		os.Exit(1)
	}
	emb := catalogSearch.NewOpenAIEmbedder(cfg.AIUpstream.BaseURL, cfg.AIUpstream.Token, *model)
	ctx := context.Background()
	filter := spec.WorksFilter{OLang: spec.OLang{All: true}}

	modes := []string{"bm25", "knn", "rrf"}
	overall := map[string]*tally{}
	by := map[string]*tally{}
	get := func(m map[string]*tally, key string) *tally {
		if m[key] == nil {
			m[key] = &tally{}
		}
		return m[key]
	}
	for _, r := range rows {
		res, err := idx.SearchWorks(ctx, spec.WorksQuery{Q: r.Query, Filter: filter, Limit: *depth, SearchIntro: true})
		if err != nil {
			slog.Error("bm25", "query", r.Query, "error", err)
			os.Exit(1)
		}
		text := r.Query
		if *instruct {
			text = catalogSearch.WorkQueryText(r.Query)
		}
		vecs, err := emb.Embed(ctx, []string{text})
		if err != nil {
			slog.Error("embed query", "query", r.Query, "error", err)
			os.Exit(1)
		}
		hits, err := idx.SearchWorkVectors(ctx, *vectorIndex, vecs[0], *depth, filter)
		if err != nil {
			slog.Error("knn", "query", r.Query, "error", err)
			os.Exit(1)
		}
		knn := make([]int64, len(hits))
		for i, h := range hits {
			knn[i] = h.CatalogID
		}
		ranked := map[string][]int64{"bm25": res.IDs, "knn": knn, "rrf": catalogSearch.FuseRRF(*rrfK,
			catalogSearch.RankedList{IDs: res.IDs, Weight: *bm25Weight}, catalogSearch.RankedList{IDs: knn, Weight: 1})}
		for _, m := range modes {
			get(overall, m).add(ranked[m], r.Expect, *k)
			for _, dim := range []string{"lang=" + r.Lang, "kind=" + r.Kind, "tier=" + r.Tier} {
				get(by, m+" "+dim).add(ranked[m], r.Expect, *k)
			}
		}
	}
	fmt.Printf("goldset=%s queries=%d k=%d depth=%d rrf-k=%d bm25-weight=%g instruct=%v model=%s\n", *goldPath, len(rows), *k, *depth, *rrfK, *bm25Weight, *instruct, *model)
	for _, m := range modes {
		fmt.Printf("%-5s %s\n", m, overall[m])
	}
	keys := make([]string, 0, len(by))
	for key := range by {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-22s %s\n", key, by[key])
	}
}

func readGold(path string) ([]goldRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []goldRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r goldRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("goldset line %d: %w", len(rows)+1, err)
		}
		if r.Query == "" || len(r.Expect) == 0 {
			return nil, fmt.Errorf("goldset line %d: query and expect are required", len(rows)+1)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}
