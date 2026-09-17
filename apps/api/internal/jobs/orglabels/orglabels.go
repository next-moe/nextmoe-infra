package orglabels

import (
	"fmt"
	"regexp"
	"time"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const (
	sourceVNDB    int16 = 2
	sourceBangumi int16 = 3
	sourceDlsite  int16 = 4
	sourceEG      int16 = 5

	sourceSteam        int16 = 8
	sourceOfficialSite int16 = 9
	sourceTwitter      int16 = 10
	sourcePixiv        int16 = 11
	sourceCien         int16 = 14
	sourceDmm          int16 = 15
)

const (
	ruleVNDBCoworks    = "rule:vndb-org-coworks"
	ruleVNDBCoworkName = "rule:vndb-org-cowork-name"
	ruleVNDBNameOnly   = "rule:vndb-org-name-only"
	ruleVNDBNameLoose  = "rule:vndb-org-name-loose"
	ruleVNDBNew        = "rule:vndb-org-new"

	ruleBGMCoworks    = "rule:bangumi-org-coworks"
	ruleBGMCoworkName = "rule:bangumi-org-cowork-name"
	ruleBGMNameOnly   = "rule:bangumi-org-name-only"
	ruleBGMNameLoose  = "rule:bangumi-org-name-loose"
	ruleBGMNew        = "rule:bangumi-org-new"

	ruleEGCoworks    = "rule:eg-org-coworks"
	ruleEGCoworkName = "rule:eg-org-cowork-name"
	ruleEGNameOnly   = "rule:eg-org-name-only"
	ruleEGNameLoose  = "rule:eg-org-name-loose"
	ruleEGNew        = "rule:eg-org-new"
)

type ruleSet struct {
	coworks, coworkName, nameOnly, nameLoose, newLabel string
}

func srcKey(source int16) string {
	switch source {
	case sourceVNDB:
		return "vndb"
	case sourceBangumi:
		return "bangumi"
	case sourceEG:
		return "erogamescape"
	default:
		return fmt.Sprintf("source-%d", source)
	}
}

type Opts struct {
	Apply     bool
	DSN       string
	EGDSN     string
	DlsiteDSN string
	Source    string
	Facet     string
	Limit     int
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

var reDBName = regexp.MustCompile(`dbname=\S+`)

func deriveEGDSN(catalogDSN string) (string, error) {
	if !reDBName.MatchString(catalogDSN) {
		return "", fmt.Errorf("cannot derive eg dsn: no dbname= in catalog dsn")
	}
	return reDBName.ReplaceAllString(catalogDSN, "dbname=erogamescape"), nil
}

func openPools(opts Opts, needEG bool) (catalog, eg *gorm.DB, err error) {
	if opts.DSN == "" {
		return nil, nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess the target database")
	}
	catalog, err = openGorm(opts.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open catalog pool: %w", err)
	}
	if needEG {
		egDSN := opts.EGDSN
		if egDSN == "" {
			if egDSN, err = deriveEGDSN(opts.DSN); err != nil {
				return nil, nil, err
			}
		}
		eg, err = openGorm(egDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open erogamescape pool: %w", err)
		}
	}
	return catalog, eg, nil
}

func resolveSourceID(db *gorm.DB, key string) (int16, error) {
	var id int16
	if err := db.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("resolve source %q: %w", key, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("source %q not seeded — run migrate-catalog first", key)
	}
	return id, nil
}

var nowUTC = func() time.Time { return time.Now().UTC() }
