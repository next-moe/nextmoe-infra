package getchuchars

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type Edition struct {
	GetchuID string
	Ordinal  int
}

type Candidate struct {
	CharacterID int64  `gorm:"column:character_id"`
	WorkID      int64  `gorm:"column:work_id"`
	GetchuID    string `gorm:"column:getchu_id"`
	Ordinal     int    `gorm:"column:ordinal"`
	Name        string `gorm:"column:name"`
	Profile     string `gorm:"column:profile"`
	Attrs       []byte `gorm:"column:attrs"`
	MatchedBy   string `gorm:"column:matched_by"`
	Editions    []Edition
}

// rosterSQL deliberately carries no catalog.work.roster suppression predicate,
// unlike catalog-char-xsrc which reads the same co-residence. The difference is
// what the two conclude from it: char-xsrc concludes "these two catalog
// characters are one person", which is the very claim a suppressed edge denies,
// while this job uses the roster as a COORDINATE ("the Getchu character named X
// on this work is that catalog character"). Hiding a row does not move the
// coordinate, and filtering here would make the matcher miss the character and
// write Getchu's profile onto nobody.
//
// A bundle product (repository.BundleReleaseSQL) lists every bundled game's
// cast, so a common name there would resolve onto the anchored work's
// namesake; it matches nobody.
var rosterSQL = `
SELECT DISTINCT g.external_id AS getchu_id, rel.work_id, wc.character_id,
       lower(normalize(regexp_replace(ch.display_name, '[[:space:]　]', '', 'g'), NFKC)) AS key_name,
       lower(normalize(regexp_replace(coalesce(al.name,''), '[[:space:]　]', '', 'g'), NFKC)) AS key_alias,
       ` + repository.BundleReleaseSQL("rel") + ` AS bundle
FROM catalog_external_ref g
JOIN catalog_release rel ON rel.id = g.entity_id AND rel.deleted_at IS NULL
JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
JOIN catalog_work_character wc ON wc.work_id = rel.work_id
JOIN catalog_character ch ON ch.id = wc.character_id AND ch.deleted_at IS NULL
LEFT JOIN catalog_character_alias al ON al.character_id = ch.id
WHERE g.entity_type = ? AND g.source_id = ? AND g.link_kind = ?`

type rosterRow struct {
	GetchuID    string `gorm:"column:getchu_id"`
	WorkID      int64  `gorm:"column:work_id"`
	CharacterID int64  `gorm:"column:character_id"`
	KeyName     string `gorm:"column:key_name"`
	KeyAlias    string `gorm:"column:key_alias"`
	Bundle      bool   `gorm:"column:bundle"`
}

type getchuChar struct {
	GetchuID string `gorm:"column:getchu_id"`
	Ordinal  int    `gorm:"column:ordinal"`
	Name     string `gorm:"column:name"`
	Reading  string `gorm:"column:reading"`
	CV       string `gorm:"column:cv"`
	Profile  string `gorm:"column:profile"`
	Attrs    []byte `gorm:"column:attrs"`
}

func loadGetchuChars(ctx context.Context, gdb *gorm.DB) ([]getchuChar, error) {
	var out []getchuChar
	err := gdb.WithContext(ctx).Raw(`
		SELECT getchu_id, ordinal, name, coalesce(reading,'') AS reading,
		       coalesce(cv,'') AS cv, coalesce(profile,'') AS profile, attrs
		FROM item_characters
		WHERE btrim(name) <> ''
		ORDER BY getchu_id, ordinal`).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("read staging item_characters: %w", err)
	}
	return out, nil
}

func loadRoster(ctx context.Context, db *gorm.DB, getchuSource int16) ([]rosterRow, error) {
	var out []rosterRow
	err := db.WithContext(ctx).Raw(rosterSQL, entityTypeRelease, getchuSource, linkKindExact).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("load roster: %w", err)
	}
	return out, nil
}
