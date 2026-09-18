package importer

import (
	"fmt"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func (im *Importer) ReportDLsiteGamesHoldout(dlsiteDB *gorm.DB) (DLsiteHoldoutReport, error) {
	var rep DLsiteHoldoutReport
	snap, err := im.loadDLsiteGamesSnap(dlsiteDB)
	if err != nil {
		return rep, err
	}
	truth, err := im.loadDLHoldoutTruth()
	if err != nil {
		return rep, err
	}
	ruleN := map[string][2]int{}
	for workno, workID := range truth {
		p, ok := snap.byWorkno[workno]
		if !ok {
			continue
		}
		rep.N++
		g := dlGameGroup{worknos: []string{workno}, pop: []dlGameProd{p}}
		declared := snap.declaredWorks(g)
		var action string
		var rule string
		var attachID int64
		switch {
		case len(declared) == 1:
			action, rule, attachID = "attach", ruleDLsiteBgmXlink, declared[0]
		case len(declared) > 1:
			action = "quarantine"
		default:
			hits := snap.titleHits(g)
			switch {
			case len(hits) == 1:
				if r, ok := snap.corroborated(g, hits[0]); ok {
					action, rule, attachID = "attach", r, hits[0]
				} else {
					action = "quarantine"
				}
			case len(hits) > 1:
				action = "quarantine"
			default:
				action = "mint"
			}
		}
		switch action {
		case "attach":
			pair := ruleN[rule]
			if attachID == workID {
				rep.AttachCorrect++
				pair[0]++
			} else {
				rep.AttachWrong++
				pair[1]++
				rep.Wrong = append(rep.Wrong, DLsiteGamesReceipt{
					Action: "holdout_wrong", Rule: rule, Worknos: []string{workno}, WorkID: attachID,
				})
			}
			ruleN[rule] = pair
		case "quarantine":
			rep.Quarantined++
		default:
			rep.Minted++
		}
	}
	var rules []string
	for r := range ruleN {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	for _, r := range rules {
		pair := ruleN[r]
		rep.ByRule = append(rep.ByRule, DLsiteHoldoutRule{Rule: r, Correct: pair[0], Wrong: pair[1]})
	}
	return rep, nil
}

func (rep DLsiteHoldoutReport) Lines() []string {
	out := []string{fmt.Sprintf("holdout n=%d attach_correct=%d attach_wrong=%d quarantined=%d minted=%d",
		rep.N, rep.AttachCorrect, rep.AttachWrong, rep.Quarantined, rep.Minted)}
	for _, r := range rep.ByRule {
		out = append(out, fmt.Sprintf("holdout_rule rule=%s correct=%d wrong=%d", r.Rule, r.Correct, r.Wrong))
	}
	return out
}

func (im *Importer) loadDLHoldoutTruth() (map[string]int64, error) {
	var rows []struct {
		Workno string `gorm:"column:external_id"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id, rel.work_id
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_external_ref v ON v.entity_type = ? AND v.entity_id = w.id
			AND v.source_id = ? AND v.link_kind = ? AND v.dead_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeWork, vndbSource, model.LinkKindExact,
		model.EntityTypeRelease, dlsiteSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load holdout truth: %w", err)
	}
	out := map[string]int64{}
	for _, r := range rows {
		if _, ok := out[r.Workno]; !ok {
			out[r.Workno] = r.WorkID
		}
	}
	return out, nil
}
