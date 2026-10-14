package seed

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"api/internal/platform/catalog/bangumicommon"

	"gopkg.in/yaml.v3"
)

type RoleSeed struct {
	ID         int64  `yaml:"id"`
	Key        string `yaml:"key"`
	Category   string `yaml:"category"`
	NameEN     string `yaml:"name_en"`
	NameCN     string `yaml:"name_cn"`
	NameJA     string `yaml:"name_ja"`
	Deprecated bool   `yaml:"deprecated,omitempty"`
}

type RoleMapSeed struct {
	SourceRole string `yaml:"source_role"`
	RoleID     int64  `yaml:"role_id"`
	Note       string `yaml:"note"`
}

type GeneratedRoles struct {
	Roles            []RoleSeed
	Mappings         []RoleMapSeed
	GameRoleToShared map[int64]int64
}

const roleIDBase = 100

// Bangumi's game positions name-match roles of their own, so a creator VNDB,
// ErogameScape and DLsite credit as scenario was credited again as 剧本 on the
// same work: 7,545 scenario × 剧本 pairs and 2,353 music × 音乐 pairs in
// production on 2026-10-13. These positions take the role the other sources
// share; the role each would have generated is left with no mapping and retired.
var bangumiSharedRoles = map[string]int64{
	"4:1004": roleScenario,
	"4:1006": roleMusic,
	"4:1013": roleIllustration,
	"4:1016": roleDirector,
	"4:1018": roleExecutiveProducer,
}

func GenerateBangumiRoles() (GeneratedRoles, error) {
	staffs, err := bangumicommon.LoadStaffPositions()
	if err != nil {
		return GeneratedRoles{}, err
	}

	type nameKey struct{ en, cn string }
	type occurrence struct {
		subjectType, position int
		pos                   bangumicommon.StaffPosition
	}
	type roleGroup struct {
		key         nameKey
		occurrences []occurrence
	}

	groups := make(map[nameKey]*roleGroup)
	var groupOrder []*roleGroup
	var walk []occurrence
	for _, st := range sortedKeys(staffs) {
		for _, pid := range sortedKeys(staffs[st]) {
			occ := occurrence{subjectType: st, position: pid, pos: staffs[st][pid]}
			walk = append(walk, occ)
			k := nameKey{en: normalizeName(occ.pos.EN), cn: normalizeName(occ.pos.CN)}
			g, ok := groups[k]
			if !ok {
				g = &roleGroup{key: k}
				groups[k] = g
				groupOrder = append(groupOrder, g)
			}
			g.occurrences = append(g.occurrences, occ)
		}
	}

	firstNonEmpty := func(g *roleGroup, get func(bangumicommon.StaffPosition) string) string {
		for _, occ := range g.occurrences {
			if v := strings.TrimSpace(get(occ.pos)); v != "" {
				return v
			}
		}
		return ""
	}
	roleByGroup := make(map[*roleGroup]*RoleSeed)
	var roles []*RoleSeed
	for _, g := range groupOrder {
		r := &RoleSeed{
			NameEN: firstNonEmpty(g, func(p bangumicommon.StaffPosition) string { return p.EN }),
			NameCN: firstNonEmpty(g, func(p bangumicommon.StaffPosition) string { return p.CN }),
			NameJA: firstNonEmpty(g, func(p bangumicommon.StaffPosition) string { return p.JP }),
			Category: firstNonEmpty(g, func(p bangumicommon.StaffPosition) string {
				if len(p.Categories) == 0 {
					return ""
				}
				return p.Categories[0].EN
			}),
		}
		roleByGroup[g] = r
		roles = append(roles, r)
	}

	bySlug := make(map[string][]*roleGroup)
	for _, g := range groupOrder {
		src := g.key.en
		if src == "" {
			src = g.key.cn
		}
		s := slugify(src)
		if s == "" {
			return GeneratedRoles{}, fmt.Errorf("catalog seed: empty slug for role group en=%q cn=%q", g.key.en, g.key.cn)
		}
		bySlug[s] = append(bySlug[s], g)
	}
	for s, gs := range bySlug {
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].key.en != gs[j].key.en {
				return gs[i].key.en < gs[j].key.en
			}
			return gs[i].key.cn < gs[j].key.cn
		})
		for i, g := range gs {
			if i == 0 {
				roleByGroup[g].Key = s
			} else {
				roleByGroup[g].Key = fmt.Sprintf("%s-%d", s, i+1)
			}
		}
	}
	seen := make(map[string]bool, len(roles))
	for _, r := range roles {
		if seen[r.Key] {
			return GeneratedRoles{}, fmt.Errorf("catalog seed: duplicate role key %q after disambiguation", r.Key)
		}
		seen[r.Key] = true
	}

	sort.Slice(roles, func(i, j int) bool { return roles[i].Key < roles[j].Key })
	for i, r := range roles {
		r.ID = roleIDBase + int64(i)
	}

	mappings := make([]RoleMapSeed, 0, len(walk))
	targeted := make(map[int64]bool, len(roles))
	gameRoleToShared := make(map[int64]int64, len(bangumiSharedRoles))
	for _, occ := range walk {
		k := nameKey{en: normalizeName(occ.pos.EN), cn: normalizeName(occ.pos.CN)}
		sourceRole := fmt.Sprintf("%d:%d", occ.subjectType, occ.position)
		roleID := roleByGroup[groups[k]].ID
		if shared, ok := bangumiSharedRoles[sourceRole]; ok {
			gameRoleToShared[roleID] = shared
			roleID = shared
		}
		targeted[roleID] = true
		mappings = append(mappings, RoleMapSeed{SourceRole: sourceRole, RoleID: roleID, Note: occ.pos.CN})
	}
	for _, r := range roles {
		r.Deprecated = !targeted[r.ID]
	}

	out := GeneratedRoles{Roles: make([]RoleSeed, len(roles)), Mappings: mappings, GameRoleToShared: gameRoleToShared}
	for i, r := range roles {
		out.Roles[i] = *r
	}
	return out, nil
}

func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func slugify(s string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		} else {
			pendingHyphen = true
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

const genHeader = `# Code generated by internal/platform/catalog/seed/gen from the embedded
# bangumicommon snapshot. DO NOT EDIT.
# Role IDs are frozen once generated: refresh = re-run the generator, review
# the diff, keep changes additive (see seed/rolegen.go).
`

func RenderRolesYAML(roles []RoleSeed) ([]byte, error) {
	return renderYAML(struct {
		Roles []RoleSeed `yaml:"roles"`
	}{roles})
}

func RenderRoleMapYAML(mappings []RoleMapSeed) ([]byte, error) {
	return renderYAML(struct {
		Mappings []RoleMapSeed `yaml:"mappings"`
	}{mappings})
}

func renderYAML(doc any) ([]byte, error) {
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("catalog seed: render yaml: %w", err)
	}
	return append([]byte(genHeader), body...), nil
}
