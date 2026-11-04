package orglabels

import (
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type repairOrg struct {
	name    string
	latin   *string
	lang    string
	kind    int16
	canMint bool
	norms   []string
}

func loadRepairOrg(p repairPools, source int16, ext string) (*repairOrg, error) {
	var names []string
	org := &repairOrg{kind: model.LabelKindGameBrand}
	switch source {
	case sourceVNDB:
		var row struct{ Name, Latin, Lang, Type, Alias string }
		res := p.catalog.Raw(`SELECT name, coalesce(latin,'') AS latin, coalesce(lang,'') AS lang, type,
			coalesce(alias,'') AS alias FROM src_vndb.producers WHERE id = ?`, ext).Scan(&row)
		if res.Error != nil || res.RowsAffected == 0 {
			return nil, res.Error
		}
		org.name, org.lang = row.Name, row.Lang
		if row.Latin != "" {
			l := row.Latin
			org.latin = &l
		}
		org.canMint = row.Type == "co" || row.Type == "ng"
		if row.Type == "ng" {
			org.kind = model.LabelKindDoujinCircle
		}
		names = append([]string{row.Name, row.Latin}, strings.Split(row.Alias, "\n")...)
	case sourceEG:
		var row struct{ Brandname, Furigana, Kind string }
		res := p.eg.Raw(`SELECT coalesce(raw->>'brandname','') AS brandname, coalesce(raw->>'brandfurigana','') AS furigana,
			coalesce(raw->>'kind','') AS kind FROM brands WHERE id::text = ?`, ext).Scan(&row)
		if res.Error != nil || res.RowsAffected == 0 || row.Brandname == "" {
			return nil, res.Error
		}
		org.name, org.lang, org.canMint = row.Brandname, "ja", true
		if row.Kind == "CIRCLE" {
			org.kind = model.LabelKindDoujinCircle
		}
		names = []string{row.Brandname, row.Furigana}
	case sourceDlsite:
		var rows []string
		if err := p.dlsite.Raw(`SELECT DISTINCT maker_name FROM works WHERE maker_id = ? AND coalesce(maker_name,'') <> ''`,
			ext).Scan(&rows).Error; err != nil || len(rows) == 0 {
			return nil, err
		}
		org.name, org.lang, org.canMint, org.kind = rows[0], "ja", true, model.LabelKindDoujinCircle
		if strings.HasPrefix(ext, "BG") {
			org.kind = model.LabelKindPublisher
		}
		names = rows
	case sourceBangumi:
		var row struct{ Name string }
		res := p.catalog.Raw(`SELECT name FROM src_bangumi.person WHERE id::text = ?`, ext).Scan(&row)
		if res.Error != nil || res.RowsAffected == 0 {
			return nil, res.Error
		}
		org.name = row.Name
		names = []string{row.Name}
	}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			var norm string
			if err := p.catalog.Raw(`SELECT lower(normalize(?, NFKC))`, n).Scan(&norm).Error; err != nil {
				return nil, err
			}
			org.norms = append(org.norms, norm)
		}
	}
	org.norms = dedupStrings(org.norms)
	return org, nil
}

var linkRulesBySource = map[int16][]string{
	sourceVNDB:    {"rule:vndb-extlink"},
	sourceEG:      {ruleEGOfficial, ruleEGTwitter, ruleEGCien},
	sourceBangumi: {ruleBGMOfficial, ruleBGMTwitter},
}

func moveDerived(tx *gorm.DB, p repairPools, d RepairDecision, target int64, res *RepairResult) ([]int64, error) {
	var touched []int64
	works, releases, err := sourcedRows(tx, p, d)
	if err != nil {
		return nil, err
	}
	if len(releases) > 0 {
		n, w, err := moveRows(tx, "catalog_release_label", "release_id", d, target, releases)
		if err != nil {
			return nil, err
		}
		res.ReleaseRows, touched = n, append(touched, w...)
	}
	if len(works) > 0 {
		n, w, err := moveRows(tx, "catalog_work_label", "work_id", d, target, works)
		if err != nil {
			return nil, err
		}
		res.WorkRows, touched = n, append(touched, w...)
	}
	if d.Source == sourceBangumi {
		var cnIDs []int64
		if err := tx.Raw(`SELECT entity_id FROM catalog_external_ref WHERE entity_type = 1 AND source_id = 3
			AND external_id = ?`, d.ExtID).Scan(&cnIDs).Error; err != nil {
			return nil, err
		}
		if len(cnIDs) > 0 {
			var newLabel any
			if target != 0 {
				newLabel = target
			}
			r := tx.Exec(`UPDATE catalog_credit SET label_id = ?, updated_at = now()
				WHERE label_id = ? AND source_id = 3 AND credit_name_id IN ?`, newLabel, d.LabelID, cnIDs)
			if r.Error != nil {
				return nil, r.Error
			}
			res.Credits = r.RowsAffected
		}
	}
	if res.Intros, err = moveLabelOwned(tx, "catalog_label_intro", "lang, source_id", d, target); err != nil {
		return nil, err
	}
	if res.Aliases, err = moveLabelOwned(tx, "catalog_label_alias", "name, lang", d, target); err != nil {
		return nil, err
	}
	if rules := linkRulesBySource[d.Source]; len(rules) > 0 {
		var moved []struct{ N int64 }
		if err := tx.Raw(`WITH gone AS (
				DELETE FROM catalog_external_ref WHERE entity_type = 3 AND entity_id = ? AND matched_by IN ?
				RETURNING source_id, external_id, link_kind, matched_by),
			ins AS (
				INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
				SELECT 3, ?, source_id, external_id, link_kind, matched_by, now() FROM gone WHERE ?::bigint <> 0
				ON CONFLICT DO NOTHING RETURNING 1)
			SELECT (SELECT count(*) FROM gone) AS n`, d.LabelID, rules, target, target).Scan(&moved).Error; err != nil {
			return nil, err
		}
		if len(moved) > 0 {
			res.Links = moved[0].N
		}
	}
	return touched, nil
}

// sourcedRows names the work and release rows that came to the label through
// this one external entry, so a label holding two anchors of one source keeps
// the rows its other anchor still justifies.
func sourcedRows(tx *gorm.DB, p repairPools, d RepairDecision) (works, releases []int64, err error) {
	switch d.Source {
	case sourceVNDB:
		const producedBy = `JOIN catalog_external_ref rr ON rr.entity_type = 6 AND rr.source_id = 2 AND rr.link_kind = 0
			JOIN catalog_release rel ON rel.id = rr.entity_id
			JOIN src_vndb.releases_producers rp ON rp.id = rr.external_id`
		const notOther = `NOT EXISTS (SELECT 1 FROM src_vndb.releases_producers op
			JOIN catalog_external_ref ol ON ol.entity_type = 3 AND ol.source_id = 2 AND ol.link_kind = 0
			     AND ol.entity_id = ? AND ol.external_id = op.pid
			WHERE op.id = rr.external_id)`
		if err = tx.Raw(`SELECT DISTINCT rel.id FROM catalog_release_label rl `+producedBy+`
			WHERE rl.release_id = rel.id AND rl.label_id = ? AND rl.source_id = 2 AND rp.pid = ? AND `+notOther,
			d.LabelID, d.ExtID, d.LabelID).Scan(&releases).Error; err != nil {
			return nil, nil, err
		}
		err = tx.Raw(`SELECT DISTINCT rel.work_id FROM catalog_work_label wl `+producedBy+`
			WHERE wl.work_id = rel.work_id AND wl.label_id = ? AND wl.source_id = 2 AND rp.pid = ? AND `+notOther,
			d.LabelID, d.ExtID, d.LabelID).Scan(&works).Error
		return works, releases, err
	case sourceEG, sourceDlsite:
		mine, err := productsOf(p, d.Source, []string{d.ExtID})
		if err != nil {
			return nil, nil, err
		}
		if works, err = worksByRef(tx, d.Source, mine, d.LabelID); err != nil || len(works) == 0 {
			return works, nil, err
		}
		var others []string
		if err := tx.Raw(`SELECT external_id FROM catalog_external_ref WHERE entity_type = 3 AND entity_id = ?
			AND source_id = ? AND link_kind = 0`, d.LabelID, d.Source).Scan(&others).Error; err != nil {
			return nil, nil, err
		}
		theirs, err := productsOf(p, d.Source, others)
		if err != nil {
			return nil, nil, err
		}
		kept, err := worksByRef(tx, d.Source, theirs, d.LabelID)
		if err != nil {
			return nil, nil, err
		}
		return subtract(works, kept), nil, nil
	}
	return nil, nil, nil
}

func productsOf(p repairPools, source int16, orgs []string) ([]string, error) {
	if len(orgs) == 0 {
		return nil, nil
	}
	var out []string
	var err error
	if source == sourceEG {
		err = p.eg.Raw(`SELECT id::text FROM games WHERE brand_id::text IN ?`, orgs).Scan(&out).Error
	} else {
		err = p.dlsite.Raw(`SELECT workno FROM works WHERE maker_id IN ?`, orgs).Scan(&out).Error
	}
	return out, err
}

func subtract(a, b []int64) []int64 {
	drop := make(map[int64]bool, len(b))
	for _, x := range b {
		drop[x] = true
	}
	out := a[:0]
	for _, x := range a {
		if !drop[x] {
			out = append(out, x)
		}
	}
	return out
}

func worksByRef(tx *gorm.DB, source int16, exts []string, labelID int64) ([]int64, error) {
	if len(exts) == 0 {
		return nil, nil
	}
	var works []int64
	err := tx.Raw(`SELECT DISTINCT wl.work_id FROM catalog_work_label wl
		WHERE wl.label_id = ? AND wl.source_id = ? AND wl.work_id IN (
			SELECT r.entity_id FROM catalog_external_ref r WHERE r.entity_type = 5 AND r.source_id = ? AND r.external_id IN ?
			UNION
			SELECT rel.work_id FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
			WHERE r.entity_type = 6 AND r.source_id = ? AND r.external_id IN ?)`,
		labelID, source, source, exts, source, exts).Scan(&works).Error
	return works, err
}

func moveRows(tx *gorm.DB, table, key string, d RepairDecision, target int64, ids []int64) (int64, []int64, error) {
	if target != 0 {
		if err := tx.Exec(`INSERT INTO `+table+` (`+key+`, label_id, kind, source_id, created_at)
			SELECT `+key+`, ?, kind, source_id, now() FROM `+table+`
			WHERE label_id = ? AND source_id = ? AND `+key+` IN ? ON CONFLICT DO NOTHING`,
			target, d.LabelID, d.Source, ids).Error; err != nil {
			return 0, nil, err
		}
	}
	r := tx.Exec(`DELETE FROM `+table+` WHERE label_id = ? AND source_id = ? AND `+key+` IN ?`,
		d.LabelID, d.Source, ids)
	if r.Error != nil {
		return 0, nil, r.Error
	}
	works := ids
	if key == "release_id" {
		works = nil
		if err := tx.Raw(`SELECT DISTINCT work_id FROM catalog_release WHERE id IN ?`, ids).Scan(&works).Error; err != nil {
			return 0, nil, err
		}
	}
	return r.RowsAffected, works, nil
}

func moveLabelOwned(tx *gorm.DB, table, uniq string, d RepairDecision, target int64) (int64, error) {
	var n int64
	if err := tx.Raw(`SELECT count(*) FROM `+table+` WHERE label_id = ? AND source_id = ?`, d.LabelID, d.Source).
		Scan(&n).Error; err != nil || n == 0 {
		return 0, err
	}
	if target != 0 {
		if err := tx.Exec(`UPDATE `+table+` t SET label_id = ? WHERE t.label_id = ? AND t.source_id = ?
			AND NOT EXISTS (SELECT 1 FROM `+table+` o WHERE o.label_id = ? AND (`+prefixCols("o", uniq)+`) = (`+prefixCols("t", uniq)+`))`,
			target, d.LabelID, d.Source, target).Error; err != nil {
			return 0, err
		}
	}
	return n, tx.Exec(`DELETE FROM `+table+` WHERE label_id = ? AND source_id = ?`, d.LabelID, d.Source).Error
}

func prefixCols(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "ROW(" + strings.Join(parts, ", ") + ")"
}
