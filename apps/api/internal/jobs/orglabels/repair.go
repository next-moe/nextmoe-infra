package orglabels

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ruleRepairProbable = "rule:anchor-audit-promoted"
	ruleRepairName     = "rule:anchor-audit-name"
	ruleRepairNew      = "rule:anchor-audit-new"
	ruleRepairManual   = "rule:anchor-audit-manual"
)

type RepairDecision struct {
	Action  string `json:"action,omitempty"`
	LabelID int64  `json:"label_id"`
	Source  int16  `json:"src"`
	ExtID   string `json:"ext"`
	Reason  string `json:"reason"`
	Target  int64  `json:"target_label_id,omitempty"`
	// TargetRef sends the entry to whichever label holds this other anchor exactly
	// once the earlier decisions have run, so two wrong anchors that are one
	// organisation land on one label instead of minting two.
	TargetRef *struct {
		Source int16  `json:"src"`
		ExtID  string `json:"ext"`
	} `json:"target_ref,omitempty"`
	// Mint skips the probable-holder and same-name steps. A homonym re-homed by
	// name lands on whichever other label carries that name, and on 2026-11-05
	// that was the wrong company again for 8 of 10 homonyms: ErogameScape's 2008
	// ALIBI went from one アリバイ label onto its duplicate, the rock band NAZCA
	// onto the doujin circle Nasska, the folk duo ゆず onto ももや本舗.
	Mint bool `json:"mint,omitempty"`
}

type RepairResult struct {
	RepairDecision
	Status       string `json:"status"`
	TargetID     int64  `json:"target"`
	TargetHow    string `json:"target_how"`
	ReleaseRows  int64  `json:"release_rows"`
	WorkRows     int64  `json:"work_rows"`
	Credits      int64  `json:"credits"`
	Intros       int64  `json:"intros"`
	Aliases      int64  `json:"aliases"`
	Links        int64  `json:"links"`
	LogoCleared  bool   `json:"logo_cleared"`
	IdentityFrom bool   `json:"identity_from_source"`
	Err          string `json:"error,omitempty"`
}

type repairPools struct {
	catalog, eg, dlsite *gorm.DB
}

func ReadRepairDecisions(r io.Reader) ([]RepairDecision, error) {
	var out []RepairDecision
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var d RepairDecision
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if d.LabelID == 0 || d.ExtID == "" || !repairableSource(d.Source) {
			return nil, fmt.Errorf("line %d: need label_id, ext and src in 2/3/4/5", n)
		}
		if d.Action == "" {
			d.Action = "detach"
		}
		if d.Action != "detach" && d.Action != "promote" {
			return nil, fmt.Errorf("line %d: action must be detach or promote", n)
		}
		out = append(out, d)
	}
	return out, sc.Err()
}

func repairableSource(s int16) bool {
	return s == sourceVNDB || s == sourceBangumi || s == sourceDlsite || s == sourceEG
}

func RunRepair(ctx context.Context, opts Opts, decisions []RepairDecision) ([]RepairResult, error) {
	catalog, eg, err := openPools(opts, true)
	if err != nil {
		return nil, err
	}
	dlDSN := opts.DlsiteDSN
	if dlDSN == "" {
		dlDSN = reDBName.ReplaceAllString(opts.DSN, "dbname=dlsite")
	}
	dl, err := openGorm(dlDSN)
	if err != nil {
		return nil, fmt.Errorf("open dlsite pool: %w", err)
	}
	return repairAll(ctx, repairPools{catalog, eg, dl}, decisions, opts.Apply)
}

var errDryRun = errors.New("dry run")

func repairAll(ctx context.Context, p repairPools, decisions []RepairDecision, apply bool) ([]RepairResult, error) {
	out := make([]RepairResult, 0, len(decisions))
	for _, d := range decisions {
		res := RepairResult{RepairDecision: d}
		var touched []int64
		err := p.catalog.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			w, err := repairOne(ctx, tx, p, &res)
			touched = w
			if err != nil {
				return err
			}
			if !apply {
				return errDryRun
			}
			return repository.TouchWorks(ctx, tx, touched)
		})
		switch {
		case errors.Is(err, errDryRun) || err == nil:
		case res.Status == "":
			res.Status, res.Err = "error", err.Error()
		}
		out = append(out, res)
	}
	return out, nil
}

func repairOne(ctx context.Context, tx *gorm.DB, p repairPools, res *RepairResult) ([]int64, error) {
	d := res.RepairDecision
	var kinds []int16
	if err := tx.Raw(`SELECT link_kind FROM catalog_external_ref
		WHERE entity_type = 3 AND entity_id = ? AND source_id = ? AND external_id = ? FOR UPDATE`,
		d.LabelID, d.Source, d.ExtID).Scan(&kinds).Error; err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		res.Status = "gone"
		return nil, nil
	}
	if d.Action == "promote" {
		return nil, promoteAnchor(tx, d, kinds[0], res)
	}

	org, err := loadRepairOrg(p, d.Source, d.ExtID)
	if err != nil {
		return nil, fmt.Errorf("load org: %w", err)
	}
	if err := tx.Exec(`DELETE FROM catalog_external_ref
		WHERE entity_type = 3 AND entity_id = ? AND source_id = ? AND external_id = ?`,
		d.LabelID, d.Source, d.ExtID).Error; err != nil {
		return nil, err
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeLabel, EntityID: d.LabelID, SourceID: d.Source, ExternalID: d.ExtID,
		Reason: "anchor audit 2026-11: " + d.Reason,
	}).Error; err != nil {
		return nil, err
	}

	target, how, err := resolveRepairTarget(tx, d, org)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}
	res.TargetID, res.TargetHow = target, how

	touched, err := moveDerived(tx, p, d, org, target, res)
	if err != nil {
		return nil, fmt.Errorf("move derived rows: %w", err)
	}
	if err := clearSourcedLogo(tx, d, res); err != nil {
		return nil, err
	}
	note := fmt.Sprintf("anchor audit: %s %s detached (%s)", srcKey(d.Source), d.ExtID, d.Reason)
	if target != 0 {
		note += fmt.Sprintf("; moved to label %d (%s)", target, how)
	}
	for _, id := range []int64{d.LabelID, target} {
		if id != 0 {
			if err := writeRepairRevision(tx, id, note); err != nil {
				return nil, err
			}
		}
	}
	res.Status = "detached"
	return touched, nil
}

func promoteAnchor(tx *gorm.DB, d RepairDecision, kind int16, res *RepairResult) error {
	if kind == model.LinkKindExact {
		res.Status = "already_exact"
		return nil
	}
	var holder []int64
	if err := tx.Raw(`SELECT entity_id FROM catalog_external_ref WHERE entity_type = 3 AND source_id = ?
		AND external_id = ? AND link_kind = 0`, d.Source, d.ExtID).Scan(&holder).Error; err != nil {
		return err
	}
	if len(holder) > 0 {
		res.Status, res.TargetID = "held_elsewhere", holder[0]
		return nil
	}
	if err := tx.Exec(`UPDATE catalog_external_ref SET link_kind = 0, matched_by = ?
		WHERE entity_type = 3 AND entity_id = ? AND source_id = ? AND external_id = ?`,
		ruleRepairProbable, d.LabelID, d.Source, d.ExtID).Error; err != nil {
		return err
	}
	res.Status = "promoted"
	return writeRepairRevision(tx, d.LabelID, fmt.Sprintf("anchor audit: %s %s promoted to exact (%s)", srcKey(d.Source), d.ExtID, d.Reason))
}

func resolveRepairTarget(tx *gorm.DB, d RepairDecision, org *repairOrg) (int64, string, error) {
	if d.TargetRef != nil {
		var holder []int64
		if err := tx.Raw(`SELECT r.entity_id FROM catalog_external_ref r
			JOIN catalog_label l ON l.id = r.entity_id AND l.deleted_at IS NULL
			WHERE r.entity_type = 3 AND r.source_id = ? AND r.external_id = ? AND r.link_kind = 0`,
			d.TargetRef.Source, d.TargetRef.ExtID).Scan(&holder).Error; err != nil {
			return 0, "", err
		}
		if len(holder) != 1 || holder[0] == d.LabelID {
			return 0, "", fmt.Errorf("target_ref %d/%s is held exactly by %v", d.TargetRef.Source, d.TargetRef.ExtID, holder)
		}
		d.Target = holder[0]
	}
	if d.Target == d.LabelID {
		return 0, "", fmt.Errorf("target is the label being repaired")
	}
	if d.Target != 0 {
		return d.Target, "manual", anchorRepairTarget(tx, d, d.Target, ruleRepairManual)
	}
	if d.Mint {
		return mintOrNone(tx, d, org)
	}
	var probable []int64
	if err := tx.Raw(`SELECT DISTINCT r.entity_id FROM catalog_external_ref r
		JOIN catalog_label l ON l.id = r.entity_id AND l.deleted_at IS NULL
		WHERE r.entity_type = 3 AND r.source_id = ? AND r.external_id = ? AND r.entity_id <> ?`,
		d.Source, d.ExtID, d.LabelID).Scan(&probable).Error; err != nil {
		return 0, "", err
	}
	if len(probable) == 1 {
		return probable[0], "promoted", anchorRepairTarget(tx, d, probable[0], ruleRepairProbable)
	}
	if org == nil {
		return 0, "none:no_org_data", nil
	}
	var named []int64
	if len(org.norms) > 0 {
		if err := tx.Raw(`SELECT DISTINCT l.id FROM catalog_label l
			LEFT JOIN catalog_label_alias a ON a.label_id = l.id
			WHERE l.deleted_at IS NULL AND l.id <> ?
			  AND (l.display_name_norm IN ? OR a.name_norm IN ?)
			  AND NOT EXISTS (SELECT 1 FROM catalog_external_ref x WHERE x.entity_type = 3
			      AND x.entity_id = l.id AND x.source_id = ? AND x.link_kind = 0)
			  AND NOT EXISTS (SELECT 1 FROM catalog_match_rejection j WHERE j.entity_type = 3
			      AND j.entity_id = l.id AND j.source_id = ? AND j.external_id = ?)`,
			d.LabelID, org.norms, org.norms, d.Source, d.Source, d.ExtID).Scan(&named).Error; err != nil {
			return 0, "", err
		}
	}
	if len(named) == 1 {
		return named[0], "name", anchorRepairTarget(tx, d, named[0], ruleRepairName)
	}
	if len(named) > 1 {
		return 0, fmt.Sprintf("none:ambiguous%v", named), nil
	}
	return mintOrNone(tx, d, org)
}

func mintOrNone(tx *gorm.DB, d RepairDecision, org *repairOrg) (int64, string, error) {
	if org == nil {
		return 0, "none:no_org_data", nil
	}
	if !org.canMint {
		return 0, "none:cannot_mint", nil
	}
	id, err := mintRepairLabel(tx, d, org)
	return id, "minted", err
}

func anchorRepairTarget(tx *gorm.DB, d RepairDecision, target int64, rule string) error {
	return tx.Exec(`INSERT INTO catalog_external_ref
		(entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
		VALUES (3, ?, ?, ?, 0, ?, now())
		ON CONFLICT (entity_type, entity_id, source_id, external_id)
		DO UPDATE SET link_kind = 0, matched_by = EXCLUDED.matched_by`,
		target, d.Source, d.ExtID, rule).Error
}

func mintRepairLabel(tx *gorm.DB, d RepairDecision, org *repairOrg) (int64, error) {
	label := model.CatalogLabel{
		DisplayName: org.name, Latin: org.latin, Lang: org.lang, Kind: org.kind,
		FieldProvenance: mintProvenance(d.Source, &orgRec{lang: org.lang, latin: org.latin}),
	}
	if err := tx.Create(&label).Error; err != nil {
		return 0, err
	}
	if err := anchorRepairTarget(tx, d, label.ID, ruleRepairNew); err != nil {
		return 0, err
	}
	return label.ID, tx.Create(&model.CatalogRevision{
		EntityType: model.EntityTypeLabel, EntityID: label.ID, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: labelSnapshot(label),
		Note: "anchor audit: minted for " + srcKey(d.Source) + " " + d.ExtID,
	}).Error
}

func writeRepairRevision(tx *gorm.DB, labelID int64, note string) error {
	var label model.CatalogLabel
	if err := tx.First(&label, labelID).Error; err != nil {
		return err
	}
	var aliases []model.CatalogLabelAlias
	if err := tx.Where("label_id = ?", labelID).Order("id").Find(&aliases).Error; err != nil {
		return err
	}
	snap, _ := json.Marshal(map[string]any{"label": label, "aliases": aliases})
	return tx.Exec(`INSERT INTO catalog_revision (entity_type, entity_id, revision, action, snapshot, is_minor, note, created_at)
		SELECT 3, ?, coalesce(max(revision), 0) + 1, ?, ?, false, ?, now()
		FROM catalog_revision WHERE entity_type = 3 AND entity_id = ?`,
		labelID, model.RevisionActionUpdated, string(snap), note, labelID).Error
}

func clearSourcedLogo(tx *gorm.DB, d RepairDecision, res *RepairResult) error {
	var prov struct {
		Logo     string `gorm:"column:logo"`
		Identity bool   `gorm:"column:identity"`
	}
	key := srcKey(d.Source)
	if err := tx.Raw(`SELECT field_provenance->'logo_hash'->0->>'source' AS logo,
		EXISTS (SELECT 1 FROM jsonb_each(field_provenance) e
		        WHERE e.key IN ('display_name','latin','kind') AND e.value->0->>'source' = ?) AS identity
		FROM catalog_label WHERE id = ?`, key, d.LabelID).Scan(&prov).Error; err != nil {
		return err
	}
	res.IdentityFrom = prov.Identity
	if prov.Logo != key {
		return nil
	}
	res.LogoCleared = true
	return tx.Exec(`UPDATE catalog_label SET logo_hash = '', field_provenance = field_provenance - 'logo_hash',
		updated_at = now() WHERE id = ?`, d.LabelID).Error
}
