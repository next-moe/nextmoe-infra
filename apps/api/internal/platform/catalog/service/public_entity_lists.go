package service

import (
	"context"
	"strings"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

const (
	taxonomyLaneCharacters = "characters"
	taxonomyLaneNames      = "credit_names"
	taxonomyLanePersons    = "persons"
	taxonomyLaneTraits     = "traits"
)

type EntityListRow struct {
	ID                  int64
	DisplayName         string
	Latin               *string
	Lang                string
	PersonID            *int64
	VndbTID             string
	NameZh              string
	Sexual              bool
	PrimaryCreditNameID *int64
	Gender              *int16
	Localized           map[string]dto.PublicLocalizedName
	Attrs               *CharacterAttributes
	Image, Figure       string
	ImageMeta           *dto.PublicImageMeta
	FigureMeta          *dto.PublicImageMeta
	Aliases             []dto.PublicAlias
	Intros              []dto.PublicIntro
	Refs                []dto.PublicCatalogRef
	Traits              []dto.PublicCharacterTrait
}

// entityListScan is the Scan destination behind EntityListRow: column-backed
// fields only. Scanning into EntityListRow itself made GORM log
// `failed to parse field: Localized, error: unsupported data type: &map[]` on
// every entity-list read, once per call site.
type entityListScan struct {
	ID          int64
	DisplayName string
	Latin       *string
	Lang        string
	PersonID    *int64
	// GORM snake-cases VndbTID to vndb_t_id, which matches no result column, so
	// every /v2/catalog/traits row shipped vndb_tid:"" until 2026-08-28.
	VndbTID             string `gorm:"column:vndb_tid"`
	NameZh              string
	Sexual              bool
	PrimaryCreditNameID *int64
	Gender              *int16
}

func entityListRows(scanned []entityListScan) []EntityListRow {
	rows := make([]EntityListRow, len(scanned))
	for i, r := range scanned {
		rows[i] = EntityListRow{
			ID: r.ID, DisplayName: r.DisplayName, Latin: r.Latin, Lang: r.Lang,
			PersonID: r.PersonID, VndbTID: r.VndbTID, NameZh: r.NameZh, Sexual: r.Sexual,
			PrimaryCreditNameID: r.PrimaryCreditNameID, Gender: r.Gender,
		}
	}
	return rows
}

type EntityListPage struct {
	Items      []EntityListRow
	NextCursor *string
	Total      int64
}

type CharacterAttributes struct {
	Gender        *int16
	BirthdayMonth *int16
	BirthdayDay   *int16
	BloodType     *int16
	HeightCm      *int16
	WeightKg      *int16
	BustCm        *int16
	WaistCm       *int16
	HipCm         *int16
	Cup           *string
	InstanceOf    *int64
}

type PersonRow struct {
	ID                  int64
	DisplayName         string
	PrimaryCreditNameID *int64
	Gender              *int16
}

type TraitRow struct {
	ID          int64
	DisplayName string
	NameZh      string
	VndbTID     string `gorm:"column:vndb_tid"`
	Sexual      bool
	Description string
}

// CharacterListInclude is the character list lane's half of collect.CharacterSpec.
// The spec has declared every one of these tokens since wave 3 and the lane
// filled none of them: include=traits answered 200 with no block and view=full
// returned a key set byte-identical to basic.
type CharacterListInclude struct {
	Attributes bool
	Image      bool
	Figure     bool
	Traits     bool
	Aliases    bool
	Intros     bool
	Refs       bool
}

func CharacterListIncludeFrom(tokens []string) CharacterListInclude {
	var inc CharacterListInclude
	for _, t := range tokens {
		switch t {
		case "gender", "birthday", "height_cm", "weight_kg", "measurements", "blood_type", "instance_of_id":
			inc.Attributes = true
		case "image":
			inc.Image = true
		case "figure":
			inc.Figure = true
		case "traits":
			inc.Traits = true
		case "aliases":
			inc.Aliases = true
		case "intros":
			inc.Intros = true
		case "refs":
			inc.Refs = true
		}
	}
	return inc
}

func (inc CharacterListInclude) any() bool {
	return inc.Attributes || inc.Image || inc.Figure || inc.Traits || inc.Aliases || inc.Intros || inc.Refs
}

func (s *PublicService) CharactersList(ctx context.Context, ids []int64, cursor string, limit int, inc CharacterListInclude, nsfw bool, filter CharacterTraitFilter, includeTotal bool) (EntityListPage, error) {
	spec := entityListSpec{
		lane:      taxonomyLaneCharacters,
		table:     "catalog_character",
		selectSQL: "id, display_name, latin, lang",
		deleted:   true,
		ids:       ids, cursor: cursor, limit: limit,
		alias: "character",
	}
	if len(filter.TraitIDs) > 0 {
		if _, err := decodePublicCursor(cursor, taxonomyLaneCharacters); err != nil {
			return EntityListPage{}, err
		}
		sets, err := s.expandCharacterTraits(ctx, filter.TraitIDs, nsfw)
		if err != nil {
			return EntityListPage{}, err
		}
		where, args, empty := characterTraitWhere(sets, filter.MatchAny)
		if empty {
			return EntityListPage{Items: []EntityListRow{}}, nil
		}
		spec.extraWhere = where
		spec.extraArgs = args
		spec.skipTotal = !includeTotal
	}
	page, err := s.entityIDList(ctx, spec)
	if err != nil || !inc.any() {
		return page, err
	}
	if err := s.attachCharacterListBlocks(ctx, page.Items, inc, nsfw); err != nil {
		return EntityListPage{}, err
	}
	return page, nil
}

func (s *PublicService) NamesList(ctx context.Context, ids []int64, q, cursor string, limit int) (EntityListPage, error) {
	return s.entityIDList(ctx, entityListSpec{
		lane:  taxonomyLaneNames,
		table: "catalog_credit_name",
		// The person link is gated exactly as the detail face gates it
		// (read_service.go NameWorks): a hidden link publishes no person_id.
		selectSQL: "id, name AS display_name, latin, lang, " +
			"CASE WHEN link_visibility = 0 THEN person_id END AS person_id",
		ids: ids, q: q, qCol: "name", cursor: cursor, limit: limit,
		alias: "name",
	})
}

func (s *PublicService) PersonsList(ctx context.Context, ids []int64, cursor string, limit int) (EntityListPage, error) {
	return s.entityIDList(ctx, entityListSpec{
		lane:      taxonomyLanePersons,
		table:     "catalog_person",
		selectSQL: "id, display_name, primary_credit_name_id, gender",
		deleted:   true,
		ids:       ids, cursor: cursor, limit: limit,
	})
}

func (s *PublicService) TraitsList(ctx context.Context, ids []int64, cursor string, limit int) (EntityListPage, error) {
	return s.entityIDList(ctx, entityListSpec{
		lane:      taxonomyLaneTraits,
		table:     "catalog_character_trait",
		selectSQL: "id, name AS display_name, name_zh, vndb_tid, sexual",
		ids:       ids, cursor: cursor, limit: limit,
	})
}

type entityListSpec struct {
	lane, table, selectSQL, q, qCol, alias, cursor string
	deleted                                        bool
	ids                                            []int64
	limit                                          int
	extraWhere                                     []string
	extraArgs                                      []any
	skipTotal                                      bool
}

func (s *PublicService) entityIDList(ctx context.Context, spec entityListSpec) (EntityListPage, error) {
	cur, err := decodePublicCursor(spec.cursor, spec.lane)
	if err != nil {
		return EntityListPage{}, err
	}
	limit := clampBrowseLimit(spec.limit)
	var where []string
	var args []any
	if spec.deleted {
		where = append(where, "deleted_at IS NULL")
	}
	if len(spec.ids) > 0 {
		where = append(where, "id IN ?")
		args = append(args, spec.ids)
	}
	if spec.q != "" && spec.qCol != "" {
		where = append(where, spec.qCol+` ILIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLikePattern(spec.q)+"%")
	}
	if len(spec.extraWhere) > 0 {
		where = append(where, spec.extraWhere...)
		args = append(args, spec.extraArgs...)
	}
	filterWhere, filterArgs := append([]string(nil), where...), append([]any(nil), args...)
	if cur.ID > 0 {
		where = append(where, "id > ?")
		args = append(args, cur.ID)
	}
	q := `SELECT ` + spec.selectSQL + ` FROM ` + spec.table + ` ` + whereClause(where) + ` ORDER BY id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit+taxonomyOverFetch, spec.ids)
	var scanned []entityListScan
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&scanned).Error; err != nil {
		return EntityListPage{}, err
	}
	rows := entityListRows(scanned)
	var more bool
	if paginated {
		rows, more = taxonomyTrim(rows, limit)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	if spec.alias != "" {
		loc, lerr := s.LocalizedForEntities(ctx, spec.alias, ids)
		if lerr != nil {
			return EntityListPage{}, lerr
		}
		for i := range rows {
			rows[i].Localized = loc[rows[i].ID]
		}
	}
	out := EntityListPage{Items: rows, NextCursor: taxonomyNextCursor(spec.lane, ids, more)}
	if !spec.skipTotal {
		if out.Total, err = s.taxonomyTotal(ctx, spec.table, filterWhere, filterArgs); err != nil {
			return EntityListPage{}, err
		}
	}
	return out, nil
}

func (s *PublicService) CharacterAttributes(ctx context.Context, id int64) (CharacterAttributes, bool, error) {
	var row CharacterAttributes
	err := s.db.WithContext(ctx).Raw(
		`SELECT gender, birthday_month, birthday_day, blood_type, height_cm, weight_kg,
			bust_cm, waist_cm, hip_cm, cup, instance_of
		 FROM catalog_character WHERE id = ? AND deleted_at IS NULL`, id).Scan(&row).Error
	if err != nil {
		return CharacterAttributes{}, false, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM catalog_character WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n).Error; err != nil {
		return CharacterAttributes{}, false, err
	}
	return row, n > 0, nil
}

func (s *PublicService) Person(ctx context.Context, id int64) (PersonRow, bool, error) {
	var row PersonRow
	err := s.db.WithContext(ctx).Raw(
		`SELECT id, display_name, primary_credit_name_id, gender
		 FROM catalog_person WHERE id = ? AND deleted_at IS NULL`, id).Scan(&row).Error
	if err != nil {
		return PersonRow{}, false, err
	}
	if row.ID == 0 {
		return PersonRow{}, false, nil
	}
	return row, true, nil
}

func (s *PublicService) PersonNames(ctx context.Context, personID int64) ([]EntityListRow, bool, error) {
	if _, ok, err := s.Person(ctx, personID); err != nil || !ok {
		return nil, ok, err
	}
	var scanned []entityListScan
	// link_visibility is the public gate the detail face applies to the same
	// edge (read_service.go NameWorks + its siblings query); listing a hidden
	// link here would have published the association the detail face refuses.
	err := s.db.WithContext(ctx).Raw(
		`SELECT id, name AS display_name, latin, lang, person_id
		 FROM catalog_credit_name WHERE person_id = ? AND link_visibility = ? ORDER BY id ASC`,
		personID, model.LinkVisibilityPublic).Scan(&scanned).Error
	if err != nil {
		return nil, false, err
	}
	rows := entityListRows(scanned)
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	loc, err := s.LocalizedForEntities(ctx, "name", ids)
	if err != nil {
		return nil, false, err
	}
	for i := range rows {
		rows[i].Localized = loc[rows[i].ID]
	}
	return rows, true, nil
}

func (s *PublicService) Trait(ctx context.Context, id int64) (TraitRow, bool, error) {
	var row TraitRow
	err := s.db.WithContext(ctx).Raw(
		`SELECT id, name AS display_name, name_zh, vndb_tid, sexual, description
		 FROM catalog_character_trait WHERE id = ?`, id).Scan(&row).Error
	if err != nil {
		return TraitRow{}, false, err
	}
	if row.ID == 0 {
		return TraitRow{}, false, nil
	}
	return row, true, nil
}

func NormalizeNameQuery(q string) string {
	return strings.TrimSpace(q)
}

// The caller's q= is a substring, not a pattern. Interpolated raw it was one:
// credit-names?q=%25 matched every row and scanned the whole table, and q=a_b
// matched axb.
func escapeLikePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

func (s *PublicService) attachCharacterListBlocks(ctx context.Context, rows []EntityListRow, inc CharacterListInclude, nsfw bool) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	if inc.Attributes {
		attrs, err := s.characterAttributesBatch(ctx, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			if a, ok := attrs[rows[i].ID]; ok {
				rows[i].Attrs = &a
			}
		}
	}
	if inc.Image || inc.Figure {
		if err := s.attachCharacterListArt(ctx, rows, ids, inc); err != nil {
			return err
		}
	}
	if inc.Aliases {
		aliases, err := s.entityAliasesBatch(ctx, characterAliasSource, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			rows[i].Aliases = richAliases(aliases[rows[i].ID])
		}
	}
	if inc.Refs {
		refs, err := s.entityRefsFor(ctx, model.EntityTypeCharacter, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			if rows[i].Refs = refs[rows[i].ID]; rows[i].Refs == nil {
				rows[i].Refs = []dto.PublicCatalogRef{}
			}
		}
	}
	if inc.Intros {
		intros, err := s.characterIntrosBatch(ctx, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			rows[i].Intros = intros[rows[i].ID]
		}
	}
	if inc.Traits {
		traits, err := s.characterTraitsBatch(ctx, ids, nsfw)
		if err != nil {
			return err
		}
		for i := range rows {
			rows[i].Traits = traits[rows[i].ID]
		}
	}
	return nil
}

func (s *PublicService) attachCharacterListArt(ctx context.Context, rows []EntityListRow, ids []int64, inc CharacterListInclude) error {
	var art []struct {
		ID         int64   `gorm:"column:id"`
		ImageHash  *string `gorm:"column:image_hash"`
		FigureHash *string `gorm:"column:figure_hash"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, image_hash, figure_hash FROM catalog_character WHERE id IN ?`, ids).
		Scan(&art).Error; err != nil {
		return err
	}
	hashes := make([]string, 0, 2*len(art))
	byID := make(map[int64]struct{ image, figure string }, len(art))
	for _, a := range art {
		pair := struct{ image, figure string }{}
		if inc.Image && a.ImageHash != nil {
			pair.image = *a.ImageHash
		}
		if inc.Figure && a.FigureHash != nil {
			pair.figure = *a.FigureHash
		}
		hashes = append(hashes, pair.image, pair.figure)
		byID[a.ID] = pair
	}
	meta := s.entityMetaFor(ctx, hashes...)
	for i := range rows {
		pair := byID[rows[i].ID]
		if pair.image != "" {
			if rows[i].Image = s.imageURL(pair.image); rows[i].Image != "" {
				rows[i].ImageMeta = publicImageMeta(meta, pair.image)
			}
		}
		if pair.figure != "" {
			if rows[i].Figure = s.imageURL(pair.figure); rows[i].Figure != "" {
				rows[i].FigureMeta = publicImageMeta(meta, pair.figure)
			}
		}
	}
	return nil
}

func (s *PublicService) characterAttributesBatch(ctx context.Context, ids []int64) (map[int64]CharacterAttributes, error) {
	var rows []struct {
		ID int64 `gorm:"column:id"`
		CharacterAttributes
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, gender, birthday_month, birthday_day, blood_type, height_cm, weight_kg,
			bust_cm, waist_cm, hip_cm, cup, instance_of
		 FROM catalog_character WHERE id IN ? AND deleted_at IS NULL`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]CharacterAttributes, len(rows))
	for _, r := range rows {
		out[r.ID] = r.CharacterAttributes
	}
	return out, nil
}

func (s *PublicService) characterIntrosBatch(ctx context.Context, ids []int64) (map[int64][]dto.PublicIntro, error) {
	var rows []struct {
		CharacterID int64  `gorm:"column:character_id"`
		Lang        string `gorm:"column:lang"`
		Intro       string `gorm:"column:intro"`
		SourceID    int16  `gorm:"column:source_id"`
		Provenance  int16  `gorm:"column:provenance"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT character_id, lang, intro, source_id, provenance
		FROM catalog_character_intro WHERE character_id IN ?
		ORDER BY character_id, lang, provenance, `+editspec.HumanLaneFirstSQL("source_id", "provenance")+
		`, (provenance = 1 AND source_id = ?) DESC, source_id`,
		ids, sourceDerived).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]dto.PublicIntro, len(ids))
	seen := map[[2]any]bool{}
	for _, r := range rows {
		key := [2]any{r.CharacterID, r.Lang}
		if seen[key] {
			continue
		}
		seen[key] = true
		out[r.CharacterID] = append(out[r.CharacterID], dto.PublicIntro{
			Lang: r.Lang, Intro: r.Intro, Source: s.sourceKey(r.SourceID), Machine: r.Provenance == 1,
		})
	}
	return out, nil
}

func (s *PublicService) characterTraitsBatch(ctx context.Context, ids []int64, nsfw bool) (map[int64][]dto.PublicCharacterTrait, error) {
	var rows []struct {
		CharacterID     int64 `gorm:"column:character_id"`
		ID              int64
		Name            string
		NameZh          string `gorm:"column:name_zh"`
		NameZhProv      int16  `gorm:"column:name_zh_provenance"`
		GroupName       *string
		GroupNameZh     *string `gorm:"column:group_name_zh"`
		GroupNameZhProv *int16  `gorm:"column:group_name_zh_provenance"`
		Sexual          bool
		SpoilerLevel    int16
		Lie             bool
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT l.character_id, t.id, t.name, t.name_zh, t.name_zh_provenance,
			g.name AS group_name, g.name_zh AS group_name_zh,
			g.name_zh_provenance AS group_name_zh_provenance,
			t.sexual, l.spoiler_level, l.lie
		FROM catalog_character_trait_link l
		JOIN catalog_character_trait t ON t.id = l.trait_id
		LEFT JOIN catalog_character_trait g ON g.vndb_tid = t.group_tid
		WHERE l.character_id IN ? AND l.spoiler_level <= ? AND (? OR NOT t.sexual)
		ORDER BY l.character_id, t.group_tid, t.gorder, t.name`,
		ids, model.SpoilerNone, nsfw).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]dto.PublicCharacterTrait, len(ids))
	for _, r := range rows {
		out[r.CharacterID] = append(out[r.CharacterID], dto.PublicCharacterTrait{
			ID: r.ID, Name: r.Name, Group: derefStrPub(r.GroupName),
			Localized:      traitLocalized(r.NameZh, r.NameZhProv),
			GroupLocalized: traitLocalized(derefStrPub(r.GroupNameZh), derefI16Pub(r.GroupNameZhProv)),
			Spoiler:        r.SpoilerLevel, Sexual: r.Sexual, Lie: r.Lie,
		})
	}
	return out, nil
}
