package creditsplit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Opts struct {
	Apply        bool
	DSN          string
	WorklistPath string
	ReceiptsPath string
	ActorID      *int64
	Limit        int
}

type Stats struct {
	Rows            int
	Skipped         int
	Refused         int
	WouldDropAnchor int
	WouldUnlink     int
	AnchorsDropped  int
	PersonsUnlinked int
	Revisions       int
	Refusals        []Refusal
	Receipts        []Receipt
}

type Refusal struct {
	CreditNameID int64  `json:"credit_name_id"`
	Reason       string `json:"reason"`
}

type Receipt struct {
	CreditNameID   int64    `json:"credit_name_id"`
	Name           string   `json:"name"`
	DetachSources  []string `json:"detach_sources"`
	DroppedAnchors []Anchor `json:"dropped_anchors"`
	PersonUnlinked *int64   `json:"person_unlinked,omitempty"`
}

type Anchor struct {
	SourceID   int16  `json:"source_id"`
	SourceKey  string `json:"source_key"`
	ExternalID string `json:"external_id"`
	LinkKind   int16  `json:"link_kind"`
	MatchedBy  string `json:"matched_by"`
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
	}
	rows, err := LoadWorklist(opts.WorklistPath)
	if err != nil {
		return nil, err
	}
	if opts.Limit > 0 && opts.Limit < len(rows) {
		rows = rows[:opts.Limit]
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	sources, err := loadSources(db)
	if err != nil {
		return nil, err
	}

	st := &Stats{Rows: len(rows)}
	for _, r := range rows {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := splitOne(ctx, db, sources, r, opts, st); err != nil {
			return nil, err
		}
	}
	slog.Info("credit-split done", "apply", opts.Apply, "rows", st.Rows,
		"skipped", st.Skipped, "refused", st.Refused,
		"would_drop_anchor", st.WouldDropAnchor, "would_unlink", st.WouldUnlink,
		"anchors_dropped", st.AnchorsDropped, "persons_unlinked", st.PersonsUnlinked,
		"revisions", st.Revisions)
	return st, nil
}

func splitOne(ctx context.Context, db *gorm.DB, sources map[string]int16, r Row, opts Opts, st *Stats) error {
	wanted := make([]int16, 0, len(r.DetachSources))
	for _, key := range r.DetachSources {
		id, ok := sources[key]
		if !ok {
			st.Refused++
			st.Refusals = append(st.Refusals, Refusal{r.CreditNameID, fmt.Sprintf("unknown source key %q", key)})
			return nil
		}
		wanted = append(wanted, id)
	}
	sort.Slice(wanted, func(i, j int) bool { return wanted[i] < wanted[j] })

	var cn model.CatalogCreditName
	if err := db.WithContext(ctx).First(&cn, r.CreditNameID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			st.Refused++
			st.Refusals = append(st.Refusals, Refusal{r.CreditNameID, "credit_name no longer exists (merged away?)"})
			return nil
		}
		return err
	}
	var refs []model.CatalogExternalRef
	if err := db.WithContext(ctx).Where("entity_type = ? AND entity_id = ?",
		model.EntityTypeCreditName, r.CreditNameID).Order("source_id, external_id").Find(&refs).Error; err != nil {
		return err
	}

	var drop []model.CatalogExternalRef
	for _, ref := range refs {
		for _, id := range wanted {
			if ref.SourceID == id {
				drop = append(drop, ref)
			}
		}
	}
	if len(drop) > 0 && len(drop) == len(refs) {
		st.Refused++
		st.Refusals = append(st.Refusals, Refusal{r.CreditNameID,
			"detaching every anchor would leave the name sourceless — refusing"})
		return nil
	}
	unlink := cn.PersonID != nil
	if len(drop) == 0 && !unlink {
		st.Skipped++
		return nil
	}
	st.WouldDropAnchor += len(drop)
	if unlink {
		st.WouldUnlink++
	}

	rec := Receipt{CreditNameID: cn.ID, Name: cn.Name, DetachSources: r.DetachSources}
	for _, ref := range drop {
		rec.DroppedAnchors = append(rec.DroppedAnchors, Anchor{
			SourceID: ref.SourceID, SourceKey: sourceKey(sources, ref.SourceID),
			ExternalID: ref.ExternalID, LinkKind: ref.LinkKind, MatchedBy: ref.MatchedBy,
		})
	}
	if unlink {
		rec.PersonUnlinked = cn.PersonID
	}
	st.Receipts = append(st.Receipts, rec)
	if !opts.Apply {
		return nil
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		snap, err := json.Marshal(map[string]any{
			"credit_name": cn,
			"anchors":     refs,
		})
		if err != nil {
			return err
		}
		for _, ref := range drop {
			res := tx.Exec(`DELETE FROM catalog_external_ref
			                 WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
				model.EntityTypeCreditName, cn.ID, ref.SourceID, ref.ExternalID)
			if res.Error != nil {
				return res.Error
			}
			st.AnchorsDropped += int(res.RowsAffected)
		}
		if unlink {
			res := tx.Exec(`UPDATE catalog_credit_name SET person_id = NULL, updated_at = now()
			                 WHERE id = ? AND person_id IS NOT NULL
			                   AND `+editspec.NotHumanFieldSQL("field_provenance", "person_id"), cn.ID)
			if res.Error != nil {
				return res.Error
			}
			st.PersonsUnlinked += int(res.RowsAffected)
		}
		changed, err := json.Marshal(map[string]any{
			"detached_sources": r.DetachSources,
			"person_id":        unlink,
			"reason":           r.Reason,
		})
		if err != nil {
			return err
		}
		rev, err := repository.NextRevision(tx, model.EntityTypeCreditName, cn.ID)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.CatalogRevision{
			EntityType:    model.EntityTypeCreditName,
			EntityID:      cn.ID,
			Revision:      rev,
			Action:        model.RevisionActionSplit,
			Snapshot:      datatypes.JSON(snap),
			ChangedFields: datatypes.JSON(changed),
			ActorID:       opts.ActorID,
			Note:          "wave 156 credit-name split: " + r.Reason,
		}).Error; err != nil {
			return err
		}
		st.Revisions++
		return nil
	})
}

func loadSources(db *gorm.DB) (map[string]int16, error) {
	var rows []model.CatalogSource
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int16, len(rows))
	for _, r := range rows {
		out[r.Key] = r.ID
	}
	if id, ok := out["erogamescape"]; ok {
		out["eg"] = id
	}
	if id, ok := out["bangumi"]; ok {
		out["bgm"] = id
	}
	return out, nil
}

func sourceKey(sources map[string]int16, id int16) string {
	for k, v := range sources {
		if v == id && k != "eg" && k != "bgm" {
			return k
		}
	}
	return fmt.Sprintf("source#%d", id)
}
