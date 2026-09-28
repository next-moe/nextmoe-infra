package model

import "fmt"

var WorkExactRefsSQL = fmt.Sprintf(`(SELECT r.entity_id AS work_id, r.source_id, r.external_id
	FROM catalog_external_ref r
	WHERE r.entity_type = %[1]d AND r.link_kind = %[3]d AND r.dead_at IS NULL
	UNION
	SELECT rel.work_id, r.source_id, r.external_id
	FROM catalog_external_ref r
	JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
	WHERE r.entity_type = %[2]d AND r.link_kind = %[3]d AND r.dead_at IS NULL)`,
	EntityTypeWork, EntityTypeRelease, LinkKindExact)
