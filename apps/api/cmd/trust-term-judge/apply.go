package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/platform/trust/model"
	"api/internal/platform/trust/service"

	"gorm.io/gorm"
)

type retired struct {
	ID   int64   `json:"id"`
	Term string  `json:"term"`
	Note *string `json:"note,omitempty"`
}

func runApply(db *gorm.DB, verdictsPath, backupPath, modelID string, apply bool) error {
	all, err := readJSONL[verdict](verdictsPath)
	if err != nil {
		return err
	}
	doomed, skipped, err := selectRetired(db, all)
	if err != nil {
		return err
	}
	fmt.Printf("\nVerdicts: %d judged, %d retire\n", len(all), len(doomed)+skipped)
	fmt.Printf("Skipped:  %d no longer an active abuse-purpose suspect term under the judged spelling\n", skipped)
	fmt.Printf("Would retire %d terms.\n\n", len(doomed))
	if len(doomed) == 0 {
		return nil
	}
	if backupPath != "" {
		b, err := json.MarshalIndent(doomed, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(backupPath, b, 0o644); err != nil {
			return err
		}
		slog.Info("backup written", "path", backupPath, "terms", len(doomed))
	}
	if !apply {
		slog.Info("dry run — nothing written; re-run with -apply to retire these terms")
		return nil
	}
	if err := deprecate(db, doomed, modelID, len(all)); err != nil {
		return err
	}
	slog.Info("terms retired", "count", len(doomed))
	return nil
}

func selectRetired(db *gorm.DB, all []verdict) (doomed []retired, skipped int, err error) {
	want := map[int64]verdict{}
	for _, v := range all {
		if v.Verdict == verdictRetire {
			want[v.ID] = v
		}
	}
	ids := make([]int64, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	const batch = 1000
	for start := 0; start < len(ids); start += batch {
		var terms []model.TrustTerm
		if err := db.Where("id IN ?", ids[start:min(start+batch, len(ids))]).
			Where("is_deprecated = false AND kind = ? AND purpose = ?", model.TermKindSuspect, model.TermPurposeAbuse).
			Order("id").Find(&terms).Error; err != nil {
			return nil, 0, err
		}
		for _, t := range terms {
			if v := want[t.ID]; v.Term == t.TermNorm {
				doomed = append(doomed, retired{ID: t.ID, Term: t.TermNorm, Note: t.Note})
			}
		}
	}
	return doomed, len(want) - len(doomed), nil
}

func deprecate(db *gorm.DB, doomed []retired, modelID string, judged int) error {
	policy := fmt.Sprintf("trust-term-judge model=%q judged=%d terms=%d at=%s",
		modelID, judged, len(doomed), time.Now().UTC().Format(time.RFC3339))
	return db.Transaction(func(tx *gorm.DB) error {
		const batch = 1000
		for start := 0; start < len(doomed); start += batch {
			ids := make([]int64, 0, batch)
			for _, d := range doomed[start:min(start+batch, len(doomed))] {
				ids = append(ids, d.ID)
			}
			if err := tx.Model(&model.TrustTerm{}).Where("id IN ?", ids).
				Update("is_deprecated", true).Error; err != nil {
				return err
			}
		}
		return service.AppendAudit(tx, service.AuditEntry{
			Action:    "terms_pruned_by_llm",
			PolicyRef: &policy,
		})
	})
}
