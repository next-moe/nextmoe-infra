package repository

import (
	"fmt"

	"api/internal/platform/catalog/model"
)

// BundleReleaseSQL is a boolean SQL expression over the catalog_release
// aliased relAlias: true when its exact VNDB release is filed under more than
// one VN. Such a release is still one row on one work, so anything read off
// its store page (synopsis, cast, genres, series, kana) describes whatever the
// package was named after. The 2026-09-16 backlog drain wrote Escalayer
// Reboot's synopsis onto 超昂閃忍ハルカ ハルカVSエスカレイヤー through r33394
// (v3100 + v311); lanes that carry store-page content onto a work skip these.
func BundleReleaseSQL(relAlias string) string {
	return fmt.Sprintf(`EXISTS (
		SELECT 1 FROM catalog_external_ref bundle_vr
		JOIN src_vndb.releases_vn bundle_rv ON bundle_rv.id = bundle_vr.external_id
		JOIN src_vndb.releases_vn bundle_rv2 ON bundle_rv2.id = bundle_rv.id AND bundle_rv2.vid <> bundle_rv.vid
		WHERE bundle_vr.entity_type = %d AND bundle_vr.entity_id = %s.id AND bundle_vr.link_kind = %d
		  AND bundle_vr.source_id = (SELECT id FROM catalog_source WHERE key = 'vndb'))`,
		model.EntityTypeRelease, relAlias, model.LinkKindExact)
}
