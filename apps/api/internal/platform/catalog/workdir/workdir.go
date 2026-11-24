// Package workdir is the work directory of the transition (plan 23, 10 §3):
// the one view the user-state code reads a work through, so a work served by
// canon can be kept in a folder, marked and rated like a legacy one.
package workdir

// View carries only what user lists need of a work. Both migrations create
// it: the catalog one alone before canon exists, then with canon's works.
const View = "catalog_work_directory"

// SQL replaces the view. Canon's side yields to the legacy row of the same id:
// a work that took over a legacy id is in both tables until P4, and two rows
// of one id would double every list that joins here.
func SQL(canon bool) string {
	legacy := `SELECT w.id, w.medium_id, w.display_name, w.content_rating, w.status, w.deleted_at,
		w.display_nsfw, w.cover_art_all_explicit, w.title_nsfw, w.site, w.product_work_id FROM public.catalog_work w`
	if !canon {
		return `CREATE OR REPLACE VIEW public.` + View + ` AS ` + legacy
	}
	return `CREATE OR REPLACE VIEW public.` + View + ` AS ` + legacy + `
		UNION ALL
		SELECT c.id, (CASE c.medium WHEN 'game' THEN 1 WHEN 'manga' THEN 2 WHEN 'novel' THEN 3 WHEN 'anime' THEN 4 WHEN 'audio' THEN 5 END)::smallint,
			(SELECT t.text FROM canon.work_title t WHERE t.work_id = c.id ORDER BY t.kind <> 'official', t.position, t.id LIMIT 1)::text,
			(CASE c.content_rating WHEN 'r15' THEN 1 WHEN 'r18' THEN 2 ELSE 0 END)::smallint,
			(CASE WHEN EXISTS (SELECT 1 FROM canon.takedown d WHERE d.entity = 'work' AND d.entity_id = c.id AND d.scope = 'entity' AND d.restored_at IS NULL) THEN 3 ELSE 0 END)::smallint,
			NULL::timestamptz, c.display_safety = 'explicit', false, NULL::boolean, NULL::text, NULL::bigint
		FROM canon.work c WHERE NOT EXISTS (SELECT 1 FROM public.catalog_work w WHERE w.id = c.id)`
}

// Present asks whether a database has canon's work table.
const Present = `SELECT to_regclass('canon.work') IS NOT NULL AND to_regclass('canon.takedown') IS NOT NULL`
