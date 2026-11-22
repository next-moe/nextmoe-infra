// Package xref maintains the third-party id read model (D22). Import is the
// only place canon code reads a legacy table, and only to copy ids out of it.
package xref

import (
	"context"
	"database/sql"
	"fmt"
)

// Sources maps a legacy catalog_source key to the public source name and the
// page an id opens. Official and store links are not here: they are canon
// links with evidence (D22 ①).
var Sources = map[string]struct {
	Name string
	URL  string
}{
	"vndb":          {"vndb", "https://vndb.org/%s"},
	"bangumi":       {"bangumi", "https://bgm.tv/subject/%s"},
	"erogamescape":  {"erogamescape", "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game=%s"},
	"howlongtobeat": {"howlongtobeat", "https://howlongtobeat.com/game/%s"},
}

func URL(source, external string) string {
	if s, ok := Sources[source]; ok {
		return fmt.Sprintf(s.URL, external)
	}
	return ""
}

// ImportLegacy rebuilds the legacy-origin rows from public.catalog_external_ref:
// exact, live work anchors of the four sources. Rows from other origins stay.
func ImportLegacy(ctx context.Context, db *sql.DB) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM canon.xref_link WHERE origin = 'legacy'`); err != nil {
		return 0, err
	}
	keys := make([]string, 0, len(Sources))
	for k := range Sources {
		keys = append(keys, k)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO canon.xref_link (source, external_id, entity, entity_id, origin, imported_at)
		SELECT DISTINCT ON (s.key, r.external_id) s.key, r.external_id, 'work', r.entity_id, 'legacy', now()
		FROM public.catalog_external_ref r
		JOIN public.catalog_source s ON s.id = r.source_id
		JOIN public.catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = 5 AND r.link_kind = 0 AND r.dead_at IS NULL AND s.key = ANY($1)
		ORDER BY s.key, r.external_id, r.created_at`, keys)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
