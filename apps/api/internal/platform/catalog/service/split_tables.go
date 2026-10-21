package service

import "gorm.io/gorm"

// releaseScopedWorkRows are the work-owned rows that also name one of the
// work's releases. A split moves such a row with its release: a credit limited
// to the edition that leaves belongs to the work that edition now lives under.
// Rows keyed by the release alone (catalog_release_label, release-level
// catalog_external_ref, edit history addressed to catalog.release) follow the
// release id and need nothing. TestReleaseColumnsAreAllAccountedFor fails when
// a new column names a release and is in neither list.
var releaseScopedWorkRows = []struct{ table string }{
	{"catalog_credit"},
}

var releaseKeyedColumns = map[string]string{
	"catalog_credit.release_id":         "work-owned; moves with its release (releaseScopedWorkRows)",
	"catalog_release_label.release_id":  "keyed by the release alone; follows it",
	"catalog_split_request.release_ids": "the split's own record of the releases it moves",
}

func (s *MergeService) withDB(db *gorm.DB) *MergeService {
	c := *s
	c.db = db
	return &c
}
