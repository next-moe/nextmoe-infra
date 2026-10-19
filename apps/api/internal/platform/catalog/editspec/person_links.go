package editspec

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	rePersonVNDB    = regexp.MustCompile(`(?i)^https?://(?:www\.)?vndb\.org/(s\d+)(?:[/?#]|$)`)
	rePersonBangumi = regexp.MustCompile(`(?i)^https?://(?:www\.)?(?:bgm\.tv|bangumi\.tv|chii\.in)/person/(\d+)`)
	rePersonEGS     = regexp.MustCompile(`(?i)^https?://erogamescape\.(?:dyndns\.org|org)/~ap2/ero/toukei_kaiseki/creater\.php\?creater=(\d+)`)
	reLinkSourceKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

var personAnchorTemplates = map[string]string{
	"vndb":         "https://vndb.org/%s",
	"bangumi":      "https://bgm.tv/person/%s",
	"erogamescape": "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/creater.php?creater=%s",
}

// A person's vndb/bangumi/EGS page is an identity anchor: a human may only
// assert it as probable, never exact, because exact anchors are the squatting
// defence the importers match on (doc 10).
func classifyPersonLink(raw string) (classifiedLink, error) {
	u := strings.TrimSpace(raw)
	if m := rePersonVNDB.FindStringSubmatch(u); m != nil {
		return classifiedLink{"vndb", strings.ToLower(m[1]), catmodel.LinkKindProbable}, nil
	}
	if m := rePersonBangumi.FindStringSubmatch(u); m != nil {
		return classifiedLink{"bangumi", m[1], catmodel.LinkKindProbable}, nil
	}
	if m := rePersonEGS.FindStringSubmatch(u); m != nil {
		return classifiedLink{"erogamescape", m[1], catmodel.LinkKindProbable}, nil
	}
	cl, ok := classifyWorkLink(u)
	if !ok {
		return classifiedLink{}, fmt.Errorf("must be an http:// or https:// URL")
	}
	if cl.LinkKind != catmodel.LinkKindRelated {
		return classifiedLink{}, fmt.Errorf("%s is a work page, not a person page", u)
	}
	return cl, nil
}

func canonicalPersonLinkURL(cl classifiedLink) string {
	if cl.LinkKind != catmodel.LinkKindRelated {
		if tpl, ok := personAnchorTemplates[cl.SourceKey]; ok {
			return fmt.Sprintf(tpl, cl.ExternalID)
		}
	}
	return canonicalLinkURL(cl)
}

func parsePersonLinks(v any) ([]classifiedLink, error) {
	arr, err := asArray(v, "URL strings")
	if err != nil {
		return nil, err
	}
	out := make([]classifiedLink, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		s, ok := el.(string)
		if !ok {
			return nil, fmt.Errorf("element %d: must be a URL string", i)
		}
		if len([]rune(strings.TrimSpace(s))) > maxURLRunes {
			return nil, fmt.Errorf("element %d: URL must be at most %d characters", i, maxURLRunes)
		}
		cl, err := classifyPersonLink(s)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		key := cl.SourceKey + "\x00" + cl.ExternalID
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("element %d: duplicate URL", i)
		}
		seen[key] = struct{}{}
		out = append(out, cl)
	}
	return out, nil
}

func validatePersonLinks(v any) error {
	_, err := parsePersonLinks(v)
	return err
}

func applyPersonLinks(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	links, err := parsePersonLinks(value)
	if err != nil {
		return fmt.Errorf("editspec: person links: %w", err)
	}
	if err := assertPersonExists(ctx, tx, entityID); err != nil {
		return err
	}
	sources, err := sourceIDsByKey(ctx, tx)
	if err != nil {
		return err
	}
	rows := make([]catmodel.CatalogExternalRef, 0, len(links))
	pairs := make([][]any, 0, len(links))
	for _, cl := range links {
		srcID, ok := sources[cl.SourceKey]
		if !ok {
			return &editing.ValidationError{Key: FieldPersonLinks, Reason: fmt.Sprintf("source %q is not registered", cl.SourceKey)}
		}
		pairs = append(pairs, []any{srcID, cl.ExternalID})
		rows = append(rows, catmodel.CatalogExternalRef{
			EntityType: catmodel.EntityTypePerson, EntityID: entityID,
			SourceID: srcID, ExternalID: cl.ExternalID,
			LinkKind: cl.LinkKind, MatchedBy: curatedMatchedBy,
		})
	}
	if len(pairs) > 0 {
		var clash []struct {
			SourceKey  string `gorm:"column:key"`
			ExternalID string `gorm:"column:external_id"`
		}
		if err := tx.WithContext(ctx).Raw(`
			SELECT s.key, r.external_id
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			WHERE r.entity_type = ? AND r.entity_id = ? AND r.matched_by <> ?
			  AND (r.source_id, r.external_id) IN ?
			ORDER BY r.source_id, r.external_id`,
			catmodel.EntityTypePerson, entityID, curatedMatchedBy, pairs).Scan(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{
				Key: FieldPersonLinks,
				Reason: fmt.Sprintf("this URL is already linked upstream (%s:%s); to hide an upstream link use %s",
					clash[0].SourceKey, clash[0].ExternalID, FieldPersonLinksSuppr),
			}
		}
	}
	if err := tx.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND matched_by = ?", catmodel.EntityTypePerson, entityID, curatedMatchedBy).
		Delete(&catmodel.CatalogExternalRef{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func loadCuratedPersonLinks(ctx context.Context, db *gorm.DB, personID int64) ([]any, error) {
	var rows []struct {
		SourceKey  string `gorm:"column:key"`
		ExternalID string `gorm:"column:external_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT s.key, r.external_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.matched_by = ?
		ORDER BY r.source_id, r.external_id`,
		catmodel.EntityTypePerson, personID, curatedMatchedBy).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, canonicalPersonLinkURL(classifiedLink{SourceKey: r.SourceKey, ExternalID: r.ExternalID, LinkKind: r.LinkKind}))
	}
	return out, nil
}

// PersonLinkIdentity is the frozen row identity of catalog.person.links. The
// source key is a frozen catalog_source token and the external id is the
// unescaped remainder (a web link's id is its whole URL, colons included).
func PersonLinkIdentity(sourceKey, externalID string) string {
	return "link:" + sourceKey + ":" + externalID
}

func personLinkKeyCheck(key string) error {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[0] != "link" {
		return fmt.Errorf("%q is not an identity key of this field: expected link:<source>:<external_id>", key)
	}
	if !reLinkSourceKey.MatchString(parts[1]) {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a source key", key, parts[1])
	}
	if parts[2] == "" || strings.TrimSpace(parts[2]) != parts[2] {
		return fmt.Errorf("%q is not an identity key of this field: the external id is empty or carries edge whitespace", key)
	}
	return nil
}

// NotSuppressedPersonLinkSQL is the read-path predicate, correlated on a
// catalog_external_ref alias and its catalog_source alias. Only the related
// (non-identity) rows can carry a suppression: identity anchors are what the
// importers match on, and hiding one on the page would leave the mapping it
// drives in place — that is the split/detach tools' job.
func NotSuppressedPersonLinkSQL(refAlias, sourceAlias string) string {
	return fmt.Sprintf(
		`(%[1]s.link_kind <> %[4]d OR NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%[2]s' AND esr.field_key = '%[3]s'
			  AND esr.entity_id = %[1]s.entity_id
			  AND esr.identity_key = 'link:' || %[5]s.key || ':' || %[1]s.external_id))`,
		refAlias, TypePerson, FieldPersonLinks, catmodel.LinkKindRelated, sourceAlias)
}

func loadSuppressedPersonLinks(ctx context.Context, db *gorm.DB, personID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypePerson, personID, FieldPersonLinks)
}
