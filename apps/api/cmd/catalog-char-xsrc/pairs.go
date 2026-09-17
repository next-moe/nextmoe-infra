package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

type pairMeta struct {
	A         int64    `json:"a"`
	B         int64    `json:"b"`
	Tier      int      `json:"tier"`
	Works     []int64  `json:"works"`
	CV        string   `json:"cv,omitempty"`
	AName     string   `json:"a_name"`
	BName     string   `json:"b_name"`
	ASources  []string `json:"a_sources"`
	BSources  []string `json:"b_sources"`
	Instance  bool     `json:"instance,omitempty"`
	Qualified bool     `json:"qualified,omitempty"`
	ARich     richness `json:"a_rich"`
	BRich     richness `json:"b_rich"`
}

type richness struct {
	Img      bool `json:"img,omitempty"`
	NAliases int  `json:"na,omitempty"`
}

type charInfo struct {
	ID       int64
	Name     string
	Lang     string
	Gender   *int16
	Instance bool
	HasImage bool
	Aliases  []string
	Anchors  []string
	Sources  []string
	Intro    string
	NAnchors int
}

func buildPairs(db *gorm.DB) ([]pairMeta, map[int64]*charInfo, error) {
	type rawPair struct {
		A     int64  `gorm:"column:a"`
		B     int64  `gorm:"column:b"`
		Works string `gorm:"column:works"`
	}
	var raws []rawPair
	if err := db.Raw(`
		WITH anchored AS (
			SELECT er.entity_id AS id, array_agg(DISTINCT er.source_id) AS sids
			FROM catalog_external_ref er
			JOIN catalog_character c ON c.id = er.entity_id AND c.deleted_at IS NULL
			WHERE er.entity_type = 4
			GROUP BY er.entity_id
		)
		SELECT wa.character_id AS a, wb.character_id AS b,
		       string_agg(DISTINCT wa.work_id::text, ',') AS works
		FROM catalog_work_character wa
		JOIN catalog_work_character wb
		  ON wb.work_id = wa.work_id AND wb.character_id > wa.character_id
		JOIN anchored sa ON sa.id = wa.character_id
		JOIN anchored sb ON sb.id = wb.character_id AND NOT (sa.sids && sb.sids)
		WHERE ` + editspec.NotSuppressedRosterSQL("wa") + `
		  AND ` + editspec.NotSuppressedRosterSQL("wb") + `
		GROUP BY 1, 2`).Scan(&raws).Error; err != nil {
		return nil, nil, fmt.Errorf("co-resident pairs: %w", err)
	}

	ids := map[int64]bool{}
	for _, r := range raws {
		ids[r.A], ids[r.B] = true, true
	}
	info, err := loadCharInfo(db, ids)
	if err != nil {
		return nil, nil, err
	}
	bridge, err := loadVABridges(db)
	if err != nil {
		return nil, nil, err
	}

	var pairs []pairMeta
	for _, r := range raws {
		a, b := info[r.A], info[r.B]
		if a == nil || b == nil {
			continue
		}
		aNames := append([]string{a.Name}, a.Aliases...)
		bNames := append([]string{b.Name}, b.Aliases...)
		cv := bridge[bridgeKey(r.A, r.B)]
		tier := 0
		switch {
		case foldedEqual(aNames, bNames):
			tier = 1
		case cv != "":
			tier = 2
		case namesSimilar(aNames, bNames):
			tier = 3
		default:
			continue
		}
		var works []int64
		for _, w := range strings.Split(r.Works, ",") {
			if id, err := strconv.ParseInt(w, 10, 64); err == nil {
				works = append(works, id)
			}
		}
		sort.Slice(works, func(i, j int) bool { return works[i] < works[j] })
		pairs = append(pairs, pairMeta{
			A: r.A, B: r.B, Tier: tier, Works: works, CV: cv,
			AName: a.Name, BName: b.Name, ASources: a.Sources, BSources: b.Sources,
			Instance:  a.Instance || b.Instance,
			Qualified: qualifiedPair(a.Name, b.Name),
			ARich:     richness{Img: a.HasImage, NAliases: len(a.Aliases)},
			BRich:     richness{Img: b.HasImage, NAliases: len(b.Aliases)},
		})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].A != pairs[j].A {
			return pairs[i].A < pairs[j].A
		}
		return pairs[i].B < pairs[j].B
	})
	return pairs, info, nil
}

func foldedEqual(aNames, bNames []string) bool {
	for _, an := range aNames {
		fa := foldName(an)
		if fa == "" {
			continue
		}
		for _, bn := range bNames {
			if fa == foldName(bn) {
				return true
			}
		}
	}
	return false
}

func bridgeKey(a, b int64) string { return strconv.FormatInt(a, 10) + "|" + strconv.FormatInt(b, 10) }

func loadVABridges(db *gorm.DB) (map[string]string, error) {
	type row struct {
		A  int64  `gorm:"column:a"`
		B  int64  `gorm:"column:b"`
		CV string `gorm:"column:cv"`
	}
	var rows []row
	if err := db.Raw(`
		SELECT ka.character_id AS a, kb.character_id AS b, min(cn.name) AS cv
		FROM catalog_credit ka
		JOIN catalog_credit kb
		  ON kb.work_id = ka.work_id AND kb.credit_name_id = ka.credit_name_id
		 AND kb.role_id = 1 AND kb.character_id > ka.character_id
		JOIN catalog_credit_name cn ON cn.id = ka.credit_name_id
		WHERE ka.role_id = 1 AND ka.character_id IS NOT NULL AND kb.character_id IS NOT NULL
		  AND ` + editspec.NotSuppressedCreditSQL("ka") + `
		  AND ` + editspec.NotSuppressedCreditSQL("kb") + `
		GROUP BY 1, 2`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("va bridges: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[bridgeKey(r.A, r.B)] = r.CV
	}
	return out, nil
}

func loadCharInfo(db *gorm.DB, ids map[int64]bool) (map[int64]*charInfo, error) {
	all := make([]int64, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	out := make(map[int64]*charInfo, len(all))

	for _, chunk := range chunks(all, 10000) {
		type crow struct {
			ID       int64
			Name     string `gorm:"column:display_name"`
			Lang     string
			Gender   *int16
			Instance *int64  `gorm:"column:instance_of"`
			Image    *string `gorm:"column:image_hash"`
		}
		var crows []crow
		if err := db.Raw(`SELECT id, display_name, lang, gender, instance_of, image_hash
			FROM catalog_character WHERE deleted_at IS NULL AND id IN ?`, chunk).Scan(&crows).Error; err != nil {
			return nil, fmt.Errorf("characters: %w", err)
		}
		for _, c := range crows {
			out[c.ID] = &charInfo{
				ID: c.ID, Name: c.Name, Lang: c.Lang, Gender: c.Gender,
				Instance: c.Instance != nil,
				HasImage: c.Image != nil && *c.Image != "",
			}
		}

		type arow struct {
			CharacterID int64
			Name        string
		}
		var arows []arow
		if err := db.Raw(`SELECT character_id, name FROM catalog_character_alias
			WHERE character_id IN ? ORDER BY character_id, id`, chunk).Scan(&arows).Error; err != nil {
			return nil, fmt.Errorf("aliases: %w", err)
		}
		for _, a := range arows {
			if c := out[a.CharacterID]; c != nil {
				c.Aliases = append(c.Aliases, a.Name)
			}
		}

		type erow struct {
			EntityID   int64
			Key        string
			ExternalID string
		}
		var erows []erow
		if err := db.Raw(`SELECT er.entity_id, s.key, er.external_id
			FROM catalog_external_ref er JOIN catalog_source s ON s.id = er.source_id
			WHERE er.entity_type = 4 AND er.entity_id IN ?
			ORDER BY er.entity_id, er.source_id, er.external_id`, chunk).Scan(&erows).Error; err != nil {
			return nil, fmt.Errorf("anchors: %w", err)
		}
		for _, e := range erows {
			if c := out[e.EntityID]; c != nil {
				c.Anchors = append(c.Anchors, e.Key+":"+e.ExternalID)
				if len(c.Sources) == 0 || c.Sources[len(c.Sources)-1] != e.Key {
					c.Sources = append(c.Sources, e.Key)
				}
				c.NAnchors++
			}
		}

		type irow struct {
			CharacterID int64
			Intro       string
		}
		var irows []irow
		if err := db.Raw(`SELECT DISTINCT ON (character_id) character_id, intro
			FROM catalog_character_intro WHERE character_id IN ?
			ORDER BY character_id, provenance,
			         CASE lang WHEN 'ja' THEN 0 WHEN 'zh-Hans' THEN 1 WHEN 'en' THEN 2 ELSE 3 END`,
			chunk).Scan(&irows).Error; err != nil {
			return nil, fmt.Errorf("intros: %w", err)
		}
		for _, i := range irows {
			if c := out[i.CharacterID]; c != nil {
				c.Intro = truncateRunes(i.Intro, 500)
			}
		}
	}
	return out, nil
}

func chunks(ids []int64, n int) [][]int64 {
	var out [][]int64
	for len(ids) > n {
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
