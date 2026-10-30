package service

import (
	"context"
	stderrors "errors"
	"reflect"
	"testing"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"gorm.io/gorm"
)

func TestChoosePartSize(t *testing.T) {
	const mib = 1 << 20
	cases := []struct {
		name                          string
		fileSize, requested, configed int64
		want                          int64
	}{
		{"no hint uses the configured size", 100 * mib, 0, 16 * mib, 16 * mib},
		{"hint wins over the configured size", 100 * mib, 8 * mib, 16 * mib, 8 * mib},
		{"hint below the S3 floor is raised", 100 * mib, 1 * mib, 16 * mib, minPartSize},
		{"hint above the S3 ceiling is lowered", 100 * mib, 6 << 30, 16 * mib, maxPartSize},
		{"grown to stay within 10000 parts", 100_000 * mib, 5 * mib, 16 * mib, 10 * mib},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := choosePartSize(c.fileSize, c.requested, c.configed); got != c.want {
				t.Errorf("choosePartSize = %d, want %d", got, c.want)
			}
		})
	}
}

func TestPlanPartsRejectsWrongSizedAndForeignParts(t *testing.T) {
	uploaded := []storage.UploadedPart{
		{PartNumber: 3, ETag: "c", Size: 5},
		{PartNumber: 1, ETag: "a", Size: 10},
		{PartNumber: 2, ETag: "b", Size: 7},
		{PartNumber: 9, ETag: "z", Size: 10},
	}
	good, missing := planParts(25, 10, uploaded)
	if want := []storage.UploadedPart{{PartNumber: 1, ETag: "a", Size: 10}, {PartNumber: 3, ETag: "c", Size: 5}}; !reflect.DeepEqual(good, want) {
		t.Errorf("good = %+v, want %+v", good, want)
	}
	if want := []int32{2}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

type fakeStore struct {
	objectStore
	parts       []storage.UploadedPart
	listErr     error
	completeErr error
	assembled   bool
	size        int64
	completed   [][]storage.CompletedPart
	presigned   []int32
}

func (f *fakeStore) ListParts(context.Context, string, string) ([]storage.UploadedPart, error) {
	return f.parts, f.listErr
}

func (f *fakeStore) CompleteMultipart(_ context.Context, _, _ string, parts []storage.CompletedPart) error {
	f.completed = append(f.completed, parts)
	if f.completeErr != nil {
		return f.completeErr
	}
	f.assembled = true
	return nil
}

func (f *fakeStore) Exists(context.Context, string) (bool, error) { return f.assembled, nil }

func (f *fakeStore) HeadSize(context.Context, string) (int64, error) { return f.size, nil }

func (f *fakeStore) PresignUploadPart(_ context.Context, _, _ string, n int32, _ time.Duration) (string, error) {
	f.presigned = append(f.presigned, n)
	return "https://b2.example/part", nil
}

func seedMultipart(t *testing.T, db *gorm.DB, site string) *model.Artifact {
	t.Helper()
	id := seedArtifact(t, db, site, "u1", model.StatusUploading)
	db.Model(&model.Artifact{}).Where("uuid = ?", id).Updates(map[string]any{
		"reported_size": 25, "upload_id": "up-1", "part_size": 10,
	})
	var a model.Artifact
	db.Where("uuid = ?", id).First(&a)
	return &a
}

func allParts() []storage.UploadedPart {
	return []storage.UploadedPart{
		{PartNumber: 1, ETag: "a", Size: 10},
		{PartNumber: 2, ETag: "b", Size: 10},
		{PartNumber: 3, ETag: "c", Size: 5},
	}
}

func statusOf(t *testing.T, db *gorm.DB, id string) int {
	t.Helper()
	var a model.Artifact
	db.Where("uuid = ?", id).First(&a)
	return a.Status
}

func TestCompleteSurvivesATransientStorageFailure(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	a := seedMultipart(t, db, site)
	store := &fakeStore{parts: allParts(), completeErr: stderrors.New("503 service unavailable"), size: 25}
	svc.store = store

	if _, err := svc.CompleteUpload(ctx, a.UUID, site, "u1", dto.CompleteUploadRequest{}); err == nil {
		t.Fatal("CompleteUpload succeeded through a storage failure")
	}
	if got := statusOf(t, db, a.UUID); got != model.StatusUploading {
		t.Fatalf("status after a transient failure = %d, want uploading", got)
	}

	store.completeErr = nil
	if _, err := svc.CompleteUpload(ctx, a.UUID, site, "u1", dto.CompleteUploadRequest{}); err != nil {
		t.Fatalf("retried CompleteUpload: %v", err)
	}
	if got := statusOf(t, db, a.UUID); got != model.StatusReady {
		t.Errorf("status after retry = %d, want ready", got)
	}
	want := []storage.CompletedPart{{PartNumber: 1, ETag: "a"}, {PartNumber: 2, ETag: "b"}, {PartNumber: 3, ETag: "c"}}
	if len(store.completed) != 2 || !reflect.DeepEqual(store.completed[1], want) {
		t.Errorf("complete calls = %+v", store.completed)
	}
}

func TestCompleteAfterALostResponseDoesNotReassemble(t *testing.T) {
	svc, db, site := ownerTestService(t)
	a := seedMultipart(t, db, site)
	store := &fakeStore{assembled: true, size: 25}
	svc.store = store

	if _, err := svc.CompleteUpload(context.Background(), a.UUID, site, "u1", dto.CompleteUploadRequest{}); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if len(store.completed) != 0 {
		t.Errorf("reassembled an already assembled object: %+v", store.completed)
	}
	if got := statusOf(t, db, a.UUID); got != model.StatusReady {
		t.Errorf("status = %d, want ready", got)
	}
}

func TestCompleteWithAMissingPartStaysResumable(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	a := seedMultipart(t, db, site)
	store := &fakeStore{parts: []storage.UploadedPart{{PartNumber: 1, ETag: "a", Size: 10}, {PartNumber: 2, ETag: "b", Size: 4}}}
	svc.store = store

	if _, err := svc.CompleteUpload(ctx, a.UUID, site, "u1", dto.CompleteUploadRequest{}); !stderrors.Is(err, ErrIncomplete) {
		t.Fatalf("CompleteUpload err = %v, want ErrIncomplete", err)
	}
	if len(store.completed) != 0 {
		t.Errorf("assembled an incomplete upload: %+v", store.completed)
	}

	resp, err := svc.ResumeUpload(ctx, a.UUID, site, "u1")
	if err != nil {
		t.Fatalf("ResumeUpload: %v", err)
	}
	if !reflect.DeepEqual(store.presigned, []int32{2, 3}) {
		t.Errorf("resume re-signed parts %v, want [2 3] (part 2 is short)", store.presigned)
	}
	if len(resp.UploadedParts) != 1 || resp.UploadedParts[0].PartNumber != 1 {
		t.Errorf("uploaded_parts = %+v, want only part 1", resp.UploadedParts)
	}
}

func TestCompleteOnAVanishedUploadFailsIt(t *testing.T) {
	svc, db, site := ownerTestService(t)
	a := seedMultipart(t, db, site)
	svc.store = &fakeStore{listErr: &types.NoSuchUpload{}}

	if _, err := svc.CompleteUpload(context.Background(), a.UUID, site, "u1", dto.CompleteUploadRequest{}); !stderrors.Is(err, ErrNotResumable) {
		t.Fatalf("CompleteUpload err = %v, want ErrNotResumable", err)
	}
	if got := statusOf(t, db, a.UUID); got != model.StatusFailed {
		t.Errorf("status = %d, want failed", got)
	}
}
