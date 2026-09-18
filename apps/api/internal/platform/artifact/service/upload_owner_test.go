package service

import (
	"context"
	stderrors "errors"
	"testing"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/testsupport/dbtest"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func ownerTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "cannot connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Artifact{}, &model.Manifest{}); err != nil {
		dbtest.Skipf(t, "artifact migration failed: %v", err)
	}
	site := "t" + uuid.NewString()[:8]
	t.Cleanup(func() { db.Unscoped().Where("site_key = ?", site).Delete(&model.Artifact{}) })
	return New(repository.NewArtifactRepository(db), nil, nil), db, site
}

func seedArtifact(t *testing.T, db *gorm.DB, site, uploader string, status int) string {
	t.Helper()
	a := &model.Artifact{
		UUID:         uuid.NewString(),
		SiteKey:      site,
		UploaderSub:  uploader,
		Name:         "a.zip",
		FileKey:      site + "/" + uploader + ".zip",
		ReportedSize: 1,
		Status:       status,
	}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return a.UUID
}

func TestResumeUploadIsScopedToTheUploader(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	alices := seedArtifact(t, db, site, "alice", model.StatusReady)
	serverUpload := seedArtifact(t, db, site, "", model.StatusReady)

	cases := []struct {
		name  string
		uuid  string
		owner string
		want  error
	}{
		{"the uploader passes the owner check", alices, "alice", ErrNotResumable},
		{"the site's server credential passes it", alices, "", ErrNotResumable},
		{"another user of the same site does not", alices, "bob", ErrNotFound},
		{"a user does not reach a server-side upload", serverUpload, "bob", ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ResumeUpload(ctx, tc.uuid, site, tc.owner)
			if !stderrors.Is(err, tc.want) {
				t.Fatalf("ResumeUpload(owner=%q) = %v, want %v", tc.owner, err, tc.want)
			}
		})
	}
}

func TestCompleteUploadIsScopedToTheUploader(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	alices := seedArtifact(t, db, site, "alice", model.StatusReady)
	serverUpload := seedArtifact(t, db, site, "", model.StatusReady)

	cases := []struct {
		name  string
		uuid  string
		owner string
		want  error
	}{
		{"the uploader completes", alices, "alice", nil},
		{"the site's server credential completes", alices, "", nil},
		{"another user of the same site is told it does not exist", alices, "bob", ErrNotFound},
		{"a user does not reach a server-side upload", serverUpload, "bob", ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.CompleteUpload(ctx, tc.uuid, site, tc.owner, dto.CompleteUploadRequest{})
			if !stderrors.Is(err, tc.want) {
				t.Fatalf("CompleteUpload(owner=%q) = %v, want %v", tc.owner, err, tc.want)
			}
			if tc.want == nil && resp.UUID != tc.uuid {
				t.Fatalf("CompleteUpload returned %q, want %q", resp.UUID, tc.uuid)
			}
		})
	}
}
