package source

import (
	"context"
	"os"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	communityMigrate "api/internal/platform/community/migrate"
	communityModel "api/internal/platform/community/model"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("chat/source")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("chat/source", "cannot connect to test database: %v", err)
	}
	if err := communityMigrate.Run(db); err != nil {
		dbtest.SkipMainf("chat/source", "community migration failed: %v", err)
	}
	if err := db.AutoMigrate(&siteModel.Role{}, &authModel.User{}); err != nil {
		dbtest.SkipMainf("chat/source", "users migration failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

const (
	alice int64 = 910001
	bob   int64 = 910002
	carol int64 = 910003
)

func cleanup(t *testing.T) {
	t.Helper()
	ids := []int64{alice, bob, carol}
	t.Cleanup(func() {
		testDB.Where("blocker_id IN ? OR blocked_id IN ?", ids, ids).Delete(&communityModel.CommunityUserBlock{})
		testDB.Where("follower_id IN ? OR followee_id IN ?", ids, ids).Delete(&communityModel.CommunityUserFollow{})
		testDB.Where("user_id IN ?", ids).Delete(&communityModel.CommunityTrust{})
		testDB.Unscoped().Where("id IN ?", ids).Delete(&authModel.User{})
	})
}

func TestRelationshipsReadTheCommunityTables(t *testing.T) {
	cleanup(t)
	ctx := context.Background()
	now := time.Now()
	must(t, testDB.Create(&communityModel.CommunityUserBlock{BlockerID: alice, BlockedID: bob, OriginSite: "letmoe", CreatedAt: now}).Error)
	must(t, testDB.Create(&communityModel.CommunityUserFollow{FollowerID: carol, FolloweeID: alice, CreatedAt: &now}).Error)
	must(t, testDB.Create(&communityModel.CommunityTrust{UserID: carol, Level: 2}).Error)

	r := NewRelationships(testDB)
	for _, pair := range [][2]int64{{alice, bob}, {bob, alice}} {
		if blocked, err := r.BlockedEitherWay(ctx, pair[0], pair[1]); err != nil || !blocked {
			t.Fatalf("block %v: %v %v", pair, blocked, err)
		}
	}
	if blocked, err := r.BlockedEitherWay(ctx, alice, carol); err != nil || blocked {
		t.Fatalf("no block between alice and carol: %v %v", blocked, err)
	}
	if f, err := r.Follows(ctx, carol, alice); err != nil || !f {
		t.Fatalf("carol follows alice: %v %v", f, err)
	}
	if f, err := r.Follows(ctx, alice, carol); err != nil || f {
		t.Fatalf("a follow has a direction: %v %v", f, err)
	}
	if lv, err := r.TrustLevel(ctx, carol); err != nil || lv != 2 {
		t.Fatalf("carol's level: %d %v", lv, err)
	}
	if lv, err := r.TrustLevel(ctx, bob); err != nil || lv != 0 {
		t.Fatalf("no trust row is level 0: %d %v", lv, err)
	}
}

func TestProfilesMarkDeletedAccounts(t *testing.T) {
	cleanup(t)
	now := time.Now().UTC().Truncate(time.Second)
	must(t, testDB.Create(&authModel.User{ID: uint(alice), Name: "alice910001", Email: "alice@chat.test", Avatar: "https://a.example/a.webp"}).Error)
	must(t, testDB.Create(&authModel.User{ID: uint(bob), Name: "bob910002", Email: "bob@chat.test", AnonymizedAt: &now}).Error)
	must(t, testDB.Create(&authModel.User{ID: uint(carol), Name: "carol910003", Email: "carol@chat.test"}).Error)
	must(t, testDB.Delete(&authModel.User{}, carol).Error)

	got, err := NewUsers(testDB).Profiles(context.Background(), []int64{alice, bob, carol, 919999})
	if err != nil {
		t.Fatal(err)
	}
	if a := got[alice]; a.Name != "alice910001" || a.Avatar != "https://a.example/a.webp" || a.Deleted || a.CreatedAt.IsZero() {
		t.Fatalf("alice: %+v", a)
	}
	if !got[bob].Deleted || !got[carol].Deleted {
		t.Fatalf("anonymized and soft-deleted accounts are deleted: %+v %+v", got[bob], got[carol])
	}
	if _, ok := got[919999]; ok {
		t.Fatal("an unknown id has no profile")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
