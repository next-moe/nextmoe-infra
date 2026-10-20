package main

import (
	"testing"

	"api/internal/platform/apiv2/repr"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUsersOfResolvesLiveErasedAndMissingAccounts(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Exec(`CREATE TEMP TABLE users (
		id bigint PRIMARY KEY, name text NOT NULL, avatar_image_hash text,
		anonymized_at timestamptz, deleted_at timestamptz)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users VALUES
		(1, 'alice', '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', NULL, NULL),
		(2, 'bob', NULL, NULL, NULL),
		(3, '已注销#3', NULL, now(), NULL),
		(4, 'carol', 'def', NULL, now())`).Error)

	got, err := usersOf(db, "https://cdn.example.test/image")(t.Context(), []int64{1, 2, 3, 4, 5})
	require.NoError(t, err)
	require.Len(t, got, 5, "every id asked for comes back")

	require.NotNil(t, got[1].Avatar)
	assert.Equal(t, repr.UserRef{Object: "user", ID: "1", Name: "alice", Avatar: got[1].Avatar}, got[1])
	assert.Contains(t, *got[1].Avatar, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	assert.Equal(t, repr.UserRef{Object: "user", ID: "2", Name: "bob"}, got[2])
	for _, id := range []int64{3, 4, 5} {
		u := got[id]
		assert.Equal(t, erasedUserName(id), u.Name, "account %d: erased, removed and unknown accounts read the placeholder", id)
		assert.Nil(t, u.Avatar, "account %d keeps no avatar", id)
	}
}
