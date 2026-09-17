package llmsuggest

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

type creditRoleEv struct {
	Role    string `json:"role"`
	Credits int    `json:"credits"`
}

type creditWorkEv struct {
	Title  string `json:"title"`
	Medium string `json:"medium"`
	Year   *int   `json:"year,omitempty"`
	Role   string `json:"role"`
}

type creditSideDossier struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Sources      []string       `json:"sources"`
	Aliases      []string       `json:"aliases"`
	LinkedPerson string         `json:"linked_person,omitempty"`
	Credits      int            `json:"credits"`
	Roles        []creditRoleEv `json:"roles"`
	Works        []creditWorkEv `json:"works"`
	FirstYear    *int           `json:"first_year,omitempty"`
	LastYear     *int           `json:"last_year,omitempty"`
	Labels       []string       `json:"labels"`

	personID   *int64
	aliasFolds map[string]bool
	aliasRaw   []string
	workIDs    map[int64]bool
	labelSet   map[string]bool
	roleKeys   map[string]int
}

type creditPairDossier struct {
	WhyPaired       string            `json:"why_paired"`
	AliasDeclaredBy string            `json:"alias_declared_by"`
	A               creditSideDossier `json:"a"`
	B               creditSideDossier `json:"b"`
	SharedWorks     int               `json:"shared_works"`
	SharedLabels    []string          `json:"shared_labels"`
}

const (
	creditAliasCap = 10
	creditRoleCap  = 5
	creditWorkCap  = 6
	creditLabelCap = 5
)

func loadCreditSides(db *gorm.DB, ids []int64) (map[int64]*creditSideDossier, error) {
	out := map[int64]*creditSideDossier{}
	notSuppressed := editspec.NotSuppressedCreditSQL("c")
	for _, chunk := range chunkBy(ids, 500) {
		if len(chunk) == 0 {
			continue
		}
		var names []struct {
			ID         int64  `gorm:"column:id"`
			Name       string `gorm:"column:name"`
			PersonID   *int64 `gorm:"column:person_id"`
			PersonName string `gorm:"column:person_name"`
		}
		if err := db.Raw(`SELECT n.id, n.name, n.person_id, COALESCE(p.display_name, '') AS person_name
			FROM catalog_credit_name n
			LEFT JOIN catalog_person p ON p.id = n.person_id AND p.deleted_at IS NULL
			WHERE n.id IN ?`, chunk).Scan(&names).Error; err != nil {
			return nil, err
		}
		for _, n := range names {
			out[n.ID] = &creditSideDossier{
				ID: n.ID, Name: n.Name, LinkedPerson: n.PersonName, personID: n.PersonID,
				Sources: []string{}, Aliases: []string{}, Roles: []creditRoleEv{},
				Works: []creditWorkEv{}, Labels: []string{},
				aliasFolds: map[string]bool{}, workIDs: map[int64]bool{},
				labelSet: map[string]bool{}, roleKeys: map[string]int{},
			}
		}

		var sources []struct {
			EntityID int64  `gorm:"column:entity_id"`
			Key      string `gorm:"column:key"`
		}
		if err := db.Raw(`SELECT DISTINCT r.entity_id, s.key
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			WHERE r.entity_type = ? AND r.link_kind = ? AND r.dead_at IS NULL AND r.entity_id IN ?
			ORDER BY 1, 2`, model.EntityTypeCreditName, model.LinkKindExact, chunk).Scan(&sources).Error; err != nil {
			return nil, err
		}
		for _, s := range sources {
			if side := out[s.EntityID]; side != nil {
				side.Sources = append(side.Sources, s.Key)
			}
		}

		var aliases []struct {
			CreditNameID int64  `gorm:"column:credit_name_id"`
			Name         string `gorm:"column:name"`
		}
		if err := db.Raw(`SELECT credit_name_id, name FROM catalog_name_alias
			WHERE credit_name_id IN ? ORDER BY credit_name_id, id`, chunk).Scan(&aliases).Error; err != nil {
			return nil, err
		}
		for _, a := range aliases {
			side := out[a.CreditNameID]
			if side == nil {
				continue
			}
			f := foldCreditName(a.Name)
			if f == "" || side.aliasFolds[f] {
				continue
			}
			side.aliasFolds[f] = true
			side.aliasRaw = append(side.aliasRaw, a.Name)
			if len(side.Aliases) < creditAliasCap {
				side.Aliases = append(side.Aliases, a.Name)
			}
		}

		var credits []struct {
			CreditNameID int64  `gorm:"column:credit_name_id"`
			WorkID       int64  `gorm:"column:work_id"`
			Role         string `gorm:"column:role"`
			Title        string `gorm:"column:title"`
			Medium       string `gorm:"column:medium"`
			Year         *int   `gorm:"column:year"`
		}
		if err := db.Raw(`SELECT c.credit_name_id, c.work_id, r.key AS role, w.display_name AS title, m.key AS medium,
				(SELECT min(rl.released_y) FROM catalog_release rl
				 WHERE rl.work_id = w.id AND rl.deleted_at IS NULL AND rl.released_y IS NOT NULL) AS year
			FROM catalog_credit c
			JOIN catalog_role r ON r.id = c.role_id
			JOIN catalog_work w ON w.id = c.work_id AND w.deleted_at IS NULL
			JOIN catalog_medium m ON m.id = w.medium_id
			WHERE c.credit_name_id IN ? AND `+notSuppressed+`
			ORDER BY c.credit_name_id, c.work_id, c.id`, chunk).Scan(&credits).Error; err != nil {
			return nil, err
		}
		works := map[int64][]creditWorkEv{}
		for _, c := range credits {
			side := out[c.CreditNameID]
			if side == nil {
				continue
			}
			side.Credits++
			side.roleKeys[c.Role]++
			if side.workIDs[c.WorkID] {
				continue
			}
			side.workIDs[c.WorkID] = true
			works[c.CreditNameID] = append(works[c.CreditNameID], creditWorkEv{
				Title: c.Title, Medium: c.Medium, Year: c.Year, Role: c.Role,
			})
		}
		for id, ws := range works {
			side := out[id]
			for _, w := range ws {
				if w.Year == nil {
					continue
				}
				if side.FirstYear == nil || *w.Year < *side.FirstYear {
					side.FirstYear = w.Year
				}
				if side.LastYear == nil || *w.Year > *side.LastYear {
					side.LastYear = w.Year
				}
			}
			side.Works = spanOfWorks(ws, creditWorkCap)
		}

		var labels []struct {
			CreditNameID int64  `gorm:"column:credit_name_id"`
			Name         string `gorm:"column:name"`
			N            int    `gorm:"column:n"`
		}
		if err := db.Raw(`SELECT c.credit_name_id, l.display_name AS name, count(DISTINCT c.work_id) AS n
			FROM catalog_credit c
			JOIN catalog_work w ON w.id = c.work_id AND w.deleted_at IS NULL
			JOIN catalog_work_label wl ON wl.work_id = c.work_id
			JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
			WHERE c.credit_name_id IN ? AND `+notSuppressed+`
			GROUP BY 1, 2 ORDER BY 1, 3 DESC, 2`, chunk).Scan(&labels).Error; err != nil {
			return nil, err
		}
		for _, l := range labels {
			side := out[l.CreditNameID]
			if side == nil {
				continue
			}
			side.labelSet[l.Name] = true
			if len(side.Labels) < creditLabelCap {
				side.Labels = append(side.Labels, l.Name)
			}
		}
	}
	for _, side := range out {
		for role, n := range side.roleKeys {
			side.Roles = append(side.Roles, creditRoleEv{Role: role, Credits: n})
		}
		sort.Slice(side.Roles, func(i, j int) bool {
			if side.Roles[i].Credits != side.Roles[j].Credits {
				return side.Roles[i].Credits > side.Roles[j].Credits
			}
			return side.Roles[i].Role < side.Roles[j].Role
		})
		side.Roles = capN(side.Roles, creditRoleCap)
	}
	return out, nil
}

func spanOfWorks(ws []creditWorkEv, n int) []creditWorkEv {
	sorted := append([]creditWorkEv(nil), ws...)
	sort.SliceStable(sorted, func(i, j int) bool {
		yi, yj := sorted[i].Year, sorted[j].Year
		switch {
		case yi == nil && yj == nil:
			return sorted[i].Title < sorted[j].Title
		case yi == nil:
			return false
		case yj == nil:
			return true
		case *yi != *yj:
			return *yi < *yj
		default:
			return sorted[i].Title < sorted[j].Title
		}
	})
	if len(sorted) <= n {
		return sorted
	}
	dated := 0
	for _, w := range sorted {
		if w.Year != nil {
			dated++
		}
	}
	if dated < n {
		return sorted[:n]
	}
	head := n / 2
	return append(append([]creditWorkEv(nil), sorted[:head]...), sorted[dated-(n-head):dated]...)
}

func buildCreditPair(reason int16, a, b *creditSideDossier) creditPairDossier {
	d := creditPairDossier{A: *a, B: *b, SharedLabels: []string{}}
	switch reason {
	case model.CandidateReasonAliasDeclared:
		d.WhyPaired = "alias_declared"
	case model.CandidateReasonSharedExternalID:
		d.WhyPaired = "shared_handle"
	default:
		d.WhyPaired = "other"
	}
	aDecl := a.aliasFolds[foldCreditName(b.Name)]
	bDecl := b.aliasFolds[foldCreditName(a.Name)]
	switch {
	case aDecl && bDecl:
		d.AliasDeclaredBy = "both"
	case aDecl:
		d.AliasDeclaredBy = "a"
	case bDecl:
		d.AliasDeclaredBy = "b"
	default:
		d.AliasDeclaredBy = "neither"
	}
	for id := range a.workIDs {
		if b.workIDs[id] {
			d.SharedWorks++
		}
	}
	for l := range a.labelSet {
		if b.labelSet[l] {
			d.SharedLabels = append(d.SharedLabels, l)
		}
	}
	sort.Strings(d.SharedLabels)
	d.SharedLabels = capN(d.SharedLabels, 8)
	return d
}

const (
	guardBothLinked = "both-linked"
	guardShortName  = "short-name"
	guardCompany    = "company"
)

var companyRoleKeys = map[string]bool{
	"developer": true, "publisher": true, "brand": true, "label": true, "label-2": true,
	"出品方": true, "出版社": true, "连载杂志": true, "recording-studio": true,
}

var companyMarkers = []string{
	"株式会社", "有限会社", "合同会社", "(株)", "(有)",
	"co.,ltd", "co., ltd", "inc.", " inc", "llc", "corporation",
}

// creditNameGuard names the reason a pair never reaches the model. An accept
// here creates or joins a person, and a person is not unmerged in production.
func creditNameGuard(d creditPairDossier) string {
	switch {
	case d.A.personID != nil && d.B.personID != nil:
		return guardBothLinked
	case shortCreditName(d.A.Name) || shortCreditName(d.B.Name):
		return guardShortName
	case companySide(d.A) || companySide(d.B):
		return guardCompany
	}
	return ""
}

func shortCreditName(name string) bool {
	f := foldCreditName(name)
	n := utf8.RuneCountInString(f)
	if n <= 2 {
		return true
	}
	if n > 4 {
		return false
	}
	for _, r := range f {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func companySide(s creditSideDossier) bool {
	for _, n := range append([]string{s.Name}, s.aliasRaw...) {
		low := strings.ToLower(norm.NFKC.String(n))
		for _, m := range companyMarkers {
			if strings.Contains(low, m) {
				return true
			}
		}
	}
	if s.Credits == 0 {
		return false
	}
	for role := range s.roleKeys {
		if !companyRoleKeys[role] {
			return false
		}
	}
	return true
}

func foldCreditName(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune("・·=._-/", r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
