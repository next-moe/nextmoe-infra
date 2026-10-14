package handler

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/service"
	"api/internal/testsupport/dbtest"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type keySet map[string]bool

func (k keySet) Put(_ context.Context, key string, _ []byte, _ string) error {
	k[key] = true
	return nil
}
func (k keySet) Delete(_ context.Context, key string) error { delete(k, key); return nil }

func TestTakedownReachesAPinnedArtifactTheSiteAlreadyDeleted(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "cannot connect to test database: %v", err)
	}
	if err := db.AutoMigrate(model.All()...); err != nil {
		t.Fatalf("artifact migration: %v", err)
	}
	ctx := context.Background()
	site := "t" + uuid.NewString()[:8]
	t.Cleanup(func() {
		db.Where("site_key = ?", site).Delete(&model.Composite{})
		db.Unscoped().Where("site_key = ?", site).Delete(&model.Artifact{})
	})

	seed := func() *model.Artifact {
		id := uuid.NewString()
		a := &model.Artifact{UUID: id, SiteKey: site, Name: "a.zip", FileKey: site + "/" + id + ".zip",
			FileSize: 10, ReportedSize: 10, Status: model.StatusReady, Public: true}
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	base, loose := seed(), seed()
	objects := keySet{}
	composites := service.NewCompositeService(repository.NewCompositeRepository(db), objects, objects)
	resp, err := composites.Create(ctx, dto.CreateCompositeRequest{
		Filename: "c.zip", ContentType: "application/zip", Size: 10, BLAKE3: strings.Repeat("d", 64),
		Sources:  []dto.CompositeSourceInput{{ArtifactUUID: base.UUID}},
		Segments: []dto.CompositeSegment{{Source: 0, Offset: 0, Length: 10}},
	}, service.CompositeParams{Site: site})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(base).Error; err != nil {
		t.Fatal(err)
	}

	h := &AdminHandler{db: db, composites: composites}
	removed, err := h.Delete(ctx, base.UUID)
	if err != nil || removed != 1 {
		t.Fatalf("takedown of a soft-deleted pinned artifact = %d, %v; want 1 composite removed", removed, err)
	}
	if objects["c/"+site+"/"+resp.ID] {
		t.Error("the composite's manifest is still published")
	}
	if _, err := h.Delete(ctx, base.UUID); !stderrors.Is(err, errAdminNotFound) {
		t.Errorf("second takedown = %v, want not found", err)
	}
	if removed, err := h.Delete(ctx, loose.UUID); err != nil || removed != 0 {
		t.Errorf("plain takedown = %d, %v; want 0, nil", removed, err)
	}
	var live int64
	db.Model(&model.Artifact{}).Where("uuid = ?", loose.UUID).Count(&live)
	if live != 0 {
		t.Error("plain takedown left the artifact live")
	}
}
