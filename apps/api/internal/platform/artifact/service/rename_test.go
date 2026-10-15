package service

import (
	"context"
	stderrors "errors"
	"testing"

	"api/internal/platform/artifact/model"
)

type dispositionCall struct{ key, name, contentType string }

type fakeDisposition struct {
	calls []dispositionCall
	err   error
}

func (f *fakeDisposition) SetContentDisposition(_ context.Context, key, name, contentType string) error {
	f.calls = append(f.calls, dispositionCall{key, name, contentType})
	return f.err
}

func TestRenameRewritesTheStoredDispositionThenTheRow(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	store := &fakeDisposition{}
	svc.disposition = store
	id := seedArtifact(t, db, site, "u1", model.StatusReady)
	db.Model(&model.Artifact{}).Where("uuid = ?", id).Updates(map[string]any{"mime_type": "application/zip", "file_size": 100})

	resp, err := svc.Rename(ctx, id, site, "汉化补丁 v2.ZIP")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if resp.Name != "汉化补丁 v2.ZIP" {
		t.Errorf("response name = %q", resp.Name)
	}
	if len(store.calls) != 1 || store.calls[0] != (dispositionCall{site + "/u1.zip", "汉化补丁 v2.ZIP", "application/zip"}) {
		t.Errorf("disposition calls = %+v", store.calls)
	}
	var stored model.Artifact
	db.Where("uuid = ?", id).First(&stored)
	if stored.Name != "汉化补丁 v2.ZIP" {
		t.Errorf("stored name = %q", stored.Name)
	}

	if _, err := svc.Rename(ctx, id, site, "汉化补丁 v2.ZIP"); err != nil || len(store.calls) != 1 {
		t.Errorf("same-name rename = %v with %d storage calls, want a no-op", err, len(store.calls))
	}

	store.err = stderrors.New("b2 down")
	if _, err := svc.Rename(ctx, id, site, "third.zip"); err == nil {
		t.Fatal("Rename reported success though the storage copy failed")
	}
	db.Where("uuid = ?", id).First(&stored)
	if stored.Name != "汉化补丁 v2.ZIP" {
		t.Errorf("a failed storage copy still renamed the row to %q", stored.Name)
	}
}

func TestRenameRefusals(t *testing.T) {
	svc, db, site := ownerTestService(t)
	ctx := context.Background()
	store := &fakeDisposition{}
	svc.disposition = store
	ready := seedArtifact(t, db, site, "ready", model.StatusReady)
	uploading := seedArtifact(t, db, site, "uploading", model.StatusUploading)
	huge := seedArtifact(t, db, site, "huge", model.StatusReady)
	db.Model(&model.Artifact{}).Where("uuid = ?", huge).Update("file_size", int64(maxInPlaceCopyBytes)+1)
	gone := seedArtifact(t, db, site, "gone", model.StatusReady)
	db.Where("uuid = ?", gone).Delete(&model.Artifact{})

	refused := []struct{ name, uuid, site, to string }{
		{"a changed extension", ready, site, "a.rar"},
		{"a dropped extension", ready, site, "a"},
		{"an unfinished upload", uploading, site, "b.zip"},
		{"a file over 5 GiB", huge, site, "c.zip"},
	}
	for _, tc := range refused {
		if _, err := svc.Rename(ctx, tc.uuid, tc.site, tc.to); !stderrors.Is(err, ErrRenameRefused) {
			t.Errorf("%s: Rename = %v, want ErrRenameRefused", tc.name, err)
		}
	}
	missing := []struct{ name, uuid, site string }{
		{"another site's artifact", ready, "other" + site},
		{"a deleted artifact", gone, site},
	}
	for _, tc := range missing {
		if _, err := svc.Rename(ctx, tc.uuid, tc.site, "d.zip"); !stderrors.Is(err, ErrNotFound) {
			t.Errorf("%s: Rename = %v, want ErrNotFound", tc.name, err)
		}
	}
	if len(store.calls) != 0 {
		t.Errorf("a refused rename still touched storage: %+v", store.calls)
	}
}
