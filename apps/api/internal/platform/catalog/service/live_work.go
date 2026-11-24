package service

import "api/internal/platform/catalog/workdir"

// workDirectory names a work for the user lists, legacy or canon.
const workDirectory = workdir.View

func liveWorkCond(column string) string {
	return "NOT EXISTS (SELECT 1 FROM catalog_work live_w WHERE live_w.id = " + column + " AND live_w.deleted_at IS NOT NULL)"
}
