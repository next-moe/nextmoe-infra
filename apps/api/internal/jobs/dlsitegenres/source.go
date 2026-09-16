package dlsitegenres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type registry struct {
	galgameMedium int16
	dlsiteSource  int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&r.dlsiteSource).Error; err != nil {
		return r, fmt.Errorf("resolve dlsite source: %w", err)
	}
	if r.galgameMedium == 0 || r.dlsiteSource == 0 {
		return r, fmt.Errorf("registry not seeded (galgame medium=%d, dlsite source=%d)",
			r.galgameMedium, r.dlsiteSource)
	}
	return r, nil
}

type candidate struct {
	WorkID int64  `gorm:"column:work_id"`
	Workno string `gorm:"column:workno"`
	Bundle bool   `gorm:"column:bundle"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, reg registry, limit, offset int) ([]candidate, error) {
	var out []candidate
	if err := db.WithContext(ctx).
		Raw(`SELECT DISTINCT ON (w.id) w.id AS work_id, r.external_id AS workno,
				`+repository.BundleReleaseSQL("rel")+` AS bundle
			FROM catalog_work w
			JOIN catalog_release rel ON rel.work_id = w.id AND rel.deleted_at IS NULL
			JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = rel.id
				AND r.source_id = ? AND r.link_kind = ?
			WHERE w.medium_id = ? AND w.deleted_at IS NULL
			ORDER BY w.id, bundle, r.external_id`,
			model.EntityTypeRelease, reg.dlsiteSource, model.LinkKindExact, reg.galgameMedium).
		Scan(&out).Error; err != nil {
		return nil, err
	}
	return window(out, limit, offset), nil
}

func loadTaxonomy(ctx context.Context, dlsiteDB *gorm.DB) (map[int]string, error) {
	var rows []struct {
		GenreID int    `gorm:"column:genre_id"`
		Name    string `gorm:"column:name"`
	}
	if err := dlsiteDB.WithContext(ctx).Table("genre_taxonomy").
		Select("genre_id, name").Where("locale = ?", taxonomyLocale).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]string, len(rows))
	for _, r := range rows {
		if name := strings.TrimSpace(r.Name); name != "" {
			out[r.GenreID] = name
		}
	}
	return out, nil
}

func loadMirrorGenres(ctx context.Context, dlsiteDB *gorm.DB, worknos []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(worknos))
	type row struct {
		Workno string `gorm:"column:workno"`
		Genres []byte `gorm:"column:genres"`
	}
	for start := 0; start < len(worknos); start += 1000 {
		end := min(start+1000, len(worknos))
		var batch []row
		if err := dlsiteDB.WithContext(ctx).Table("works").
			Select(`workno, product_json->'genres' AS genres`).
			Where("workno IN ?", worknos[start:end]).Scan(&batch).Error; err != nil {
			return nil, err
		}
		for _, r := range batch {
			out[r.Workno] = r.Genres
		}
	}
	return out, nil
}

type genreEl struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type resolvedGenre struct {
	ID           int
	Name         string
	FromTaxonomy bool
}

func resolveGenres(raw []byte, taxonomy map[int]string, st *Stats) []resolvedGenre {
	if len(raw) == 0 || string(raw) == "null" {
		st.NoGenres++
		return nil
	}
	var els []genreEl
	if err := json.Unmarshal(raw, &els); err != nil {
		st.NotArray++
		return nil
	}
	if len(els) == 0 {
		st.NoGenres++
		return nil
	}
	out := make([]resolvedGenre, 0, len(els))
	seen := map[string]bool{}
	for _, el := range els {
		name, fromTaxonomy := taxonomy[el.ID]
		if !fromTaxonomy {
			name = strings.TrimSpace(el.Name)
			if name == "" {
				st.NameBlank++
				continue
			}
		}
		if fromTaxonomy {
			st.ZhHit++
		} else {
			st.JaFallback++
		}
		if seen[name] {
			st.DupCollapsed++
			continue
		}
		seen[name] = true
		out = append(out, resolvedGenre{ID: el.ID, Name: name, FromTaxonomy: fromTaxonomy})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func window[T any](in []T, limit, offset int) []T {
	if offset > 0 {
		if offset >= len(in) {
			return nil
		}
		in = in[offset:]
	}
	if limit > 0 && limit < len(in) {
		in = in[:limit]
	}
	return in
}
