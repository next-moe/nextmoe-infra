package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/trust/model"
	"api/internal/platform/trust/service"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

type termStat struct {
	ID        int64   `json:"id"`
	Term      string  `json:"term"`
	Kind      int16   `json:"kind"`
	Purpose   int16   `json:"purpose"`
	Site      *string `json:"site,omitempty"`
	Note      *string `json:"note,omitempty"`
	Hits      int64   `json:"hits"`
	Flagged   int64   `json:"flagged"`
	Precision float64 `json:"precision"`
	Reason    string  `json:"reason"`
}

func main() {
	source := flag.String("source", "", "only consider terms whose note contains this substring")
	minHits := flag.Int64("min-hits", 20, "matches required before a term's precision is trusted")
	maxPrecision := flag.Float64("max-precision", 0.10, "deprecate evidenced terms scoring below this")
	dropUnevidenced := flag.Bool("drop-unevidenced", false, "also deprecate terms below -min-hits matches")
	backup := flag.String("backup", "", "write the affected set to this JSON file (required with -apply)")
	apply := flag.Bool("apply", false, "write to the database (default: dry-run report)")
	flag.Parse()

	if *apply && *backup == "" {
		fmt.Fprintln(os.Stderr, "-apply requires -backup: deprecation is terminal and must be recoverable")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)
	slog.Info("connecting to trust database", "dbname", cfg.TrustDatabase.DBName)

	db, err := database.NewPostgresDB(cfg.TrustDatabase)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	stats, corpus, err := collect(db.DB(), *source)
	if err != nil {
		slog.Error("collecting term statistics", "error", err)
		os.Exit(1)
	}

	doomed, review := classify(stats, *minHits, *maxPrecision, *dropUnevidenced)
	report(stats, doomed, review, corpus, *minHits, *maxPrecision)

	if len(doomed) == 0 {
		slog.Info("nothing to retire under this policy")
		return
	}
	if *backup != "" {
		if err := writeBackup(*backup, doomed); err != nil {
			slog.Error("writing backup", "error", err)
			os.Exit(1)
		}
		slog.Info("backup written", "path", *backup, "terms", len(doomed))
	}
	if !*apply {
		slog.Info("dry run — nothing written; re-run with -apply to retire these terms")
		return
	}
	if err := deprecate(db.DB(), doomed, *source, *minHits, *maxPrecision, *dropUnevidenced); err != nil {
		slog.Error("deprecating terms", "error", err)
		os.Exit(1)
	}
	slog.Info("terms retired", "count", len(doomed))
}

func collect(db *gorm.DB, source string) ([]termStat, int64, error) {
	var corpus int64
	if err := db.Model(&model.TrustScanResult{}).
		Where("tier0_matched IS NOT NULL").Count(&corpus).Error; err != nil {
		return nil, 0, err
	}

	const q = `
		WITH matched AS (
		    SELECT coalesce(flagged, false) AS flagged,
		           jsonb_array_elements_text(tier0_matched) AS term
		      FROM trust_scan_result
		     WHERE tier0_matched IS NOT NULL
		       AND jsonb_array_length(tier0_matched) > 0
		), per_term AS (
		    SELECT term,
		           count(*) AS hits,
		           count(*) FILTER (WHERE flagged) AS flagged
		      FROM matched GROUP BY term
		)
		SELECT t.id, t.term_norm AS term, t.kind, t.purpose, t.site, t.note,
		       coalesce(p.hits, 0) AS hits,
		       coalesce(p.flagged, 0) AS flagged
		  FROM trust_term t
		  LEFT JOIN per_term p ON p.term = t.term_norm
		 WHERE t.is_deprecated = false
		   AND (? = '' OR coalesce(t.note, '') LIKE '%' || ? || '%')
		 ORDER BY hits DESC, t.term_norm ASC`

	var stats []termStat
	if err := db.Raw(q, source, source).Scan(&stats).Error; err != nil {
		return nil, 0, err
	}
	for i := range stats {
		if stats[i].Hits > 0 {
			stats[i].Precision = float64(stats[i].Flagged) / float64(stats[i].Hits)
		}
	}
	return stats, corpus, nil
}

func classify(stats []termStat, minHits int64, maxPrecision float64, dropUnevidenced bool) (doomed, review []termStat) {
	for _, s := range stats {
		var reason string
		switch {
		case s.Hits >= minHits && s.Precision < maxPrecision:
			reason = fmt.Sprintf("measured: %d hits, %d flagged, precision %.4f < %.4f",
				s.Hits, s.Flagged, s.Precision, maxPrecision)
		case s.Hits < minHits && dropUnevidenced:
			reason = fmt.Sprintf("unevidenced: %d hits (< %d), never demonstrated value",
				s.Hits, minHits)
		default:
			continue
		}
		s.Reason = reason
		if s.Purpose == model.TermPurposeCompliance {
			review = append(review, s)
			continue
		}
		doomed = append(doomed, s)
	}
	return doomed, review
}

func report(stats, doomed, review []termStat, corpus int64, minHits int64, maxPrecision float64) {
	var fired, totalHits, totalFlagged int64
	for _, s := range stats {
		if s.Hits > 0 {
			fired++
			totalHits += s.Hits
			totalFlagged += s.Flagged
		}
	}

	fmt.Printf("\nCorpus:      %d scans with an evaluated Tier0 record\n", corpus)
	fmt.Printf("Active terms: %d considered, %d ever fired, %d never fired\n",
		len(stats), fired, int64(len(stats))-fired)
	if totalHits > 0 {
		fmt.Printf("Aggregate:   %d matches, %d on flagged content (precision %.4f)\n",
			totalHits, totalFlagged, float64(totalFlagged)/float64(totalHits))
	}
	fmt.Printf("Policy:      deprecate if hits >= %d and precision < %.4f\n\n", minHits, maxPrecision)

	loud := make([]termStat, len(stats))
	copy(loud, stats)
	sort.Slice(loud, func(i, j int) bool { return loud[i].Hits > loud[j].Hits })
	fmt.Println("Loudest terms (hits / flagged / precision):")
	for i, s := range loud {
		if i >= 20 || s.Hits == 0 {
			break
		}
		verdict := "KEEP"
		if s.Hits >= minHits && s.Precision < maxPrecision {
			verdict = "RETIRE"
		}
		fmt.Printf("  %-24s %7d %7d  %6.2f%%  %s\n",
			s.Term, s.Hits, s.Flagged, s.Precision*100, verdict)
	}
	if len(review) > 0 {
		fmt.Printf("\nCompliance terms failing the policy (NOT retired — a human decides):\n")
		for _, s := range review {
			fmt.Printf("  %-24s %7d %7d  %6.2f%%  %s\n",
				s.Term, s.Hits, s.Flagged, s.Precision*100, noteOf(s))
		}
		fmt.Println("  (the abuse classifier does not judge compliance content; this number" +
			"\n   cannot convict these terms, only nominate them for review)")
	}

	fmt.Printf("\nWould retire %d of %d terms.\n\n", len(doomed), len(stats))
}

func noteOf(s termStat) string {
	if s.Note == nil {
		return ""
	}
	return *s.Note
}

func writeBackup(path string, doomed []termStat) error {
	b, err := json.MarshalIndent(doomed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func deprecate(db *gorm.DB, doomed []termStat, source string, minHits int64, maxPrecision float64, dropUnevidenced bool) error {
	ids := make([]int64, len(doomed))
	for i, s := range doomed {
		ids[i] = s.ID
	}
	policy := fmt.Sprintf(
		"trust-term-prune source=%q min_hits=%d max_precision=%.4f drop_unevidenced=%t terms=%d at=%s",
		source, minHits, maxPrecision, dropUnevidenced, len(ids), time.Now().UTC().Format(time.RFC3339))

	return db.Transaction(func(tx *gorm.DB) error {
		const batch = 1000
		for start := 0; start < len(ids); start += batch {
			end := min(start+batch, len(ids))
			if err := tx.Model(&model.TrustTerm{}).
				Where("id IN ?", ids[start:end]).
				Update("is_deprecated", true).Error; err != nil {
				return err
			}
		}
		return service.AppendAudit(tx, service.AuditEntry{
			Action:    "terms_pruned_by_precision",
			PolicyRef: &policy,
		})
	})
}
