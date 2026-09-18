package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	bangumiSource int16 = 3
	dlsiteSource  int16 = 4
	egSource      int16 = 5

	ruleBangumiPerson = "rule:bangumi-person-import"
	ruleBangumiChar   = "rule:bangumi-character-import"
	ruleEGCreater     = "rule:eg-creater-import"
	ruleEGChar        = "rule:eg-character-import"
	ruleDLsiteWork    = "rule:dlsite-work-import"
	ruleDLsiteMaker   = "rule:dlsite-maker-import"
	ruleDLsiteCreater = "rule:dlsite-creater-import"
	ruleBangumiXmedia = "rule:bangumi-xmedia-import"

	roleVoiceActor int64 = 1
	mediumASMR     int16 = 5
)

type Options struct {
	Source           string
	DryRun           bool
	Limit            int
	ResolveAmbiguous bool
	ConflictsOut     string
	StaleAnchorsOut  string
}

type Stats struct {
	NamesCreated               int
	LabelsCreated              int
	CharactersCreated          int
	CreditsWritten             int
	SkippedUnmappedRole        int
	SkippedGate                int
	SkippedClaimedProbableName int
	SkippedClaimedProbableChar int
	SkippedRetiredExactChar    int
	Already                    int
	Errors                     int
}

func (s *Stats) add(o Stats) {
	s.NamesCreated += o.NamesCreated
	s.LabelsCreated += o.LabelsCreated
	s.CharactersCreated += o.CharactersCreated
	s.CreditsWritten += o.CreditsWritten
	s.SkippedUnmappedRole += o.SkippedUnmappedRole
	s.SkippedGate += o.SkippedGate
	s.SkippedClaimedProbableName += o.SkippedClaimedProbableName
	s.SkippedClaimedProbableChar += o.SkippedClaimedProbableChar
	s.SkippedRetiredExactChar += o.SkippedRetiredExactChar
	s.Already += o.Already
	s.Errors += o.Errors
}

type Importer struct {
	catalog          *gorm.DB
	eg               *gorm.DB
	dryRun           bool
	limit            int
	resolveAmbiguous bool
	conflictsOut     string
	staleAnchorsOut  string
}

func New(catalog, eg *gorm.DB, opts Options) *Importer {
	return &Importer{
		catalog: catalog, eg: eg, dryRun: opts.DryRun, limit: opts.Limit,
		resolveAmbiguous: opts.ResolveAmbiguous, conflictsOut: opts.ConflictsOut,
		staleAnchorsOut: opts.StaleAnchorsOut,
	}
}

func (im *Importer) Run(source string) (Stats, error) {
	var total Stats
	if source == "bangumi" || source == "all" {
		s, err := im.runBangumi()
		if err != nil {
			return total, fmt.Errorf("bangumi wave: %w", err)
		}
		total.add(s)
	}
	if source == "eg" || source == "all" {
		if im.eg == nil {
			return total, fmt.Errorf("eg wave requested but no erogamescape connection")
		}
		s, err := im.runEG()
		if err != nil {
			return total, fmt.Errorf("eg wave: %w", err)
		}
		total.add(s)
	}
	if source == "eg-music" || source == "all" {
		if im.eg == nil {
			return total, fmt.Errorf("eg-music wave requested but no erogamescape connection")
		}
		s, err := im.runEGMusic()
		if err != nil {
			return total, fmt.Errorf("eg music wave: %w", err)
		}
		total.add(s)
	}
	if source == "vndb" || source == "all" {
		s, err := im.runVNDBCredits()
		if err != nil {
			return total, fmt.Errorf("vndb credits wave: %w", err)
		}
		total.add(s)
	}
	return total, nil
}

func (im *Importer) loadExactLiveWorkMap(source int16) (map[int64]int64, error) {
	var rows []struct {
		ExternalID int64 `gorm:"column:external_id"`
		WorkID     int64 `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id::bigint AS external_id, r.entity_id AS work_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeWork, source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.WorkID
	}
	return m, nil
}

func (im *Importer) loadEGKnownWorks() (map[int64][]int64, error) {
	var rows []struct {
		ExternalID int64 `gorm:"column:external_id"`
		WorkID     int64 `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id::bigint AS external_id, r.entity_id AS work_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind IN (?, ?, ?) AND r.dead_at IS NULL`,
		model.EntityTypeWork, egSource, model.LinkKindExact, model.LinkKindProbable, model.LinkKindRelated).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]int64{}
	seen := map[[2]int64]struct{}{}
	for _, r := range rows {
		key := [2]int64{r.ExternalID, r.WorkID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		m[r.ExternalID] = append(m[r.ExternalID], r.WorkID)
	}
	return m, nil
}

func anchorKey(source int16, extID string) string { return fmt.Sprintf("%d|%s", source, extID) }

func (im *Importer) loadAnchors(entityType int16) (map[string]int64, error) {
	var rows []struct {
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := im.catalog.Raw(
		`SELECT source_id, external_id, entity_id FROM catalog_external_ref
		 WHERE entity_type = ? AND source_id IN (?, ?, ?)`, entityType, bangumiSource, dlsiteSource, egSource,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[anchorKey(r.SourceID, r.ExternalID)] = r.EntityID
	}
	return m, nil
}

func (im *Importer) roleMap(source int16) (map[string]int64, error) {
	var rows []struct {
		SourceRole string `gorm:"column:source_role"`
		RoleID     int64  `gorm:"column:role_id"`
	}
	if err := im.catalog.Raw(`SELECT source_role, role_id FROM catalog_source_role_map WHERE source_id = ?`, source).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.SourceRole] = r.RoleID
	}
	return m, nil
}

type nameItem struct{ extID, name, lang string }
type labelItem struct {
	extID, name, lang string
	kind              int16
}
type charItem struct{ extID, name, lang string }

func entitySnapshot(kind string, entity any) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{kind: entity, "aliases": []any{}})
	return b
}

func (im *Importer) batchRefsRevs(tx *gorm.DB, refs []model.CatalogExternalRef, revs []model.CatalogRevision) error {
	if err := tx.CreateInBatches(refs, 1000).Error; err != nil {
		return err
	}
	return tx.CreateInBatches(revs, 1000).Error
}

func (im *Importer) createCreditNames(tx *gorm.DB, source int16, rule string, items []nameItem) (map[string]int64, error) {
	out := make(map[string]int64, len(items))
	if len(items) == 0 {
		return out, nil
	}
	rows := make([]model.CatalogCreditName, len(items))
	for i, it := range items {
		rows[i] = model.CatalogCreditName{Name: it.name, Lang: it.lang, Kind: model.CreditNameKindMain, LinkVisibility: model.LinkVisibilityPublic}
	}
	if err := tx.CreateInBatches(rows, 1000).Error; err != nil {
		return nil, err
	}
	refs := make([]model.CatalogExternalRef, len(rows))
	revs := make([]model.CatalogRevision, len(rows))
	for i, r := range rows {
		out[items[i].extID] = r.ID
		refs[i] = selfRef(model.EntityTypeCreditName, r.ID, source, items[i].extID, rule)
		revs[i] = importedRev(model.EntityTypeCreditName, r.ID, entitySnapshot("credit_name", r))
	}
	return out, im.batchRefsRevs(tx, refs, revs)
}

func (im *Importer) createLabels(tx *gorm.DB, source int16, rule string, items []labelItem) (map[string]int64, error) {
	out := make(map[string]int64, len(items))
	if len(items) == 0 {
		return out, nil
	}
	rows := make([]model.CatalogLabel, len(items))
	for i, it := range items {
		rows[i] = model.CatalogLabel{DisplayName: it.name, Lang: it.lang, Kind: it.kind}
	}
	if err := tx.CreateInBatches(rows, 1000).Error; err != nil {
		return nil, err
	}
	refs := make([]model.CatalogExternalRef, len(rows))
	revs := make([]model.CatalogRevision, len(rows))
	for i, r := range rows {
		out[items[i].extID] = r.ID
		refs[i] = selfRef(model.EntityTypeLabel, r.ID, source, items[i].extID, rule)
		revs[i] = importedRev(model.EntityTypeLabel, r.ID, entitySnapshot("label", r))
	}
	return out, im.batchRefsRevs(tx, refs, revs)
}

func (im *Importer) createCharacters(tx *gorm.DB, source int16, rule string, items []charItem) (map[string]int64, error) {
	out := make(map[string]int64, len(items))
	if len(items) == 0 {
		return out, nil
	}
	rows := make([]model.CatalogCharacter, len(items))
	for i, it := range items {
		rows[i] = model.CatalogCharacter{DisplayName: it.name, Lang: it.lang}
	}
	if err := tx.CreateInBatches(rows, 1000).Error; err != nil {
		return nil, err
	}
	refs := make([]model.CatalogExternalRef, len(rows))
	revs := make([]model.CatalogRevision, len(rows))
	for i, r := range rows {
		out[items[i].extID] = r.ID
		refs[i] = selfRef(model.EntityTypeCharacter, r.ID, source, items[i].extID, rule)
		revs[i] = importedRev(model.EntityTypeCharacter, r.ID, entitySnapshot("character", r))
	}
	return out, im.batchRefsRevs(tx, refs, revs)
}

func selfRef(etype int16, id int64, source int16, extID, rule string) model.CatalogExternalRef {
	return model.CatalogExternalRef{
		EntityType: etype, EntityID: id, SourceID: source, ExternalID: extID,
		LinkKind: model.LinkKindExact, MatchedBy: rule,
	}
}

func touchWorks(db *gorm.DB, workIDs []int64) error {
	return repository.TouchWorks(context.Background(), db, workIDs)
}

func insertWorkLabelEdges(db *gorm.DB, edges []model.CatalogWorkLabel) ([]int64, error) {
	var touched []int64
	const batch = 1000
	for start := 0; start < len(edges); start += batch {
		end := min(start+batch, len(edges))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO catalog_work_label (work_id, label_id, kind, source_id) VALUES `)
		args := make([]any, 0, (end-start)*4)
		for i := start; i < end; i++ {
			e := edges[i]
			if i > start {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?)")
			args = append(args, e.WorkID, e.LabelID, e.Kind, e.SourceID)
		}
		sb.WriteString(` ON CONFLICT DO NOTHING RETURNING work_id`)
		var hosts []int64
		if err := db.Raw(sb.String(), args...).Scan(&hosts).Error; err != nil {
			return nil, err
		}
		touched = append(touched, hosts...)
	}
	return touched, nil
}

func relationEndpoints(edges []model.CatalogWorkRelation) []int64 {
	out := make([]int64, 0, len(edges)*2)
	for _, e := range edges {
		out = append(out, e.AWorkID, e.BWorkID)
	}
	return out
}

func importedRev(etype int16, id int64, snap datatypes.JSON) model.CatalogRevision {
	return model.CatalogRevision{
		EntityType: etype, EntityID: id, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: snap, IsMinor: false,
	}
}

func (im *Importer) insertCredits(tx *gorm.DB, credits []model.CatalogCredit) (int, error) {
	written := 0
	var touched []int64
	const batch = 1000
	for start := 0; start < len(credits); start += batch {
		end := min(start+batch, len(credits))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO catalog_credit (work_id, credit_name_id, label_id, role_id, character_id, spoiler, note, source_id, created_at, updated_at) VALUES `)
		args := make([]any, 0, (end-start)*8)
		for i := start; i < end; i++ {
			c := credits[i]
			if i > start {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?,?,?,?,?,now(),now())")
			args = append(args, c.WorkID, c.CreditNameID, c.LabelID, c.RoleID, c.CharacterID, c.Spoiler, c.Note, c.SourceID)
		}
		sb.WriteString(` ON CONFLICT (work_id, credit_name_id, role_id, COALESCE(character_id, 0)) DO NOTHING RETURNING work_id`)
		var hosts []int64
		if err := tx.Raw(sb.String(), args...).Scan(&hosts).Error; err != nil {
			return written, err
		}
		written += len(hosts)
		touched = append(touched, hosts...)
	}
	return written, touchWorks(tx, touched)
}

func resolver(anchors map[string]int64, source int16, fresh map[string]int64) func(string) (int64, bool) {
	return func(ext string) (int64, bool) {
		if id, ok := fresh[ext]; ok {
			return id, true
		}
		if id, ok := anchors[anchorKey(source, ext)]; ok && id != 0 {
			return id, true
		}
		return 0, false
	}
}

func materialize(plans []creditPlan, cn, label, char func(string) (int64, bool), source int16) ([]model.CatalogCredit, int) {
	src := source
	out := make([]model.CatalogCredit, 0, len(plans))
	dropped := 0
	for _, p := range plans {
		cnID, ok := cn(p.cnExtID)
		if !ok {
			dropped++
			continue
		}
		c := model.CatalogCredit{
			WorkID: p.workID, CreditNameID: cnID, RoleID: p.roleID,
			Spoiler: model.SpoilerNone, Note: p.note, SourceID: &src,
		}
		if p.labelExt != "" {
			if lid, ok := label(p.labelExt); ok {
				c.LabelID = &lid
			}
		}
		if p.charExtID != "" {
			chid, ok := char(p.charExtID)
			if !ok {
				dropped++
				continue
			}
			c.CharacterID = &chid
		}
		out = append(out, c)
	}
	return out, dropped
}

func int64Keys(m map[int64]int64) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func capMap(m map[int64]int64, n int) map[int64]int64 {
	keys := int64Keys(m)
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if n < len(keys) {
		keys = keys[:n]
	}
	out := make(map[int64]int64, len(keys))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}
