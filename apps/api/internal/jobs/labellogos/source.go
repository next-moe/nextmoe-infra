package labellogos

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Source struct {
	Key         string
	FileStem    string
	UploaderSub string
	LinkKind    int16
	MatchedBy   []string
}

var (
	SourceBangumi = Source{
		Key: "bangumi", FileStem: "logo", UploaderSub: "system:label-logo-backfill:bangumi",
		LinkKind: model.LinkKindExact,
	}
	SourceCien = Source{
		Key: "cien", FileStem: "avatar", UploaderSub: "system:label-logo-backfill:cien",
		LinkKind: model.LinkKindRelated, MatchedBy: cienAnchorRules,
	}
)

var cienAnchorRules = []string{"rule:eg-cien", "rule:cien-self"}

func ParseSource(name string) (Source, error) {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "bangumi":
		return SourceBangumi, nil
	case "cien":
		return SourceCien, nil
	case "":
		return Source{}, fmt.Errorf("--source is REQUIRED: bangumi (brand logos) or cien (creator avatars)")
	default:
		return Source{}, fmt.Errorf("unknown --source %q (want bangumi or cien)", name)
	}
}

type registry struct {
	bangumi int16
	cien    int16
}

func (r registry) sourceID(s Source) int16 {
	if s.Key == SourceCien.Key {
		return r.cien
	}
	return r.bangumi
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&r.bangumi).Error; err != nil {
		return r, fmt.Errorf("resolve bangumi source: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'cien'`).Scan(&r.cien).Error; err != nil {
		return r, fmt.Errorf("resolve cien source: %w", err)
	}
	if r.bangumi == 0 || r.cien == 0 {
		return r, fmt.Errorf("registry not seeded (bangumi source=%d, cien source=%d)", r.bangumi, r.cien)
	}
	return r, nil
}

type candidate struct {
	LabelID         int64          `gorm:"column:label_id"`
	ExternalID      string         `gorm:"column:external_id"`
	FieldProvenance datatypes.JSON `gorm:"column:field_provenance"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, sourceID int16, src Source) ([]candidate, error) {
	clause, args := anchorClause("r", sourceID, src)
	var out []candidate
	err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (l.id)
		       l.id AS label_id, r.external_id AS external_id, l.field_provenance AS field_provenance
		FROM catalog_label l
		JOIN catalog_external_ref r ON r.entity_id = l.id AND `+clause+`
		WHERE l.deleted_at IS NULL AND l.logo_hash = '' AND `+editspec.NotHumanFieldSQL("l.field_provenance", provField)+`
		ORDER BY l.id, r.external_id`, args...).Scan(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func anchorClause(alias string, sourceID int16, src Source) (string, []any) {
	q := fmt.Sprintf(`%[1]s.entity_type = ? AND %[1]s.source_id = ? AND %[1]s.link_kind = ?`, alias)
	args := []any{model.EntityTypeLabel, sourceID, src.LinkKind}
	if len(src.MatchedBy) > 0 {
		q += fmt.Sprintf(` AND %s.matched_by IN ?`, alias)
		args = append(args, src.MatchedBy)
	}
	return q, args
}

func writeIDs(path string, cands []candidate, m *mirror) (int, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		if seen[c.ExternalID] || m.has(c.ExternalID) {
			continue
		}
		seen[c.ExternalID] = true
		ids = append(ids, c.ExternalID)
	}
	sort.Strings(ids)
	body := ""
	if len(ids) > 0 {
		body = strings.Join(ids, "\n") + "\n"
	}
	return len(ids), os.WriteFile(path, []byte(body), 0o644)
}
