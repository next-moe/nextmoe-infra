package service

import (
	"encoding/json"
	"fmt"

	"api/internal/platform/catalog/model"
	"api/internal/platform/provenance"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type fieldResolution map[string]string

func parseResolution(raw datatypes.JSON) (fieldResolution, error) {
	res := fieldResolution{}
	if len(raw) == 0 {
		return res, nil
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("parse field_resolution: %w", err)
	}
	return res, nil
}

func firstProvSource(prov datatypes.JSON, field string) string {
	return provenance.FirstSource(prov, field)
}

type fieldMerger struct {
	res     fieldResolution
	srcProv datatypes.JSON
	dstProv datatypes.JSON
	updates map[string]any
	changed map[string]any
}

func newFieldMerger(res fieldResolution, srcProv, dstProv datatypes.JSON) *fieldMerger {
	return &fieldMerger{res: res, srcProv: srcProv, dstProv: dstProv,
		updates: map[string]any{}, changed: map[string]any{}}
}

func (m *fieldMerger) take(field string, targetEmpty, sourceEmpty bool) bool {
	if sourceEmpty {
		return false
	}
	if !targetEmpty {
		if m.res[field] != "source" {
			return false
		}
		if provenance.IsHuman(firstProvSource(m.dstProv, field)) &&
			!provenance.IsHuman(firstProvSource(m.srcProv, field)) {
			return false
		}
	}
	return true
}

func (m *fieldMerger) str(field, column, dstVal, srcVal string) {
	if m.take(field, dstVal == "", srcVal == "") {
		m.updates[column] = srcVal
		m.changed[field] = srcVal
	}
}

func mergerPtr[T any](m *fieldMerger, field, column string, dstVal, srcVal *T) {
	if m.take(field, dstVal == nil, srcVal == nil) {
		m.updates[column] = *srcVal
		m.changed[field] = *srcVal
	}
}

func (m *fieldMerger) trio(field, prefix string, dst [3]*int16, src [3]*int16) {
	dstEmpty := dst[0] == nil && dst[1] == nil && dst[2] == nil
	srcEmpty := src[0] == nil && src[1] == nil && src[2] == nil
	if m.take(field, dstEmpty, srcEmpty) {
		m.updates[prefix+"_y"], m.updates[prefix+"_m"], m.updates[prefix+"_d"] = src[0], src[1], src[2]
		m.changed[field] = map[string]*int16{"y": src[0], "m": src[1], "d": src[2]}
	}
}

func (m *fieldMerger) apply(tx *gorm.DB, table string, targetID int64) error {
	if len(m.updates) == 0 {
		return nil
	}
	if prov, err := mergeProvenance(m.dstProv, m.srcProv, m.changed); err == nil && prov != nil {
		m.updates["field_provenance"] = prov
	} else if err != nil {
		return err
	}
	m.updates["updated_at"] = gorm.Expr("now()")
	return tx.Table(table).Where("id = ?", targetID).Updates(m.updates).Error
}

func mergeProvenance(dstProv, srcProv datatypes.JSON, changed map[string]any) (datatypes.JSON, error) {
	if len(changed) == 0 {
		return nil, nil
	}
	dst := map[string]json.RawMessage{}
	if len(dstProv) > 0 {
		if err := json.Unmarshal(dstProv, &dst); err != nil {
			return nil, fmt.Errorf("parse target provenance: %w", err)
		}
	}
	src := map[string]json.RawMessage{}
	if len(srcProv) > 0 {
		if err := json.Unmarshal(srcProv, &src); err != nil {
			return nil, fmt.Errorf("parse source provenance: %w", err)
		}
	}
	for field := range changed {
		if entry, ok := src[field]; ok {
			dst[field] = entry
		}
	}
	raw, err := json.Marshal(dst)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}

func applySurvivorship(tx *gorm.DB, entityType int16, src, dst int64, resolutionRaw datatypes.JSON) (map[string]any, error) {
	res, err := parseResolution(resolutionRaw)
	if err != nil {
		return nil, err
	}

	switch entityType {
	case model.EntityTypePerson:
		var s, d model.CatalogPerson
		if err := loadPair(tx, &s, &d, src, dst); err != nil {
			return nil, err
		}
		m := newFieldMerger(res, s.FieldProvenance, d.FieldProvenance)
		m.str("display_name", "display_name", d.DisplayName, s.DisplayName)
		m.str("description", "description", d.Description, s.Description)
		mergerPtr(m, "gender", "gender", d.Gender, s.Gender)
		m.trio("birth", "birth", [3]*int16{d.BirthY, d.BirthM, d.BirthD}, [3]*int16{s.BirthY, s.BirthM, s.BirthD})
		mergerPtr(m, "primary_credit_name_id", "primary_credit_name_id", d.PrimaryCreditNameID, s.PrimaryCreditNameID)
		return m.changed, m.apply(tx, "catalog_person", dst)

	case model.EntityTypeCreditName:
		var s, d model.CatalogCreditName
		if err := loadPair(tx, &s, &d, src, dst); err != nil {
			return nil, err
		}
		m := newFieldMerger(res, s.FieldProvenance, d.FieldProvenance)
		mergerPtr(m, "latin", "latin", d.Latin, s.Latin)
		m.str("lang", "lang", d.Lang, s.Lang)
		m.str("note", "note", d.Note, s.Note)
		return m.changed, m.apply(tx, "catalog_credit_name", dst)

	case model.EntityTypeLabel:
		var s, d model.CatalogLabel
		if err := loadPair(tx, &s, &d, src, dst); err != nil {
			return nil, err
		}
		m := newFieldMerger(res, s.FieldProvenance, d.FieldProvenance)
		m.str("display_name", "display_name", d.DisplayName, s.DisplayName)
		mergerPtr(m, "latin", "latin", d.Latin, s.Latin)
		m.str("lang", "lang", d.Lang, s.Lang)
		m.str("note", "note", d.Note, s.Note)
		m.str("logo_hash", "logo_hash", d.LogoHash, s.LogoHash)
		return m.changed, m.apply(tx, "catalog_label", dst)

	case model.EntityTypeCharacter:
		var s, d model.CatalogCharacter
		if err := loadPair(tx, &s, &d, src, dst); err != nil {
			return nil, err
		}
		m := newFieldMerger(res, s.FieldProvenance, d.FieldProvenance)
		m.str("display_name", "display_name", d.DisplayName, s.DisplayName)
		mergerPtr(m, "latin", "latin", d.Latin, s.Latin)
		m.str("lang", "lang", d.Lang, s.Lang)
		mergerPtr(m, "gender", "gender", d.Gender, s.Gender)
		m.str("description", "description", d.Description, s.Description)
		mergerPtr(m, "instance_of", "instance_of", d.InstanceOf, s.InstanceOf)
		mergerPtr(m, "image_hash", "image_hash", d.ImageHash, s.ImageHash)
		return m.changed, m.apply(tx, "catalog_character", dst)

	case model.EntityTypeWork:
		var s, d model.CatalogWork
		if err := loadPair(tx, &s, &d, src, dst); err != nil {
			return nil, err
		}
		m := newFieldMerger(res, s.FieldProvenance, d.FieldProvenance)
		m.str("display_name", "display_name", d.DisplayName, s.DisplayName)
		m.str("olang", "olang", d.OLang, s.OLang)
		changed := m.changed
		if err := m.apply(tx, "catalog_work", dst); err != nil {
			return nil, err
		}
		if d.Site == nil && s.Site != nil {
			productWorkID := s.ProductWorkID
			if model.SiteWorkIDIsCatalogID(*s.Site) {
				productWorkID = &dst
			}
			if err := tx.Exec(`UPDATE catalog_work SET site = NULL, product_work_id = NULL, updated_at = now() WHERE id = ?`, src).Error; err != nil {
				return nil, err
			}
			if err := tx.Exec(`UPDATE catalog_work SET site = ?, product_work_id = ?, status = ?, updated_at = now() WHERE id = ?`,
				*s.Site, productWorkID, s.Status, dst).Error; err != nil {
				return nil, err
			}
			changed["claim"] = map[string]any{"site": *s.Site, "product_work_id": productWorkID}
		}
		return changed, nil
	}
	return nil, fmt.Errorf("catalog survivorship: unsupported entity type %d", entityType)
}

func loadPair[T any](tx *gorm.DB, src, dst *T, srcID, dstID int64) error {
	if err := tx.Unscoped().First(src, srcID).Error; err != nil {
		return fmt.Errorf("load source %d: %w", srcID, err)
	}
	if err := tx.Unscoped().First(dst, dstID).Error; err != nil {
		return fmt.Errorf("load target %d: %w", dstID, err)
	}
	return nil
}
