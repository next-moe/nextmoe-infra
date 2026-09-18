package egworks

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"api/internal/jobs/workplatforms"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyMints(ctx context.Context, db *gorm.DB, ids registryIDs, snap snapshot, mints []plannedAction) (touched []int64, written, errors int) {
	for start := 0; start < len(mints); start += writeChunk {
		end := start + writeChunk
		if end > len(mints) {
			end = len(mints)
		}
		chunk := mints[start:end]
		var chunkTouched []int64
		var chunkWritten int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var n int
			var hosts []int64
			var err error
			hosts, n, err = createMintChunk(tx, ids, snap, chunk)
			if err != nil {
				return err
			}
			chunkWritten = n
			chunkTouched = hosts
			return nil
		})
		if err != nil {
			errors++
			slog.Warn("egworks mint chunk", "start", start, "err", err)
			continue
		}
		written += chunkWritten
		touched = append(touched, chunkTouched...)
	}
	return touched, written, errors
}

func createMintChunk(tx *gorm.DB, ids registryIDs, snap snapshot, chunk []plannedAction) ([]int64, int, error) {
	works := make([]model.CatalogWork, len(chunk))
	for i, p := range chunk {
		status := model.WorkStatusLive
		if p.Quarantine {
			status = model.WorkStatusQuarantine
		}
		rating := model.ContentRatingAllAges
		if p.Primary.Erogame {
			rating = model.ContentRatingR18
		}
		works[i] = model.CatalogWork{
			MediumID: ids.galgame, OLang: model.OLangDefault, DisplayName: p.Primary.Gamename,
			ContentRating: rating, Status: status,
			Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
		}
	}
	if err := tx.CreateInBatches(works, 1000).Error; err != nil {
		return nil, 0, err
	}

	var titles []model.CatalogWorkTitle
	releases := make([]model.CatalogRelease, len(chunk))
	for i, p := range chunk {
		wid := works[i].ID
		titles = append(titles, model.CatalogWorkTitle{
			WorkID: wid, Lang: "ja", Title: p.Primary.Gamename, Kind: model.WorkTitleKindOfficial,
		})
		if p.Primary.Furigana != "" && p.Primary.Furigana != p.Primary.Gamename {
			titles = append(titles, model.CatalogWorkTitle{
				WorkID: wid, Lang: "ja", Title: p.Primary.Furigana, Kind: model.WorkTitleKindSearchHint,
			})
		}
		y, m, d := parseSellday(p.Primary.Sellday, snap.now)
		rel := model.CatalogRelease{
			WorkID: wid, Kind: model.ReleaseKindDefault,
			ReleasedY: y, ReleasedM: m, ReleasedD: d,
			Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
		}
		if code := workplatforms.Normalize(p.Primary.Model, snap.registry); code != "" {
			c := code
			rel.Platform = &c
		}
		releases[i] = rel
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return nil, 0, err
	}

	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	var platforms []model.CatalogWorkPlatform
	var edges []model.CatalogWorkLabel
	var cands []model.CatalogMatchCandidate
	src := ids.eg
	written := 0
	hosts := make([]int64, 0, len(chunk))
	titlesByWork := map[int64][]model.CatalogWorkTitle{}
	for _, t := range titles {
		titlesByWork[t.WorkID] = append(titlesByWork[t.WorkID], t)
	}
	for i, p := range chunk {
		wid := works[i].ID
		hosts = append(hosts, wid)
		refs = append(refs, model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: wid, SourceID: ids.eg,
			ExternalID: strconv.FormatInt(p.Primary.ID, 10),
			LinkKind:   model.LinkKindExact, MatchedBy: ruleWorkImport,
		})
		written++
		for _, f := range p.Folded {
			refs = append(refs, model.CatalogExternalRef{
				EntityType: model.EntityTypeWork, EntityID: wid, SourceID: ids.eg,
				ExternalID: strconv.FormatInt(f.ID, 10),
				LinkKind:   model.LinkKindRelated, MatchedBy: ruleEdition,
			})
			written++
		}
		revs = append(revs,
			importedRev(model.EntityTypeWork, wid, workSnapshotJSON(works[i], titlesByWork[wid])),
			importedRev(model.EntityTypeRelease, releases[i].ID, releaseSnapshotJSON(releases[i])),
		)
		if code := workplatforms.Normalize(p.Primary.Model, snap.registry); code != "" {
			platforms = append(platforms, model.CatalogWorkPlatform{
				WorkID: wid, Platform: code, SourceID: ids.eg,
			})
		}
		if p.Primary.BrandID != 0 {
			if lid, ok := snap.brandLabel[p.Primary.BrandID]; ok {
				edges = append(edges, model.CatalogWorkLabel{
					WorkID: wid, LabelID: lid, Kind: model.WorkLabelKindDeveloper, SourceID: &src,
				})
			}
		}
		if p.Quarantine {
			for _, hit := range p.Hits {
				a, b := wid, hit
				if b < a {
					a, b = b, a
				}
				cands = append(cands, model.CatalogMatchCandidate{
					EntityType: model.EntityTypeWork, AID: a, BID: b,
					Reason: model.CandidateReasonNameNormEqual,
					Status: model.CandidateStatusPending,
				})
			}
		}
	}
	if err := tx.CreateInBatches(refs, 1000).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.CreateInBatches(revs, 1000).Error; err != nil {
		return nil, 0, err
	}
	if len(platforms) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&platforms).Error; err != nil {
			return nil, 0, err
		}
	}
	if len(edges) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&edges).Error; err != nil {
			return nil, 0, err
		}
	}
	if len(cands) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
			return nil, 0, err
		}
	}
	return hosts, written, nil
}

func parseSellday(s string, now time.Time) (y, m, d *int16) {
	if s == "" {
		return nil, nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, nil, nil
	}
	// EG writes placeholder far-future dates for unannounced releases.
	if !t.After(now.AddDate(2, 0, 0)) {
		yy, mm, dd := int16(t.Year()), int16(t.Month()), int16(t.Day())
		return &yy, &mm, &dd
	}
	return nil, nil, nil
}

func importedRev(etype int16, id int64, snap datatypes.JSON) model.CatalogRevision {
	return model.CatalogRevision{
		EntityType: etype, EntityID: id, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: snap, IsMinor: false,
	}
}

func workSnapshotJSON(w model.CatalogWork, titles []model.CatalogWorkTitle) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"work": w, "titles": titles})
	return b
}

func releaseSnapshotJSON(r model.CatalogRelease) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"release": r})
	return b
}
