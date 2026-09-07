package service

import (
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	srcb "api/internal/platform/catalog/srcbangumi"
	srcv "api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB      *gorm.DB
	testResolve *ResolveService
	testMerge   *MergeService
	testGuard   *GuardService
	testQueues  *AdminQueueService
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/service")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		dbtest.SkipMainf("catalog/service", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("catalog/service", "catalog migration failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("catalog/service", "catalog seeding failed: %v", err)
	}
	if err := srcb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("catalog/service", "src_bangumi schema failed: %v", err)
	}
	if err := srcv.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("catalog/service", "src_vndb schema failed: %v", err)
	}

	testDB = db
	redirects := repository.NewRedirectRepository(db)
	testResolve = NewResolveService(redirects)
	testMerge = NewMergeService(db, testResolve, repository.NewProposalRepository(db), repository.NewRevisionRepository(db))
	testGuard = NewGuardService(db)
	testQueues = NewAdminQueueService(db, testMerge)

	release := acquireCatalogSuiteLock(db)
	code := m.Run()
	release()
	sweepWorksSearchIndexes()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_credit",
		"catalog_match_candidate", "catalog_match_rejection", "catalog_external_ref",
		"catalog_merge_proposal", "catalog_revision", "catalog_redirect", "catalog_entity_usage",
		"catalog_work_relation", "catalog_entity_relation",
		"catalog_work_title", "catalog_release", "catalog_work",
		"catalog_work_intro", "catalog_work_cover", "catalog_work_screenshot",
		"catalog_work_rating", "catalog_work_tag", "catalog_work_popularity",
		"catalog_work_playtime", "catalog_work_platform",
		"catalog_work_engine", "catalog_engine",
		"catalog_series_member", "catalog_series",
		"catalog_work_label", "catalog_release_label", "catalog_work_character",
		"catalog_tag_work_count",
		"catalog_name_alias", "catalog_credit_name", "catalog_person_intro", "catalog_person",
		"catalog_label_alias", "catalog_label_intro", "catalog_label_relation", "catalog_label",
		"catalog_character_alias", "catalog_character_intro",
		"catalog_character_trait_link", "catalog_character_trait", "catalog_character",
		"catalog_survivorship_rule",
		"catalog_user_folder_item", "catalog_user_folder",
		"edit_suppressed_row",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func createPerson(t *testing.T, displayName string) *model.CatalogPerson {
	t.Helper()
	p := &model.CatalogPerson{DisplayName: displayName}
	if err := testDB.Create(p).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	return p
}

func createCreditName(t *testing.T, personID *int64, name string) *model.CatalogCreditName {
	t.Helper()
	n := &model.CatalogCreditName{
		PersonID: personID, Name: name,
		Kind: model.CreditNameKindMain, LinkVisibility: model.LinkVisibilityPublic,
	}
	if err := testDB.Create(n).Error; err != nil {
		t.Fatalf("create credit name: %v", err)
	}
	return n
}

func createNameAlias(t *testing.T, creditNameID int64, name, lang string) *model.CatalogNameAlias {
	t.Helper()
	a := &model.CatalogNameAlias{
		CreditNameID: creditNameID, Name: name, Lang: lang,
		Kind: model.AliasKindSpellingVariant,
	}
	if err := testDB.Create(a).Error; err != nil {
		t.Fatalf("create name alias: %v", err)
	}
	return a
}

func createWork(t *testing.T, displayName string) *model.CatalogWork {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: displayName,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	if err := testDB.Create(w).Error; err != nil {
		t.Fatalf("create work: %v", err)
	}
	return w
}

func createCharacter(t *testing.T, displayName string) *model.CatalogCharacter {
	t.Helper()
	c := &model.CatalogCharacter{DisplayName: displayName}
	if err := testDB.Create(c).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	return c
}

func createWorkCharacter(t *testing.T, workID, characterID int64, kind, spoiler int16) *model.CatalogWorkCharacter {
	t.Helper()
	e := &model.CatalogWorkCharacter{
		WorkID: workID, CharacterID: characterID, Kind: kind, Spoiler: spoiler,
		MatchedBy: "import:test",
	}
	if err := testDB.Create(e).Error; err != nil {
		t.Fatalf("create work_character: %v", err)
	}
	return e
}

func createCharacterAlias(t *testing.T, characterID int64, name, lang string) *model.CatalogCharacterAlias {
	t.Helper()
	a := &model.CatalogCharacterAlias{
		CharacterID: characterID, Name: name, Lang: lang,
		Kind: model.AliasKindSpellingVariant,
	}
	if err := testDB.Create(a).Error; err != nil {
		t.Fatalf("create character alias: %v", err)
	}
	return a
}

func createCredit(t *testing.T, workID, creditNameID, roleID int64, characterID *int64) *model.CatalogCredit {
	t.Helper()
	c := &model.CatalogCredit{
		WorkID: workID, CreditNameID: creditNameID, RoleID: roleID,
		CharacterID: characterID, Spoiler: model.SpoilerNone,
	}
	if err := testDB.Create(c).Error; err != nil {
		t.Fatalf("create credit: %v", err)
	}
	return c
}

func addUsage(t *testing.T, entityType int16, entityID int64, site string, firstUsed time.Time) {
	t.Helper()
	if err := testDB.Create(&model.CatalogEntityUsage{
		EntityType: entityType, EntityID: entityID, Site: site,
		FirstUsedAt: firstUsed, LastConfirmedAt: firstUsed,
	}).Error; err != nil {
		t.Fatalf("add usage: %v", err)
	}
}

func addExternalRef(t *testing.T, entityType int16, entityID int64, sourceID int16, externalID string, linkKind int16) {
	t.Helper()
	if err := testDB.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: sourceID,
		ExternalID: externalID, LinkKind: linkKind, MatchedBy: "import:test",
	}).Error; err != nil {
		t.Fatalf("add external ref: %v", err)
	}
}

func approveAndForceExecutable(t *testing.T, proposalID int64) {
	t.Helper()
	if err := testMerge.ApproveMerge(t.Context(), proposalID, 7); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour' WHERE id = ?`, proposalID).Error; err != nil {
		t.Fatalf("rewind execute_after: %v", err)
	}
}

func seededRoleID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := testDB.Raw(`SELECT id FROM catalog_role WHERE key = 'developer'`).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("seeded role lookup failed: id=%d err=%v", id, err)
	}
	return id
}
