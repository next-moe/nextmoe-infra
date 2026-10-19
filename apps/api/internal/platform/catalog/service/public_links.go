package service

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const egsPages = "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/"

var (
	digitsID      = regexp.MustCompile(`^[0-9]+$`)
	vndbWorkID    = regexp.MustCompile(`^v[0-9]+$`)
	vndbCharID    = regexp.MustCompile(`^c[0-9]+$`)
	vndbProducer  = regexp.MustCompile(`^p[0-9]+$`)
	vndbStaffID   = regexp.MustCompile(`^s[0-9]+$`)
	twitterHandle = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
	bareHost      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+(:[0-9]+)?(/\S*)?$`)
)

type pageTemplate struct {
	shape *regexp.Regexp
	base  string
}

// dlsite and dmm are absent from every table: their storefront URLs depend on
// a site section (maniax/home/pro/soft, digital/dlsoft) the stored code does
// not carry, so any template would 404 for part of the population.
var personPages = map[string]pageTemplate{
	"vndb":         {vndbStaffID, "https://vndb.org/"},
	"bangumi":      {digitsID, "https://bgm.tv/person/"},
	"erogamescape": {digitsID, egsPages + "creater.php?creater="},
}

var identityPages = map[int16]map[string]pageTemplate{
	model.EntityTypeWork: {
		"vndb":          {vndbWorkID, "https://vndb.org/"},
		"bangumi":       {digitsID, "https://bgm.tv/subject/"},
		"erogamescape":  {digitsID, egsPages + "game.php?game="},
		"howlongtobeat": {digitsID, "https://howlongtobeat.com/game/"},
		"steam":         {digitsID, "https://store.steampowered.com/app/"},
	},
	model.EntityTypeCharacter: {
		"vndb":         {vndbCharID, "https://vndb.org/"},
		"bangumi":      {digitsID, "https://bgm.tv/character/"},
		"erogamescape": {digitsID, egsPages + "character.php?character="},
	},
	model.EntityTypeLabel: {
		"vndb":         {vndbProducer, "https://vndb.org/"},
		"bangumi":      {digitsID, "https://bgm.tv/person/"},
		"erogamescape": {digitsID, egsPages + "brand.php?brand="},
	},
	model.EntityTypePerson:     personPages,
	model.EntityTypeCreditName: personPages,
}

type linkAnchor struct {
	EntityID   int64  `gorm:"column:entity_id"`
	Source     string `gorm:"column:source"`
	ExternalID string `gorm:"column:external_id"`
	LinkKind   int16  `gorm:"column:link_kind"`
}

func mintLink(entityType int16, a linkAnchor) (dto.PublicLink, bool) {
	switch a.LinkKind {
	case model.LinkKindExact:
		t, ok := identityPages[entityType][a.Source]
		if !ok || !t.shape.MatchString(a.ExternalID) {
			return dto.PublicLink{}, false
		}
		return dto.PublicLink{Source: a.Source, URL: t.base + a.ExternalID, Identity: true, PageID: a.ExternalID}, true
	case model.LinkKindRelated:
		if u, ok := relatedLinkURL(a.Source, a.ExternalID); ok {
			return dto.PublicLink{Source: a.Source, URL: u}, true
		}
	}
	return dto.PublicLink{}, false
}

// Company steam rows hold whole store URLs, not app ids; the bare app template
// turned them into https://store.steampowered.com/app/https://… links. Every
// branch therefore checks the shape it expects and skips anything else.
func relatedLinkURL(source, id string) (string, bool) {
	switch source {
	case "web":
		if isHTTPURL(id) {
			return id, true
		}
	case "official_site":
		if isHTTPURL(id) {
			return id, true
		}
		if bareHost.MatchString(id) {
			return "https://" + id, true
		}
	case "twitter":
		if twitterHandle.MatchString(id) {
			return "https://x.com/" + id, true
		}
	case "cien":
		if digitsID.MatchString(id) {
			return "https://ci-en.dlsite.com/creator/" + id, true
		}
	case "steam":
		if digitsID.MatchString(id) {
			return "https://store.steampowered.com/app/" + id, true
		}
		return urlOnHost(id, "store.steampowered.com")
	case "pixiv":
		if digitsID.MatchString(id) {
			return "https://www.pixiv.net/users/" + id, true
		}
		return urlOnHost(id, "pixiv.net")
	}
	return "", false
}

func isHTTPURL(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func urlOnHost(s, domain string) (string, bool) {
	if !isHTTPURL(s) {
		return "", false
	}
	u, _ := url.Parse(s)
	h := strings.ToLower(u.Hostname())
	if h == domain || strings.HasSuffix(h, "."+domain) {
		return s, true
	}
	return "", false
}

func linkKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == "twitter.com" {
		host = "x.com"
	}
	key := host + strings.TrimSuffix(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key
}

func linkRank(l dto.PublicLink) int {
	switch {
	case l.Identity:
		return 0
	case l.Source == "official_site":
		return 2
	case l.Source == "web":
		return 3
	default:
		return 1
	}
}

// orderLinks puts identity links before related ones and keeps one link per
// address: the most specific of the duplicates, at its own position.
func orderLinks(in []dto.PublicLink) []dto.PublicLink {
	best := make(map[string]int, len(in))
	for i, l := range in {
		k := linkKey(l.URL)
		if j, seen := best[k]; !seen || linkRank(l) < linkRank(in[j]) {
			best[k] = i
		}
	}
	out := make([]dto.PublicLink, 0, len(best))
	for _, identity := range []bool{true, false} {
		for i, l := range in {
			if l.Identity == identity && best[linkKey(l.URL)] == i {
				out = append(out, l)
			}
		}
	}
	return out
}

func mintLinks(entityType int16, anchors []linkAnchor) []dto.PublicLink {
	links := make([]dto.PublicLink, 0, len(anchors))
	for _, a := range anchors {
		if l, ok := mintLink(entityType, a); ok {
			links = append(links, l)
		}
	}
	return orderLinks(links)
}

func (s *PublicService) linkAnchors(ctx context.Context, entityType int16, ids []int64) ([]linkAnchor, error) {
	var rows []linkAnchor
	err := s.db.WithContext(ctx).Raw(`
		SELECT r.entity_id, src.key AS source, r.external_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id IN ? AND r.link_kind IN ? AND r.dead_at IS NULL
		ORDER BY r.entity_id, r.source_id, r.external_id`,
		entityType, ids, []int16{model.LinkKindExact, model.LinkKindRelated}).Scan(&rows).Error
	return rows, err
}

func (s *PublicService) entityLinksFor(ctx context.Context, entityType int16, ids []int64) (map[int64][]dto.PublicLink, error) {
	out := make(map[int64][]dto.PublicLink, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.linkAnchors(ctx, entityType, ids)
	if err != nil {
		return nil, err
	}
	byEntity := make(map[int64][]linkAnchor, len(ids))
	for _, r := range rows {
		byEntity[r.EntityID] = append(byEntity[r.EntityID], r)
	}
	for _, id := range ids {
		out[id] = mintLinks(entityType, byEntity[id])
	}
	return out, nil
}

func (s *PublicService) entityLinks(ctx context.Context, entityType int16, id int64) ([]dto.PublicLink, error) {
	m, err := s.entityLinksFor(ctx, entityType, []int64{id})
	if err != nil {
		return nil, err
	}
	return m[id], nil
}

// creditNameLinks lists the name's own upstream pages, then the linked person's
// for any source the name has none of, then the person's related links.
//
// A credit name's vndb ref is a staff ALIAS id, which has no page; the forum
// built vndb.org/s<alias> from it and linked 60,925 of 60,930 names to someone
// else. The alias resolves to the staff page through src_vndb.staff_alias.
func (s *PublicService) creditNameLinks(ctx context.Context, nameID, personID int64, personHidden bool) ([]dto.PublicLink, error) {
	var own []linkAnchor
	if err := s.db.WithContext(ctx).Raw(`
		SELECT r.entity_id, src.key AS source,
		       CASE WHEN src.key = 'vndb' THEN coalesce(sa.id, '') ELSE r.external_id END AS external_id,
		       r.link_kind
		FROM catalog_external_ref r JOIN catalog_source src ON src.id = r.source_id
		LEFT JOIN LATERAL (
			SELECT a.id FROM src_vndb.staff_alias a
			WHERE src.key = 'vndb'
			  AND a.aid = CASE WHEN r.external_id ~ '^[0-9]{1,18}$' THEN r.external_id::bigint END
		) sa ON true
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		ORDER BY r.source_id, r.external_id`,
		model.EntityTypeCreditName, nameID, model.LinkKindExact).Scan(&own).Error; err != nil {
		return nil, err
	}
	links := mintLinks(model.EntityTypeCreditName, own)
	if personHidden {
		// A vndb staff page lists every alias of the person, so it would publish
		// the very link link_visibility withholds.
		kept := links[:0]
		for _, l := range links {
			if l.Source != "vndb" {
				kept = append(kept, l)
			}
		}
		links = kept
	}
	if personID <= 0 {
		return links, nil
	}
	covered := make(map[string]bool, len(links))
	for _, l := range links {
		covered[l.Source] = true
	}
	person, err := s.entityLinks(ctx, model.EntityTypePerson, personID)
	if err != nil {
		return nil, err
	}
	for _, l := range person {
		if l.Identity && covered[l.Source] {
			continue
		}
		links = append(links, l)
	}
	return orderLinks(links), nil
}
