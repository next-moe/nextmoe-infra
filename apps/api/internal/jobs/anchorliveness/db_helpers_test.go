package anchorliveness

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcb "api/internal/platform/catalog/srcbangumi"
	srcv "api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	testDB    *gorm.DB
	testEG    *gorm.DB
	testDSN   string
	egTestDSN string
)

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err != nil {
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: cannot connect: %v\n", err)
		} else if err := migrate.Run(db); err != nil {
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog migrate failed: %v\n", err)
		} else if err := seed.Run(db); err != nil {
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog seed failed: %v\n", err)
		} else if err := srcv.EnsureSchema(db); err != nil {
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: src_vndb schema failed: %v\n", err)
		} else if err := srcb.EnsureSchema(db); err != nil {
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: src_bangumi schema failed: %v\n", err)
		} else {
			for _, ddl := range []string{
				`CREATE SCHEMA IF NOT EXISTS anchorliveness_eg`,
				`CREATE TABLE IF NOT EXISTS anchorliveness_eg.games (id int PRIMARY KEY, synced_at timestamptz)`,
				`CREATE TABLE IF NOT EXISTS anchorliveness_eg.creaters (id int PRIMARY KEY, synced_at timestamptz)`,
				`CREATE TABLE IF NOT EXISTS anchorliveness_eg.brands (id int PRIMARY KEY, synced_at timestamptz)`,
				`CREATE TABLE IF NOT EXISTS anchorliveness_eg.characters (id int PRIMARY KEY, synced_at timestamptz)`,
			} {
				if err := db.Exec(ddl).Error; err != nil {
					fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: eg fixture failed: %v\n", err)
					os.Exit(m.Run())
					return
				}
			}
			egDSN := dsn + " options='-csearch_path=anchorliveness_eg'"
			eg, err := gorm.Open(postgres.Open(egDSN), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
			if err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: cannot open eg dsn: %v\n", err)
			} else {
				testDSN = dsn
				egTestDSN = egDSN
				testDB = db
				testEG = eg
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "DB TESTS SKIPPED: TEST_DATABASE_DSN is unset")
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skipf(t, "the catalog test database is unavailable")
	}
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_external_ref", "catalog_release", "catalog_work",
		"catalog_character", "catalog_credit_name", "catalog_person", "catalog_label",
		"src_vndb.vn", "src_vndb.releases", "src_vndb.chars",
		"src_vndb.staff_alias", "src_vndb.staff", "src_vndb.producers",
		"src_bangumi.subject", "src_bangumi.character", "src_bangumi.person",
		"anchorliveness_eg.games", "anchorliveness_eg.creaters",
		"anchorliveness_eg.brands", "anchorliveness_eg.characters",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func withFloor(ln Lane, floor int64) Lane {
	ln.Floor = floor
	return ln
}

func findLane(t *testing.T, source, entity string) Lane {
	t.Helper()
	for _, ln := range LanesFor(source) {
		if ln.Entity == entity {
			return ln
		}
	}
	t.Fatalf("no lane %s/%s", source, entity)
	return Lane{}
}

func mkEntity(t *testing.T, etype int16, name string) int64 {
	t.Helper()
	switch etype {
	case model.EntityTypeWork:
		w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
		require.NoError(t, testDB.Create(&w).Error)
		return w.ID
	case model.EntityTypeRelease:
		wid := mkEntity(t, model.EntityTypeWork, name+"-work")
		rel := model.CatalogRelease{WorkID: wid, Kind: model.ReleaseKindDigital}
		require.NoError(t, testDB.Create(&rel).Error)
		return rel.ID
	case model.EntityTypeCharacter:
		c := model.CatalogCharacter{DisplayName: name}
		require.NoError(t, testDB.Create(&c).Error)
		return c.ID
	case model.EntityTypePerson:
		p := model.CatalogPerson{DisplayName: name}
		require.NoError(t, testDB.Create(&p).Error)
		return p.ID
	case model.EntityTypeCreditName:
		n := model.CatalogCreditName{Name: name, Kind: model.CreditNameKindMain, LinkVisibility: model.LinkVisibilityPublic}
		require.NoError(t, testDB.Create(&n).Error)
		return n.ID
	case model.EntityTypeLabel:
		l := model.CatalogLabel{DisplayName: name, Kind: model.LabelKindGameBrand}
		require.NoError(t, testDB.Create(&l).Error)
		return l.ID
	default:
		t.Fatalf("unsupported entity type %d", etype)
		return 0
	}
}

func mkRef(t *testing.T, etype, kind int16, entityID int64, src int16, ext string, dead bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: etype, EntityID: entityID, SourceID: src,
		ExternalID: ext, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
	if dead {
		require.NoError(t, testDB.Exec(`UPDATE catalog_external_ref SET dead_at = now()
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
			etype, entityID, src, ext).Error)
	}
}

func deadAt(t *testing.T, etype int16, entityID int64, src int16, ext string) *string {
	t.Helper()
	var got *string
	require.NoError(t, testDB.Raw(
		`SELECT dead_at::text FROM catalog_external_ref
		 WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
		etype, entityID, src, ext).Scan(&got).Error)
	return got
}

func workUpdatedAt(t *testing.T, workID int64) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, workID).Scan(&ts).Error)
	return ts
}

func seedMembership(t *testing.T, ln Lane, ids ...string) {
	t.Helper()
	now := time.Now()
	if ln.Freshness {
		for i, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			require.NoError(t, err)
			require.NoError(t, testDB.Exec(
				`INSERT INTO anchorliveness_eg.`+ln.Table+` (id, synced_at) VALUES (?, ?)`,
				n, now.Add(-time.Duration(i)*time.Second)).Error)
		}
		return
	}
	switch ln.Table {
	case "src_vndb.vn":
		rows := make([]srcv.VN, len(ids))
		for i, id := range ids {
			rows[i] = srcv.VN{ID: id, OLang: "ja", IngestedAt: now}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_vndb.releases":
		rows := make([]srcv.Release, len(ids))
		for i, id := range ids {
			rows[i] = srcv.Release{ID: id}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_vndb.chars":
		rows := make([]srcv.Char, len(ids))
		for i, id := range ids {
			rows[i] = srcv.Char{ID: id, IngestedAt: now}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_vndb.staff":
		rows := make([]srcv.Staff, len(ids))
		for i, id := range ids {
			rows[i] = srcv.Staff{ID: id}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_vndb.staff_alias":
		rows := make([]srcv.StaffAlias, len(ids))
		for i, id := range ids {
			aid, err := strconv.Atoi(id)
			require.NoError(t, err)
			rows[i] = srcv.StaffAlias{AID: aid, ID: "s0", Name: "n"}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_vndb.producers":
		rows := make([]srcv.Producer, len(ids))
		for i, id := range ids {
			rows[i] = srcv.Producer{ID: id, Type: "co", Name: id}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_bangumi.subject":
		rows := make([]srcb.Subject, len(ids))
		for i, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			require.NoError(t, err)
			rows[i] = srcb.Subject{
				ID: n, Type: 4, Name: id, ParserVersion: srcb.ParserVersion, IngestedAt: now,
			}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_bangumi.character":
		rows := make([]srcb.Character, len(ids))
		for i, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			require.NoError(t, err)
			rows[i] = srcb.Character{
				ID: n, Role: 1, Name: id, ParserVersion: srcb.ParserVersion, IngestedAt: now,
			}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	case "src_bangumi.person":
		rows := make([]srcb.Person, len(ids))
		for i, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			require.NoError(t, err)
			rows[i] = srcb.Person{
				ID: n, Type: 1, Name: id, ParserVersion: srcb.ParserVersion, IngestedAt: now,
			}
		}
		require.NoError(t, testDB.Create(&rows).Error)
	default:
		t.Fatalf("seedMembership: unknown table %s", ln.Table)
	}
}

func sampleIDs(ln Lane) (gone, back, live string) {
	if ln.NumericID {
		return "9001", "9002", "9003"
	}
	return "gone", "back", "live"
}

func runOpts(t *testing.T, source string, apply bool, lanes []Lane) Opts {
	t.Helper()
	o := Opts{DSN: testDSN, Source: source, Apply: apply, Lanes: lanes}
	if source == SourceEG {
		o.EGDSN = egTestDSN
	}
	return o
}
