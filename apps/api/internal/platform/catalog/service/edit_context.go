package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/catalog/editspec"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

const EditContextRowCap = 1000

type EditContextService struct {
	db *gorm.DB
}

func NewEditContextService(db *gorm.DB) *EditContextService {
	return &EditContextService{db: db}
}

type ContextRow struct {
	SourceKey         string
	HumanLane         bool
	Identity          string
	Suppressed        bool
	Present           bool
	Value             json.RawMessage
	MachineTranslated bool
	FieldProvenance   json.RawMessage
}

type contextRowSource struct {
	from      string
	owner     string
	sourceID  string
	sourceKey string
	human     string
	identity  string
	value     string
	machine   string
	prov      string
	order     string
}

var humanSourceList = func() string {
	ids := editspec.HumanSourceIDs()
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(int(id))
	}
	return strings.Join(parts, ", ")
}()

func humanLaneOf(sourceCol string) string {
	return fmt.Sprintf("%s IN (%s)", sourceCol, humanSourceList)
}

func curatedMatch(col string) string {
	return fmt.Sprintf("%s = '%s'", col, provenance.SourceCurated)
}

func suppressibleIntroRows(table, ownerCol string) contextRowSource {
	src := introRows(table, ownerCol, true)
	src.identity = editspec.IntroIdentitySQL("i")
	return src
}

func introRows(table, ownerCol string, machine bool) contextRowSource {
	src := contextRowSource{
		from: table + " i", owner: "i." + ownerCol, sourceID: "i.source_id",
		human: humanLaneOf("i.source_id"),
		value: `json_build_object('lang', i.lang, 'intro', i.intro)`,
		order: "i.lang, i.source_id, i.id",
	}
	if machine {
		src.human = fmt.Sprintf("%s AND i.provenance = %d", src.human, catmodel.IntroProvenanceSource)
		src.machine = fmt.Sprintf("i.provenance = %d", catmodel.IntroProvenanceMachine)
	}
	return src
}

func linkRows(entityType int16) contextRowSource {
	return contextRowSource{
		from:     "catalog_external_ref r",
		owner:    fmt.Sprintf("r.entity_type = %d AND r.entity_id", entityType),
		sourceID: "r.source_id",
		human:    curatedMatch("r.matched_by"),
		identity: fmt.Sprintf("CASE WHEN r.link_kind <> %d THEN %s END", catmodel.LinkKindExact, editspec.ExternalRefIdentitySQL("r")),
		value:    `json_build_object('external_id', r.external_id, 'link_kind', r.link_kind, 'matched_by', r.matched_by, 'dead', r.dead_at IS NOT NULL)`,
		order:    "r.source_id, r.external_id",
	}
}

// The curated lane and the *_upstream regrade fields read the same rows; the
// per-row field_provenance is where a regrade's stamp lands.
func coverRows() contextRowSource {
	return contextRowSource{
		from:  "catalog_work_cover c " + editspec.WorkMediaOrderJoinSQL("c", catmodel.WorkMediaOrderCover),
		owner: "c.work_id", sourceID: "c.source_id",
		human:    humanLaneOf("c.source_id"),
		identity: editspec.CoverIdentitySQL("c"),
		value:    `json_build_object('image_hash', c.image_hash, 'kind', c.kind, 'portrait_pinned', c.portrait_pinned, 'sexual', c.sexual, 'violence', c.violence)`,
		prov:     "c.field_provenance",
		order:    "mo.position NULLS LAST, c.sort_order, c.image_hash",
	}
}

func screenshotRows() contextRowSource {
	return contextRowSource{
		from:  "catalog_work_screenshot ss " + editspec.WorkMediaOrderJoinSQL("ss", catmodel.WorkMediaOrderScreenshot),
		owner: "ss.work_id", sourceID: "ss.source_id",
		human:    humanLaneOf("ss.source_id"),
		identity: editspec.ScreenshotIdentitySQL("ss"),
		value:    `json_build_object('image_hash', ss.image_hash, 'caption', ss.caption, 'sexual', ss.sexual, 'violence', ss.violence)`,
		prov:     "ss.field_provenance",
		order:    "mo.position NULLS LAST, ss.source_id, ss.sort_order, ss.image_hash",
	}
}

func creditRows() contextRowSource {
	return contextRowSource{
		from: "catalog_credit cr", owner: "cr.work_id", sourceID: "cr.source_id",
		human:    humanLaneOf("cr.source_id"),
		identity: editspec.CreditIdentitySQL("cr"),
		value:    `json_build_object('role_id', cr.role_id, 'credit_name_id', cr.credit_name_id, 'character_id', cr.character_id, 'note', cr.note, 'spoiler', cr.spoiler, 'release_id', cr.release_id, 'label_id', cr.label_id)`,
		prov:     "cr.field_provenance",
		order:    "cr.source_id, cr.role_id, cr.id",
	}
}

// contextRowSources names the table behind every stored row of a multi-source
// list field. It is a hand-maintained list over a growing registry:
// TestEditContextCoversEveryListField fails when a list field is registered with
// neither an entry here nor a reason in snapshotOnlyListFields, and when a field
// that has a .suppressed companion maps to rows without an identity key — the
// editor could then see an upstream row and have no key to hide it with.
var contextRowSources = map[string]contextRowSource{
	editspec.FieldWorkTitles: {
		from: "catalog_work_title t", owner: "t.work_id",
		identity: editspec.WorkTitleIdentitySQL("t"),
		value:    `json_build_object('lang', t.lang, 'title', t.title, 'kind', t.kind, 'latin', t.latin)`,
		machine:  fmt.Sprintf("t.provenance = %d", catmodel.IntroProvenanceMachine),
		order:    "t.kind, t.lang, t.id",
	},
	editspec.FieldWorkIntros: suppressibleIntroRows("catalog_work_intro", "work_id"),
	editspec.FieldWorkTagIDs: {
		from: `catalog_work_tag wt
			LEFT JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name`,
		owner: "wt.work_id", sourceID: "wt.source_id", human: humanLaneOf("wt.source_id"),
		identity: fmt.Sprintf("CASE WHEN m.tag_id IS NULL THEN NULL ELSE %s END", editspec.WorkTagIdentitySQL("m.tag_id")),
		value:    `json_build_object('tag_id', m.tag_id, 'name', wt.name, 'spoiler', wt.spoiler, 'count', wt.count)`,
		order:    "wt.source_id, m.tag_id NULLS LAST, wt.name",
	},
	editspec.FieldWorkLabels: {
		from: "catalog_work_label wl", owner: "wl.work_id", sourceID: "wl.source_id",
		human:    humanLaneOf("wl.source_id"),
		identity: editspec.WorkLabelIdentitySQL("wl"),
		value:    `json_build_object('label_id', wl.label_id, 'kind', wl.kind)`,
		order:    "wl.source_id NULLS FIRST, wl.label_id, wl.kind",
	},
	editspec.FieldWorkEngineIDs: {
		from: "catalog_work_engine we", owner: "we.work_id", sourceID: "we.source_id",
		human:    humanLaneOf("we.source_id"),
		identity: editspec.WorkEngineIdentitySQL("we.engine_id"),
		value:    `to_json(we.engine_id)`,
		order:    "we.source_id, we.engine_id",
	},
	editspec.FieldWorkSeriesIDs: {
		from:  "catalog_series_member sm JOIN catalog_series s ON s.id = sm.series_id",
		owner: "sm.work_id", sourceID: "s.source_id", human: humanLaneOf("s.source_id"),
		identity: editspec.SeriesIdentitySQL("s"),
		value:    `json_build_object('series_id', sm.series_id, 'position', sm.position, 'kind', sm.kind)`,
		order:    "s.source_id, sm.series_id",
	},
	editspec.FieldWorkLinks:               linkRows(catmodel.EntityTypeWork),
	editspec.FieldWorkCovers:              coverRows(),
	editspec.FieldWorkCoversUpstream:      coverRows(),
	editspec.FieldWorkScreenshots:         screenshotRows(),
	editspec.FieldWorkScreenshotsUpstream: screenshotRows(),
	editspec.FieldWorkCredits:             creditRows(),
	editspec.FieldWorkCreditsUpstream:     creditRows(),
	editspec.FieldWorkRoster: {
		from: "catalog_work_character wc", owner: "wc.work_id", sourceKey: "wc.matched_by",
		human:    curatedMatch("wc.matched_by"),
		identity: editspec.RosterIdentitySQL("wc"),
		value:    `json_build_object('character_id', wc.character_id, 'kind', wc.kind, 'spoiler', wc.spoiler)`,
		prov:     "wc.field_provenance",
		order:    "wc.character_id",
	},
	editspec.FieldWorkRelations: {
		from: `(SELECT r.a_work_id AS owner_id, r.b_work_id AS other_id, r.relation_type_id, 'f' AS dir,
		               r.note, r.source_id FROM catalog_work_relation r
		        UNION ALL
		        SELECT r.b_work_id, r.a_work_id, r.relation_type_id, 'r', r.note, r.source_id
		        FROM catalog_work_relation r) wr
			JOIN catalog_relation_type rt ON rt.id = wr.relation_type_id`,
		owner: "wr.owner_id", sourceID: "wr.source_id", human: humanLaneOf("wr.source_id"),
		identity: editspec.WorkRelationIdentitySQL("wr"),
		value: `json_build_object('work_id', wr.other_id, 'relation', rt.key,
			'reverse', wr.dir = 'r' AND NOT rt.is_symmetric, 'note', wr.note)`,
		order: "wr.source_id, rt.key, wr.other_id, wr.dir",
	},
	editspec.FieldWorkPlatforms: {
		from: "catalog_work_platform wp", owner: "wp.work_id", sourceID: "wp.source_id",
		human:    humanLaneOf("wp.source_id"),
		identity: editspec.WorkPlatformIdentitySQL("wp.platform"),
		value:    `to_json(wp.platform)`,
		order:    "wp.source_id, wp.platform",
	},
	// series_members and tag_spoilers regrade the rows series_ids and tag_ids
	// list; the per-row field_provenance is where their stamps land.
	editspec.FieldWorkSeriesMembers: {
		from:  "catalog_series_member sm JOIN catalog_series s ON s.id = sm.series_id",
		owner: "sm.work_id", sourceID: "s.source_id", human: humanLaneOf("s.source_id"),
		identity: editspec.SeriesIdentitySQL("s"),
		value:    `json_build_object('series_id', sm.series_id, 'position', sm.position, 'kind', sm.kind)`,
		prov:     "sm.field_provenance",
		order:    "s.source_id, sm.series_id",
	},
	editspec.FieldWorkTagSpoilers: {
		from: `catalog_work_tag wt
			LEFT JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name`,
		owner: "wt.work_id", sourceID: "wt.source_id", human: humanLaneOf("wt.source_id"),
		identity: fmt.Sprintf("CASE WHEN m.tag_id IS NULL THEN NULL ELSE %s END", editspec.WorkTagIdentitySQL("m.tag_id")),
		value:    `json_build_object('tag_id', m.tag_id, 'name', wt.name, 'spoiler', wt.spoiler)`,
		prov:     "wt.field_provenance",
		order:    "wt.source_id, m.tag_id NULLS LAST, wt.name",
	},
	editspec.FieldCharacterAliases: {
		from: "catalog_character_alias a", owner: "a.character_id", sourceID: "a.source_id",
		human:    humanLaneOf("a.source_id"),
		identity: editspec.CharacterAliasIdentitySQL("a"),
		value:    `json_build_object('name', a.name, 'lang', a.lang, 'kind', a.kind, 'latin', a.latin, 'primary', a.is_primary_for_locale)`,
		machine:  fmt.Sprintf("a.provenance = %d", catmodel.IntroProvenanceMachine),
		order:    "a.source_id, a.kind, a.lang, a.id",
	},
	editspec.FieldCharacterIntros: suppressibleIntroRows("catalog_character_intro", "character_id"),
	editspec.FieldCharacterTraits: {
		from: "catalog_character_trait_link l", owner: "l.character_id", sourceID: "l.source_id",
		human:    humanLaneOf("l.source_id"),
		identity: "('trait:' || l.trait_id)",
		value:    `json_build_object('trait_id', l.trait_id, 'spoiler', l.spoiler_level, 'lie', l.lie)`,
		order:    "l.source_id, l.trait_id",
	},
	editspec.FieldLabelLinks: linkRows(catmodel.EntityTypeLabel),
	editspec.FieldLabelAliases: {
		from: "catalog_label_alias la", owner: "la.label_id", sourceID: "la.source_id",
		human:    humanLaneOf("la.source_id"),
		identity: editspec.LabelAliasIdentitySQL("la"),
		value:    `json_build_object('name', la.name, 'lang', la.lang, 'kind', la.kind, 'latin', la.latin, 'primary', la.is_primary_for_locale)`,
		machine:  fmt.Sprintf("la.provenance = %d", catmodel.IntroProvenanceMachine),
		order:    "la.source_id, la.kind, la.lang, la.id",
	},
	editspec.FieldLabelRelations: {
		from: "catalog_label_relation lr", owner: "lr.label_id", sourceID: "lr.source_id",
		human:    humanLaneOf("lr.source_id"),
		identity: "('relation:' || lr.relation || ':' || lr.other_label_id)",
		value:    `json_build_object('label_id', lr.other_label_id, 'relation', lr.relation)`,
		order:    "lr.source_id, lr.relation, lr.other_label_id",
	},
	editspec.FieldLabelIntros:  suppressibleIntroRows("catalog_label_intro", "label_id"),
	editspec.FieldPersonIntros: introRows("catalog_person_intro", "person_id", true),
	editspec.FieldPersonLinks: {
		from:     "catalog_external_ref r JOIN catalog_source cs ON cs.id = r.source_id",
		owner:    fmt.Sprintf("r.entity_type = %d AND r.entity_id", catmodel.EntityTypePerson),
		sourceID: "r.source_id",
		human:    curatedMatch("r.matched_by"),
		identity: fmt.Sprintf("CASE WHEN r.link_kind = %d THEN 'link:' || cs.key || ':' || r.external_id END", catmodel.LinkKindRelated),
		value:    `json_build_object('external_id', r.external_id, 'link_kind', r.link_kind, 'matched_by', r.matched_by, 'dead', r.dead_at IS NOT NULL)`,
		order:    "r.source_id, r.external_id",
	},
	editspec.FieldCreditNameAliases: {
		from: "catalog_name_alias na", owner: "na.credit_name_id", sourceID: "na.source_id",
		human:    humanLaneOf("na.source_id"),
		identity: editspec.CharacterAliasIdentitySQL("na"),
		value:    `json_build_object('name', na.name, 'lang', na.lang, 'kind', na.kind, 'latin', na.latin, 'primary', na.is_primary_for_locale)`,
		machine:  fmt.Sprintf("na.provenance = %d", catmodel.IntroProvenanceMachine),
		order:    "na.source_id, na.kind, na.lang, na.id",
	},
	editspec.FieldTagIntros:    introRows("catalog_tag_intro", "tag_id", false),
	editspec.FieldSeriesIntros: introRows("catalog_series_intro", "series_id", false),
	editspec.FieldReleaseLabels: {
		from: "catalog_release_label rl", owner: "rl.release_id", sourceID: "rl.source_id",
		human:    humanLaneOf("rl.source_id"),
		identity: editspec.ReleaseLabelIdentitySQL("rl"),
		value:    `json_build_object('label_id', rl.label_id, 'kind', rl.kind)`,
		order:    "rl.source_id NULLS FIRST, rl.label_id, rl.kind",
	},
	editspec.FieldTagParents:   parentRows("catalog_tag_parent", "tag_id", catmodel.TagParentOriginCurated),
	editspec.FieldTraitParents: parentRows("catalog_character_trait_parent", "trait_id", catmodel.TraitParentOriginCurated),
	editspec.FieldWalkthroughPins: {
		from: "catalog_walkthrough_character l JOIN catalog_walkthrough wt ON wt.id = l.walkthrough_id", owner: "l.walkthrough_id",
		sourceKey: fmt.Sprintf(`CASE WHEN %s THEN '%s' ELSE (SELECT s.key FROM catalog_source s WHERE s.id = wt.source_id) END`,
			curatedMatch("l.matched_by"), provenance.SourceCurated),
		human:    curatedMatch("l.matched_by"),
		identity: "('pin:' || l.chart_key)",
		value:    `json_build_object('chart_key', l.chart_key, 'character_id', l.character_id, 'matched_by', l.matched_by)`,
		order:    "l.chart_key",
	},
}

func parentRows(table, childCol string, curatedOrigin int16) contextRowSource {
	return contextRowSource{
		from: table + " p", owner: "p." + childCol,
		sourceKey: fmt.Sprintf("CASE WHEN p.origin = %d THEN '%s' ELSE 'vndb' END", curatedOrigin, provenance.SourceCurated),
		human:     fmt.Sprintf("p.origin = %d", curatedOrigin),
		identity:  "('parent:' || p.parent_id)",
		value:     `json_build_object('parent_id', p.parent_id, 'origin', p.origin)`,
		order:     "p.origin DESC, p.parent_id",
	}
}

// snapshotOnlyListFields are the list fields deliberately absent from
// contextRowSources: each is stored as one value on the entity's own row, so
// there is no per-source row to list and the snapshot is the whole story.
var snapshotOnlyListFields = map[string]string{
	editspec.FieldEngineAliases:       "catalog_engine.aliases is one array column on the engine row",
	editspec.FieldReleasePlatforms:    "catalog_release.extra.platforms is one array on the release row",
	editspec.FieldReleaseLanguages:    "catalog_release.extra.languages is one array on the release row",
	editspec.FieldTraitAliases:        "catalog_character_trait.alias is one newline-separated column on the trait row",
	editspec.FieldWorkCoverOrder:      "one order a person sets per work, across every source; no per-source rows",
	editspec.FieldWorkScreenshotOrder: "one order a person sets per work, across every source; no per-source rows",
}

func HasContextRows(fieldKey string) bool {
	_, ok := contextRowSources[fieldKey]
	return ok
}

func ContextRowsPublishIdentity(fieldKey string) bool {
	return contextRowSources[fieldKey].identity != ""
}

func SnapshotOnlyListField(fieldKey string) bool {
	_, ok := snapshotOnlyListFields[fieldKey]
	return ok
}

func (src contextRowSource) query() string {
	nullOr := func(expr, typ string) string {
		if expr == "" {
			return "NULL::" + typ
		}
		return expr
	}
	boolOf := func(expr string) string {
		if expr == "" {
			return "false"
		}
		return "COALESCE(" + expr + ", false)"
	}
	return fmt.Sprintf(`
		SELECT %s AS source_id, %s AS source_key, %s AS human_lane, %s AS identity,
		       %s AS value, %s AS machine_translated, %s AS field_provenance
		FROM %s
		WHERE %s = ?
		ORDER BY %s
		LIMIT ?`,
		nullOr(src.sourceID, "smallint"), nullOr(src.sourceKey, "text"), boolOf(src.human),
		nullOr(src.identity, "text"), src.value, boolOf(src.machine), nullOr(src.prov, "jsonb"),
		src.from, src.owner, src.order)
}

func (s *EditContextService) SourceKeys(ctx context.Context) (map[int16]string, error) {
	var rows []catmodel.CatalogSource
	if err := s.db.WithContext(ctx).Select("id", "key").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int16]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Key
	}
	return out, nil
}

func (s *EditContextService) Rows(ctx context.Context, entityType, fieldKey string, entityID int64, sources map[int16]string) ([]ContextRow, bool, error) {
	src, ok := contextRowSources[fieldKey]
	if !ok {
		return nil, false, nil
	}
	var raw []struct {
		SourceID          *int16  `gorm:"column:source_id"`
		SourceKey         *string `gorm:"column:source_key"`
		HumanLane         bool    `gorm:"column:human_lane"`
		Identity          *string `gorm:"column:identity"`
		Value             []byte  `gorm:"column:value"`
		MachineTranslated bool    `gorm:"column:machine_translated"`
		FieldProvenance   []byte  `gorm:"column:field_provenance"`
	}
	if err := s.db.WithContext(ctx).Raw(src.query(), entityID, EditContextRowCap+1).Scan(&raw).Error; err != nil {
		return nil, false, fmt.Errorf("edit context rows for %s: %w", fieldKey, err)
	}
	truncated := len(raw) > EditContextRowCap
	if truncated {
		raw = raw[:EditContextRowCap]
	}

	suppressed := map[string]struct{}{}
	if src.identity != "" {
		keys, err := editing.LoadSuppressedKeys(ctx, s.db, entityType, entityID, fieldKey)
		if err != nil {
			return nil, false, err
		}
		for _, k := range keys {
			suppressed[k] = struct{}{}
		}
	}

	out := make([]ContextRow, 0, len(raw)+len(suppressed))
	seen := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		row := ContextRow{
			HumanLane: r.HumanLane, Present: true, Value: json.RawMessage(r.Value),
			MachineTranslated: r.MachineTranslated,
		}
		if len(r.FieldProvenance) > 0 {
			row.FieldProvenance = json.RawMessage(r.FieldProvenance)
		}
		switch {
		case r.SourceKey != nil:
			row.SourceKey = *r.SourceKey
		case r.SourceID != nil:
			row.SourceKey = sources[*r.SourceID]
		}
		if r.Identity != nil {
			row.Identity = *r.Identity
			seen[row.Identity] = struct{}{}
			_, row.Suppressed = suppressed[row.Identity]
		}
		out = append(out, row)
	}
	orphans := make([]string, 0)
	for k := range suppressed {
		if _, ok := seen[k]; !ok {
			orphans = append(orphans, k)
		}
	}
	sort.Strings(orphans)
	for _, k := range orphans {
		out = append(out, ContextRow{Identity: k, Suppressed: true})
	}
	return out, truncated, nil
}

func (s *EditContextService) HeadProvenance(ctx context.Context, table string, entityID int64) (map[string][]provenance.Entry, error) {
	var raw []byte
	err := s.db.WithContext(ctx).Raw(
		fmt.Sprintf(`SELECT field_provenance FROM %s WHERE id = ?`, table), entityID,
	).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string][]provenance.Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("edit context provenance of %s: %w", table, err)
	}
	out := map[string][]provenance.Entry{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}
