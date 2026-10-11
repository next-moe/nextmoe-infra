package main

import (
	"fmt"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDropLegacyAvatarColumnKeepsTheHash(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		dbtest.Skipf(t, "pgcrypto: %v", err)
	}
	if err := db.AutoMigrate(&authModel.User{}); err != nil {
		dbtest.Skipf(t, "migrate users: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar varchar(1007) DEFAULT ''`).Error; err != nil {
		t.Fatalf("recreate the legacy column: %v", err)
	}

	tag := time.Now().UnixNano() % 1e8
	hash := fmt.Sprintf("%064x", tag)
	u := &authModel.User{
		Name:            fmt.Sprintf("av%08d", tag),
		Email:           fmt.Sprintf("av%d@example.test", tag),
		AvatarImageHash: &hash,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(&authModel.User{}, u.ID) })
	if err := db.Exec(`UPDATE users SET avatar = 'https://image.kungal.com/avatar/old.webp' WHERE id = ?`, u.ID).Error; err != nil {
		t.Fatalf("seed the legacy url: %v", err)
	}

	for pass := 1; pass <= 2; pass++ {
		if err := authModel.DropLegacyAvatarColumn(db); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	if db.Migrator().HasColumn(&authModel.User{}, "avatar") {
		t.Fatal("users.avatar is still there")
	}
	var got authModel.User
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.AvatarImageHash == nil || *got.AvatarImageHash != hash {
		t.Fatalf("avatar_image_hash = %v, want %q", got.AvatarImageHash, hash)
	}
}
