package service

func liveWorkCond(column string) string {
	return "EXISTS (SELECT 1 FROM catalog_work live_w WHERE live_w.id = " + column + " AND live_w.deleted_at IS NULL)"
}
