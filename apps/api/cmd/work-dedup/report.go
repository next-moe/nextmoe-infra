package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
)

func runCensus(ctx context.Context, db *gorm.DB, w io.Writer, csvPath string) error {
	c, err := buildCensus(ctx, db)
	if err != nil {
		return err
	}
	printCensus(c, w)
	if csvPath != "" {
		if err := exportCSV(c, csvPath); err != nil {
			return err
		}
		fmt.Fprintf(w, "dossier: %s (%d pairs)\n", csvPath, len(c.rows))
	}
	return nil
}

func printCensus(c *census, w io.Writer) {
	buckets := map[bucket]int{}
	lanes := map[string]int{}
	for i, r := range c.rows {
		buckets[c.verdicts[i]]++
		if c.verdicts[i] != bucketOutOfScope {
			la, lb := r.LaneA, r.LaneB
			if lb < la {
				la, lb = lb, la
			}
			lanes[la+"×"+lb]++
		}
	}
	fmt.Fprintf(w, "[census] pairs=%d in_scope=%d\n", len(c.rows), len(c.rows)-buckets[bucketOutOfScope])
	for _, b := range []bucket{bucketAuto, bucketAnchorConflict, bucketRelConflict, bucketDateClash, bucketBare, bucketBothKungal, bucketHiddenClaim, bucketBridged, bucketRefCI, bucketAliasOnly, bucketOutOfScope} {
		fmt.Fprintf(w, "  %-16s %d\n", b, buckets[b])
	}
	fmt.Fprintf(w, "  merge groups: %d\n", len(c.groups))
	names := make([]string, 0, len(lanes))
	for k := range lanes {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return lanes[names[i]] > lanes[names[j]] })
	for _, k := range names {
		fmt.Fprintf(w, "  lane %-18s %d\n", k, lanes[k])
	}
	var both []pairRow
	for i, r := range c.rows {
		if c.verdicts[i] == bucketBothKungal {
			both = append(both, r)
		}
	}
	const bothKungalSamples = 50
	site := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for i, r := range both {
		if i >= bothKungalSamples {
			fmt.Fprintf(w, "  … and %d more\n", len(both)-bothKungalSamples)
			break
		}
		fmt.Fprintf(w, "  both-kungal: %d %d %q %q %q %q\n",
			r.A, r.B, r.NameA, r.NameB, site(r.SiteA), site(r.SiteB))
	}
	for i, g := range c.groups {
		if i >= 5 {
			break
		}
		fmt.Fprintf(w, "  group sample: %v -> %d (%s)\n", g.sources, g.survivor, g.sample)
	}
}

func exportCSV(c *census, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	if err := cw.Write([]string{
		"a", "b", "bucket", "lane_a", "lane_b", "site_a", "site_b", "name_a", "name_b",
		"shared_norm", "shared_norms", "anchor_conflict", "release_conflict", "ref_overlap",
		"date_a", "date_b", "label_overlap", "anchors_a", "anchors_b",
		"shared_official", "ref_overlap_ci",
	}); err != nil {
		return err
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for i, r := range c.rows {
		rec := []string{
			strconv.FormatInt(r.A, 10), strconv.FormatInt(r.B, 10), string(c.verdicts[i]),
			r.LaneA, r.LaneB, str(r.SiteA), str(r.SiteB), r.NameA, r.NameB,
			r.SharedNorm, strconv.Itoa(r.SharedNorms),
			strconv.FormatBool(r.AnchorConflict), strconv.FormatBool(r.RelConflict), strconv.FormatBool(r.RefOverlap),
			dateStr(r.DateA), dateStr(r.DateB),
			strconv.FormatBool(r.LabelOverlap), strconv.Itoa(r.AnchorsA), strconv.Itoa(r.AnchorsB),
			strconv.Itoa(r.SharedOfficial), strconv.FormatBool(r.RefOverlapCI),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func dateStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
