package service

import (
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

// workTitleStmts keeps the survivor's elected title in every language it
// already shows one in. Moving the loser's rows across unchanged let them win
// the (provenance, kind, id) election on id alone: 2,718 merge survivors came
// to show a localized title different from their display_name, 211826's
// 「魔女っ娘ア・ラ・モード2」 over 3993's own 「魔女っ娘ア・ラ・モードII」, and a
// loser's official zh title over 9453's zh name, which is a zh-Hans alias.
//
// In such a language the loser's electable source titles become language-less
// aliases (official turns alias), its machine titles are dropped, and the
// originals are archived. A language the survivor shows nothing in takes the
// loser's rows as they are; the survivor's display_name occupies its olang. An
// absorb treats every language as taken.
func workTitleStmts(p mergePlan) []mergeStmt {
	live := editspec.NotSuppressedWorkTitleSQL("u")
	taken := fmt.Sprintf(`(EXISTS (SELECT 1 FROM catalog_work_title u
	        WHERE u.work_id = %[1]d AND u.kind <> %[2]d AND u.lang <> '' AND lower(u.lang) = lower(t.lang) AND %[3]s)
	     OR EXISTS (SELECT 1 FROM catalog_work w
	        WHERE w.id = %[1]d AND w.display_name <> '' AND w.olang <> '' AND lower(w.olang) = lower(t.lang)))`,
		p.dst, model.WorkTitleKindSearchHint, live)
	if p.absorb {
		taken = "true"
	}
	electable := fmt.Sprintf("t.work_id = ? AND t.kind <> %d AND %s", model.WorkTitleKindSearchHint, taken)
	return []mergeStmt{
		archiveDelete(p, "catalog_work_title",
			electable+fmt.Sprintf(" AND t.provenance = %d", model.WorkTitleProvenanceMachine), p.src),
		recordInserted(p, "catalog_work_title", `INSERT INTO catalog_work_title (work_id, lang, title, latin, kind, provenance)
		  SELECT DISTINCT ON (t.title, a.kind) ?, '', t.title, t.latin, a.kind, ?
		    FROM catalog_work_title t
		    CROSS JOIN LATERAL (SELECT CASE WHEN t.kind = ? THEN ? ELSE t.kind END AS kind) a
		   WHERE `+electable+` AND `+editspec.NotSuppressedWorkTitleSQL("t")+`
		     AND NOT EXISTS (SELECT 1 FROM catalog_work_title x WHERE x.work_id = ? AND x.title = t.title)
		     AND NOT EXISTS (SELECT 1 FROM catalog_work w WHERE w.id = ? AND w.display_name = t.title)
		   ORDER BY t.title, a.kind, t.id
		  ON CONFLICT DO NOTHING`,
			[]any{p.dst, model.WorkTitleProvenanceSource, model.WorkTitleKindOfficial, model.WorkTitleKindAlias,
				p.src, p.dst, p.dst}),
		archiveDelete(p, "catalog_work_title", electable, p.src),
		{`UPDATE catalog_work_title t SET work_id = ? WHERE t.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_work_title u
		                     WHERE u.work_id = ? AND u.lang = t.lang AND u.title = t.title AND u.kind = t.kind)`,
			[]any{p.dst, p.src, p.dst}, false},
		archiveDelete(p, "catalog_work_title", "t.work_id = ?", p.src),
	}
}

type mergeAliasTable struct {
	table, owner, ownerTable, ownerName string
	live                                func(alias string) string
}

var (
	labelAliasTable = mergeAliasTable{table: "catalog_label_alias", owner: "label_id", ownerTable: "catalog_label", ownerName: "display_name",
		live: editspec.NotSuppressedLabelAliasSQL}
	characterAliasTable = mergeAliasTable{table: "catalog_character_alias", owner: "character_id", ownerTable: "catalog_character", ownerName: "display_name",
		live: editspec.NotSuppressedCharacterAliasSQL}
	nameAliasTable = mergeAliasTable{table: "catalog_name_alias", owner: "credit_name_id", ownerTable: "catalog_credit_name", ownerName: "name",
		live: editspec.NotSuppressedNameAliasSQL}
)

// aliasDemoteStmts is workTitleStmts for the entity alias tables. It must run
// before the rehang, and leaves exact (name, lang) twins to it: the character
// rehang lets a curated twin replace the survivor's upstream row.
func aliasDemoteStmts(p mergePlan, a mergeAliasTable) []mergeStmt {
	liveU, liveT := "true", "true"
	if a.live != nil {
		liveU, liveT = a.live("u"), a.live("t")
	}
	demoted := fmt.Sprintf(`t.%[1]s = ? AND t.kind <> %[2]d
		AND EXISTS (SELECT 1 FROM %[3]s u WHERE u.%[1]s = %[4]d AND u.kind <> %[2]d
		             AND u.lang <> '' AND lower(u.lang) = lower(t.lang) AND %[5]s)
		AND NOT EXISTS (SELECT 1 FROM %[3]s x WHERE x.%[1]s = %[4]d AND x.name = t.name AND x.lang = t.lang)`,
		a.owner, model.AliasKindSearchHint, a.table, p.dst, liveU)
	return []mergeStmt{
		archiveDelete(p, a.table, demoted+fmt.Sprintf(" AND t.provenance = %d", model.AliasProvenanceMachine), p.src),
		recordInserted(p, a.table, fmt.Sprintf(`INSERT INTO %[1]s (%[2]s, name, latin, lang, kind, is_primary_for_locale, source_id, provenance, mt_model)
		  SELECT DISTINCT ON (t.name) ?, t.name, t.latin, '', t.kind, false, t.source_id, t.provenance, t.mt_model
		    FROM %[1]s t
		   WHERE %[3]s AND %[4]s
		     AND NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.%[2]s = ? AND x.name = t.name)
		     AND NOT EXISTS (SELECT 1 FROM %[5]s o WHERE o.id = ? AND o.%[6]s = t.name)
		   ORDER BY t.name, t.kind, t.id
		  ON CONFLICT DO NOTHING`, a.table, a.owner, demoted, liveT, a.ownerTable, a.ownerName),
			[]any{p.dst, p.src, p.dst, p.dst}),
		archiveDelete(p, a.table, demoted, p.src),
	}
}

// introStmts keeps the survivor's elected intro in each language. The
// election ranks source text over machine and then by source id, so a loser
// intro from a lower source id took the language: after 211826 merged into
// 3993 its DLsite ja intro moved in, and the zh election and intromt both
// picked it over 3993's own text. Per language: a survivor source intro keeps
// out every loser intro; a survivor with only machine intros takes the loser's
// source intros, replacing a machine row on the same source; a language the
// survivor lacks takes all. Everything kept out is archived.
func introStmts(p mergePlan, table, owner string) []mergeStmt {
	src, mach := model.IntroProvenanceSource, model.IntroProvenanceMachine
	return []mergeStmt{
		archiveDelete(p, table, fmt.Sprintf(`t.%[1]s = ? AND EXISTS (SELECT 1 FROM %[2]s u
			WHERE u.%[1]s = ? AND u.lang = t.lang AND u.provenance = %[3]d)`, owner, table, src), p.src, p.dst),
		archiveDelete(p, table, fmt.Sprintf(`t.%[1]s = ? AND t.provenance = %[3]d AND EXISTS (SELECT 1 FROM %[2]s u
			WHERE u.%[1]s = ? AND u.lang = t.lang)`, owner, table, mach), p.src, p.dst),
		archiveDelete(p, table, fmt.Sprintf(`t.%[1]s = ? AND t.provenance = %[3]d AND EXISTS (SELECT 1 FROM %[2]s s
			WHERE s.%[1]s = ? AND s.lang = t.lang AND s.source_id = t.source_id AND s.provenance = %[4]d)`,
			owner, table, mach, src), p.dst, p.src),
		{fmt.Sprintf(`UPDATE %[2]s t SET %[1]s = ? WHERE t.%[1]s = ?
		    AND NOT EXISTS (SELECT 1 FROM %[2]s x WHERE x.%[1]s = ? AND x.lang = t.lang AND x.source_id = t.source_id)`,
			owner, table), []any{p.dst, p.src, p.dst}, false},
		archiveDelete(p, table, fmt.Sprintf("t.%s = ?", owner), p.src),
	}
}
