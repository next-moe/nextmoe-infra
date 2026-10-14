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
	reVNDB    = regexp.MustCompile(`(?i)^https?://(?:www\.)?vndb\.org/(v\d+)(?:[/?#]|$)`)
	reBangumi = regexp.MustCompile(`(?i)^https?://(?:www\.)?(?:bgm\.tv|bangumi\.tv|chii\.in)/subject/(\d+)`)

	reLinkArchive     = regexp.MustCompile(`(?i)web\.archive\.org/web/[^/]+/(https?://.+)$`)
	reLinkTwitter     = regexp.MustCompile(`(?i)^https?://(?:www\.|mobile\.)?(?:twitter\.com|x\.com)/@?([A-Za-z0-9_]{1,30})(?:[/?#]|$)`)
	reLinkCien        = regexp.MustCompile(`(?i)^https?://ci-en\.dlsite\.com/creator/(\d+)`)
	reLinkSteam       = regexp.MustCompile(`(?i)^https?://store\.steampowered\.com/app/(\d+)`)
	reLinkPixivUsers  = regexp.MustCompile(`(?i)^https?://(?:www\.)?pixiv\.net/(?:en/)?users/(\d+)`)
	reLinkPixivMember = regexp.MustCompile(`(?i)^https?://(?:www\.)?pixiv\.net/member\.php\?id=(\d+)`)
	reLinkDlsiteHost  = regexp.MustCompile(`(?i)^https?://(?:www\.)?dlsite\.com/`)
	reLinkDlsiteWork  = regexp.MustCompile(`(?i)\b((?:RJ|VJ|BJ)\d{5,10})\b`)
)

var linkURLTemplates = map[string]string{
	"vndb":    "https://vndb.org/%s",
	"bangumi": "https://bgm.tv/subject/%s",
	"twitter": "https://x.com/%s",
	"cien":    "https://ci-en.dlsite.com/creator/%s",
	"steam":   "https://store.steampowered.com/app/%s",
	"pixiv":   "https://www.pixiv.net/users/%s",
	"dlsite":  "https://www.dlsite.com/home/work/=/product_id/%s.html",
}

var linkReservedHandles = map[string]bool{
	"i": true, "home": true, "intent": true, "share": true,
	"search": true, "hashtag": true, "explore": true, "messages": true,
}

type classifiedLink struct {
	SourceKey  string
	ExternalID string
	LinkKind   int16
}

func classifyWorkLink(raw string) (classifiedLink, bool) {
	u := strings.TrimSpace(raw)
	if m := reLinkArchive.FindStringSubmatch(u); m != nil {
		u = strings.TrimSpace(m[1])
	}
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return classifiedLink{}, false
	}
	if m := reVNDB.FindStringSubmatch(u); m != nil {
		return classifiedLink{"vndb", strings.ToLower(m[1]), catmodel.LinkKindProbable}, true
	}
	if m := reBangumi.FindStringSubmatch(u); m != nil {
		return classifiedLink{"bangumi", m[1], catmodel.LinkKindProbable}, true
	}
	related := func(source, id string) (classifiedLink, bool) {
		return classifiedLink{source, id, catmodel.LinkKindRelated}, true
	}
	if m := reLinkTwitter.FindStringSubmatch(u); m != nil {
		if handle := strings.ToLower(m[1]); !linkReservedHandles[handle] {
			return related("twitter", handle)
		}
	}
	if m := reLinkCien.FindStringSubmatch(u); m != nil {
		return related("cien", m[1])
	}
	if m := reLinkSteam.FindStringSubmatch(u); m != nil {
		return related("steam", m[1])
	}
	if m := reLinkPixivUsers.FindStringSubmatch(u); m != nil {
		return related("pixiv", m[1])
	}
	if m := reLinkPixivMember.FindStringSubmatch(u); m != nil {
		return related("pixiv", m[1])
	}
	if reLinkDlsiteHost.MatchString(u) {
		if m := reLinkDlsiteWork.FindStringSubmatch(u); m != nil {
			return related("dlsite", strings.ToUpper(m[1]))
		}
		return related("web", u)
	}
	return related("web", u)
}

func canonicalLinkURL(cl classifiedLink) string {
	tpl, ok := linkURLTemplates[cl.SourceKey]
	if !ok {
		return cl.ExternalID
	}
	return fmt.Sprintf(tpl, cl.ExternalID)
}

func WorkLinkURL(source, externalID string) (string, bool) {
	src := strings.ToLower(strings.TrimSpace(source))
	id := strings.TrimSpace(externalID)
	if src == "" || id == "" {
		return "", false
	}
	tpl, ok := linkURLTemplates[src]
	if !ok {
		return "", false
	}
	return fmt.Sprintf(tpl, id), true
}

func parseLinks(v any) ([]string, error) {
	arr, err := asArray(v, "URL strings")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		s, ok := el.(string)
		if !ok {
			return nil, fmt.Errorf("element %d: must be a URL string", i)
		}
		s = strings.TrimSpace(s)
		if len([]rune(s)) > maxURLRunes {
			return nil, fmt.Errorf("element %d: URL must be at most %d characters", i, maxURLRunes)
		}
		cl, ok := classifyWorkLink(s)
		if !ok {
			return nil, fmt.Errorf("element %d: must be an http:// or https:// URL", i)
		}
		canonical := canonicalLinkURL(cl)
		if _, dup := seen[canonical]; dup {
			return nil, fmt.Errorf("element %d: duplicate URL", i)
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

func validateLinks(v any) error { _, err := parseLinks(v); return err }

func applyLinks(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	return applyLinksFor(ctx, tx, catmodel.EntityTypeWork, entityID, value)
}

func applyLinksFor(ctx context.Context, tx *gorm.DB, entityType int16, entityID int64, value any) error {
	urls, err := parseLinks(value)
	if err != nil {
		return fmt.Errorf("editspec: links: %w", err)
	}
	sources, err := sourceIDsByKey(ctx, tx)
	if err != nil {
		return err
	}
	if err := rejectUpstreamLinkCollisions(ctx, tx, entityType, entityID, urls, sources); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND matched_by = ?",
			entityType, entityID, curatedMatchedBy).
		Delete(&catmodel.CatalogExternalRef{}).Error; err != nil {
		return err
	}
	if len(urls) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogExternalRef, 0, len(urls))
	for _, u := range urls {
		cl, _ := classifyWorkLink(u)
		srcID, ok := sources[cl.SourceKey]
		if !ok {
			return &editing.ValidationError{
				Key:    linksFieldKey(entityType),
				Reason: fmt.Sprintf("source %q is not registered", cl.SourceKey),
			}
		}
		rows = append(rows, catmodel.CatalogExternalRef{
			EntityType: entityType, EntityID: entityID,
			SourceID: srcID, ExternalID: cl.ExternalID,
			LinkKind: cl.LinkKind, MatchedBy: curatedMatchedBy,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func linksFieldKey(entityType int16) string {
	if entityType == catmodel.EntityTypeLabel {
		return FieldLabelLinks
	}
	return FieldWorkLinks
}

// rejectUpstreamLinkCollisions makes an unwinnable insert an explicit 422.
// catalog_external_ref's identity is the (source, external_id) anchor, so a
// curated rematch of an importer-owned URL is ON CONFLICT DO NOTHING.
func rejectUpstreamLinkCollisions(ctx context.Context, tx *gorm.DB, entityType int16, entityID int64, urls []string, sources map[string]int16) error {
	if len(urls) == 0 {
		return nil
	}
	pairs := make([][]any, 0, len(urls))
	for _, u := range urls {
		cl, ok := classifyWorkLink(u)
		if !ok {
			continue
		}
		srcID, ok := sources[cl.SourceKey]
		if !ok {
			continue
		}
		pairs = append(pairs, []any{srcID, cl.ExternalID})
	}
	if len(pairs) == 0 {
		return nil
	}
	var clash []struct {
		SourceKey  string `gorm:"column:key"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := tx.WithContext(ctx).Raw(`
		SELECT s.key, r.external_id
		FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.matched_by <> ?
		  AND (r.source_id, r.external_id) IN ?
		ORDER BY r.source_id, r.external_id`,
		entityType, entityID, curatedMatchedBy, pairs).Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: linksFieldKey(entityType),
		Reason: fmt.Sprintf("this URL is already linked upstream (%s:%s); "+
			"upstream links cannot be re-added through the editor yet",
			clash[0].SourceKey, clash[0].ExternalID),
	}
}

func loadLinks(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	return loadLinksFor(ctx, db, catmodel.EntityTypeWork, workID)
}

func loadLinksFor(ctx context.Context, db *gorm.DB, entityType int16, entityID int64) ([]any, error) {
	var rows []struct {
		SourceKey  string `gorm:"column:key"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT s.key, r.external_id
		FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.matched_by = ?
		ORDER BY r.source_id, r.external_id`,
		entityType, entityID, curatedMatchedBy).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		if r.SourceKey == "web" {
			out = append(out, r.ExternalID)
			continue
		}
		if _, ok := linkURLTemplates[r.SourceKey]; ok {
			out = append(out, canonicalLinkURL(classifiedLink{SourceKey: r.SourceKey, ExternalID: r.ExternalID}))
		}
	}
	return out, nil
}

func sourceIDsByKey(ctx context.Context, tx *gorm.DB) (map[string]int16, error) {
	var rows []catmodel.CatalogSource
	if err := tx.WithContext(ctx).Select("id", "key").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int16, len(rows))
	for _, r := range rows {
		out[r.Key] = r.ID
	}
	return out, nil
}
