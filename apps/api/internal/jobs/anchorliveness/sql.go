package anchorliveness

const AliveTempTable = "anchor_liveness_alive"

func ExistsSQL(table, idColumn string, numeric bool) string {
	expr := "m." + idColumn
	if numeric {
		expr += "::text"
	}
	return "EXISTS (SELECT 1 FROM " + table + " m WHERE " + expr + " = catalog_external_ref.external_id)"
}

func LiveExistsSQL(ln Lane) string {
	if ln.Freshness {
		return ExistsSQL(AliveTempTable, "id", false)
	}
	return ExistsSQL(ln.Table, ln.IDColumn, ln.NumericID)
}

func CountRefsSQL() string {
	return `SELECT count(*) FROM catalog_external_ref WHERE source_id = ? AND entity_type = ?`
}

func CountMarkSQL(exists string) string {
	return `SELECT count(*) FROM catalog_external_ref WHERE source_id = ? AND entity_type = ? AND dead_at IS NULL AND NOT (` + exists + `)`
}

func CountClearSQL(exists string) string {
	return `SELECT count(*) FROM catalog_external_ref WHERE source_id = ? AND entity_type = ? AND dead_at IS NOT NULL AND (` + exists + `)`
}

func MarkSQL(exists string) string {
	return `UPDATE catalog_external_ref SET dead_at = now() WHERE source_id = ? AND entity_type = ? AND dead_at IS NULL AND NOT (` + exists + `) RETURNING entity_id, external_id, link_kind`
}

func ClearSQL(exists string) string {
	return `UPDATE catalog_external_ref SET dead_at = NULL WHERE source_id = ? AND entity_type = ? AND dead_at IS NOT NULL AND (` + exists + `) RETURNING entity_id, external_id, link_kind`
}

func FloorSQL(table string) string {
	return `SELECT count(*) FROM ` + table
}

func FreshnessSQL(table string) string {
	return `SELECT count(*) AS total, count(*) FILTER (WHERE synced_at >= (SELECT max(synced_at) FROM ` + table + `) - interval '48 hours') AS fresh FROM ` + table
}

func AliveSQL(table, idColumn string, numeric bool) string {
	expr := idColumn
	if numeric {
		expr += "::text"
	}
	return `SELECT ` + expr + ` AS id FROM ` + table + ` WHERE synced_at >= (SELECT max(synced_at) FROM ` + table + `) - interval '48 hours'`
}

func CreateAliveTempSQL() string {
	return `CREATE TEMP TABLE ` + AliveTempTable + ` (id text PRIMARY KEY) ON COMMIT DROP`
}
