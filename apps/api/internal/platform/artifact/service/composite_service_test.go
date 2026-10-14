package service

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"flag"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/testsupport/dbtest"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var updateGolden = flag.Bool("update", false, "rewrite the shared composite manifest fixture")

const manifestFixture = "../../../../../../workers/dl-composite/test/fixtures/manifest-v1.bin"

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, key string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), body...)
	m.puts++
	return nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func TestManifestMatchesTheWorkerFixture(t *testing.T) {
	c := &model.Composite{
		PublicID:    "0123456789abcdef0123456789abcdef",
		SiteKey:     "letmoe",
		Filename:    "孤独に効く百合 + 汉化补丁.zip",
		ContentType: "application/zip",
		Size:        40,
		BLAKE3:      strings.Repeat("ab", 32),
		CreatedAt:   time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	}
	sources := []resolvedSource{
		{artifact: &model.Artifact{FileKey: "letmoe/base.zip"}, size: 100},
		{artifact: &model.Artifact{FileKey: "letmoe/layer.zip"}, size: 50},
		{data: []byte("GLUE-0123456789"), size: 15},
	}
	segments := []dto.CompositeSegment{
		{Source: 0, Offset: 0, Length: 10},
		{Source: 2, Offset: 0, Length: 5},
		{Source: 1, Offset: 20, Length: 10},
		{Source: 2, Offset: 5, Length: 10},
		{Source: 0, Offset: 90, Length: 5},
	}
	got, err := encodeManifest(c, segments, sources)
	if err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		if err := os.WriteFile(manifestFixture, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(manifestFixture)
	if err != nil {
		t.Fatalf("read fixture (go test -run TestManifestMatchesTheWorkerFixture -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("manifest drifted from the Worker's fixture\n got: %q\nwant: %q", got, want)
	}

	head, tail, ok := bytes.Cut(got, []byte("\n"))
	if !ok || string(tail) != "GLUE-0123456789" {
		t.Fatalf("manifest is not header-newline-inline: tail %q", tail)
	}
	var h manifestHeader
	if err := json.Unmarshal(head, &h); err != nil {
		t.Fatal(err)
	}
	if h.ContentDisposition != "attachment; filename*=UTF-8''%E5%AD%A4%E7%8B%AC%E3%81%AB%E5%8A%B9%E3%81%8F%E7%99%BE%E5%90%88%20%2B%20%E6%B1%89%E5%8C%96%E8%A1%A5%E4%B8%81.zip" {
		t.Errorf("content_disposition = %q", h.ContentDisposition)
	}
	if h.LastModified != "Wed, 14 Oct 2026 12:00:00 GMT" || h.ETag != `"`+c.BLAKE3+`"` {
		t.Errorf("validators = %q %q", h.LastModified, h.ETag)
	}
}

func seg(source int, offset, length int64) dto.CompositeSegment {
	return dto.CompositeSegment{Source: source, Offset: offset, Length: length}
}

func TestCheckSegments(t *testing.T) {
	sources := []resolvedSource{{size: 100}, {size: 10}}
	plan := func(size int64, segs ...dto.CompositeSegment) dto.CreateCompositeRequest {
		return dto.CreateCompositeRequest{Size: size, Segments: segs}
	}
	cases := []struct {
		name string
		req  dto.CreateCompositeRequest
		ok   bool
	}{
		{"exact cover", plan(110, seg(0, 0, 100), seg(1, 0, 10)), true},
		{"a source used twice", plan(30, seg(0, 0, 10), seg(1, 0, 10), seg(0, 50, 10)), true},
		{"source index out of range", plan(10, seg(2, 0, 10)), false},
		{"runs past the end of its source", plan(11, seg(1, 0, 11)), false},
		{"offset past the end", plan(1, seg(1, 10, 1), seg(0, 0, 1)), false},
		{"lengths short of size", plan(111, seg(0, 0, 100), seg(1, 0, 10)), false},
		{"lengths over size", plan(109, seg(0, 0, 100), seg(1, 0, 10)), false},
		{"a source no segment uses", plan(100, seg(0, 0, 100)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSegments(tc.req, sources)
			if tc.ok != (err == nil) {
				t.Fatalf("checkSegments = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !stderrors.Is(err, ErrInvalidPlan) {
				t.Fatalf("error %v is not ErrInvalidPlan", err)
			}
		})
	}
}

func TestCompositeIDIsContentAddressedPerSite(t *testing.T) {
	req := dto.CreateCompositeRequest{
		Filename: "a.zip", ContentType: "application/zip", Size: 3, BLAKE3: strings.Repeat("0", 64),
		Sources:  []dto.CompositeSourceInput{{ArtifactUUID: "D874A243-0000-4000-8000-000000000000"}, {Data: []byte("x")}},
		Segments: []dto.CompositeSegment{seg(0, 0, 2), seg(1, 0, 1)},
	}
	id := compositeID("letmoe", req)
	if len(id) != 32 || id != compositeID("letmoe", req) {
		t.Fatalf("id %q is not a stable 32-hex digest", id)
	}
	lower := req
	lower.Sources = []dto.CompositeSourceInput{{ArtifactUUID: "d874a243-0000-4000-8000-000000000000"}, {Data: []byte("x")}}
	if compositeID("letmoe", lower) != id {
		t.Error("uuid case changed the id")
	}
	if compositeID("moyu", req) == id {
		t.Error("another site got the same id")
	}
	glue := req
	glue.Sources = []dto.CompositeSourceInput{req.Sources[0], {Data: []byte("y")}}
	if compositeID("letmoe", glue) == id {
		t.Error("different inline bytes got the same id")
	}
	named := req
	named.Filename = "b.zip"
	if compositeID("letmoe", named) == id {
		t.Error("a different filename got the same id")
	}
}

type compositeFixture struct {
	svc   *CompositeService
	db    *gorm.DB
	store *memStore
	site  string
}

func newCompositeFixture(t *testing.T) *compositeFixture {
	t.Helper()
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
	site := "t" + uuid.NewString()[:8]
	sites := []string{site, "other" + site, "third" + site}
	t.Cleanup(func() {
		db.Where("site_key IN ?", sites).Delete(&model.Composite{})
		db.Unscoped().Where("site_key IN ?", sites).Delete(&model.Artifact{})
	})
	store := newMemStore()
	return &compositeFixture{
		svc:   NewCompositeService(repository.NewCompositeRepository(db), store, store),
		db:    db,
		store: store,
		site:  site,
	}
}

func (f *compositeFixture) artifact(t *testing.T, site string, size int64, mutate func(*model.Artifact)) *model.Artifact {
	t.Helper()
	id := uuid.NewString()
	a := &model.Artifact{
		UUID: id, SiteKey: site, Name: "p.zip", FileKey: site + "/" + id + ".zip",
		ReportedSize: size, FileSize: size, Status: model.StatusReady, Public: true,
	}
	if mutate != nil {
		mutate(a)
	}
	if err := f.db.Create(a).Error; err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return a
}

func (f *compositeFixture) plan(base, layer *model.Artifact) dto.CreateCompositeRequest {
	return dto.CreateCompositeRequest{
		Filename: "base + patch.zip", ContentType: "application/zip", Size: 130, BLAKE3: strings.Repeat("c", 64),
		Sources: []dto.CompositeSourceInput{
			{ArtifactUUID: base.UUID, Size: &base.FileSize},
			{ArtifactUUID: layer.UUID},
			{Data: []byte("0123456789")},
		},
		Segments: []dto.CompositeSegment{seg(0, 0, 80), seg(2, 0, 4), seg(1, 10, 30), seg(2, 4, 6), seg(0, 90, 10)},
	}
}

func TestCreateCompositePinsPublishesAndIsIdempotent(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, f.site, 50, nil)
	p := CompositeParams{Site: f.site, Client: "c1", CDNBase: "https://dl.imoe.uk/"}

	resp, err := f.svc.Create(ctx, f.plan(base, layer), p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := "https://dl.imoe.uk/c/" + f.site + "/" + resp.ID + ".zip"; resp.URL != want {
		t.Errorf("url = %q, want %q", resp.URL, want)
	}
	if len(resp.Artifacts) != 2 || resp.Artifacts[0] != base.UUID || resp.Artifacts[1] != layer.UUID {
		t.Errorf("artifacts = %v, want [base layer]", resp.Artifacts)
	}
	if resp.SegmentCount != 5 || resp.InlineBytes != 10 || resp.Size != 130 {
		t.Errorf("counts = %+v", resp)
	}

	manifest, ok := f.store.objects["c/"+f.site+"/"+resp.ID]
	if !ok {
		t.Fatalf("no manifest object at c/%s/%s", f.site, resp.ID)
	}
	head, tail, _ := bytes.Cut(manifest, []byte("\n"))
	var h manifestHeader
	if err := json.Unmarshal(head, &h); err != nil {
		t.Fatal(err)
	}
	if string(tail) != "0123456789" || h.Sources[0].Key != base.FileKey || h.Sources[1].Key != layer.FileKey || h.Sources[2].Inline != 10 {
		t.Errorf("manifest sources = %+v tail %q", h.Sources, tail)
	}

	again, err := f.svc.Create(ctx, f.plan(base, layer), p)
	if err != nil || again.ID != resp.ID {
		t.Fatalf("replayed Create = %v, %v; want the same id %s", again, err, resp.ID)
	}
	var rows int64
	f.db.Model(&model.Composite{}).Where("site_key = ?", f.site).Count(&rows)
	if rows != 1 || f.store.puts != 1 {
		t.Errorf("replay made %d rows and %d puts, want 1 and 1", rows, f.store.puts)
	}

	listed, total, err := f.svc.List(ctx, f.site, "", 0, 10)
	if err != nil || total != 1 || listed[0].ID != resp.ID || listed[0].URL != "" || len(listed[0].Artifacts) != 2 {
		t.Fatalf("List = %+v %d %v", listed, total, err)
	}
}

func TestCompositeWithoutBLAKE3UsesItsIDAsETag(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, f.site, 50, nil)
	withSum := f.plan(base, layer)
	bare := f.plan(base, layer)
	bare.BLAKE3 = ""

	resp, err := f.svc.Create(ctx, bare, CompositeParams{Site: f.site})
	if err != nil {
		t.Fatalf("Create without blake3: %v", err)
	}
	if resp.BLAKE3 != "" {
		t.Errorf("blake3 = %q, want it absent", resp.BLAKE3)
	}
	head, _, _ := bytes.Cut(f.store.objects["c/"+f.site+"/"+resp.ID], []byte("\n"))
	var h manifestHeader
	if err := json.Unmarshal(head, &h); err != nil {
		t.Fatal(err)
	}
	if h.ETag != `"`+resp.ID+`"` {
		t.Errorf("etag = %s, want the quoted composite id", h.ETag)
	}
	if compositeID(f.site, withSum) == resp.ID {
		t.Error("a plan with and without blake3 got the same id")
	}
}

func TestCreateCompositeRefusesSourcesItCannotServe(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	layer := f.artifact(t, f.site, 50, nil)
	wrongSize := int64(99)

	cases := []struct {
		name string
		base func() *model.Artifact
		edit func(*dto.CreateCompositeRequest)
	}{
		{"another site's artifact", func() *model.Artifact { return f.artifact(t, "other"+f.site, 100, nil) }, nil},
		{"a private artifact", func() *model.Artifact {
			return f.artifact(t, f.site, 100, func(a *model.Artifact) { a.Public = false })
		}, nil},
		{"an unfinished upload", func() *model.Artifact {
			return f.artifact(t, f.site, 100, func(a *model.Artifact) { a.Status = model.StatusUploading })
		}, nil},
		{"a deleted artifact", func() *model.Artifact {
			a := f.artifact(t, f.site, 100, nil)
			f.db.Delete(a)
			return a
		}, nil},
		{"a source size the plan did not see", func() *model.Artifact { return f.artifact(t, f.site, 100, nil) },
			func(r *dto.CreateCompositeRequest) { r.Sources[0].Size = &wrongSize }},
		{"a source carrying both uuid and data", func() *model.Artifact { return f.artifact(t, f.site, 100, nil) },
			func(r *dto.CreateCompositeRequest) { r.Sources[1].Data = []byte("x") }},
		{"the same artifact twice", func() *model.Artifact { return f.artifact(t, f.site, 100, nil) },
			func(r *dto.CreateCompositeRequest) { r.Sources[1].ArtifactUUID = r.Sources[0].ArtifactUUID }},
		{"inline bytes over the cap", func() *model.Artifact { return f.artifact(t, f.site, 100, nil) },
			func(r *dto.CreateCompositeRequest) { r.Sources[2].Data = make([]byte, MaxCompositeInlineBytes+1) }},
		{"a content type that is not one", func() *model.Artifact { return f.artifact(t, f.site, 100, nil) },
			func(r *dto.CreateCompositeRequest) { r.ContentType = "zip file" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := f.plan(tc.base(), layer)
			if tc.edit != nil {
				tc.edit(&req)
			}
			_, err := f.svc.Create(ctx, req, CompositeParams{Site: f.site})
			if !stderrors.Is(err, ErrInvalidPlan) {
				t.Fatalf("Create = %v, want ErrInvalidPlan", err)
			}
		})
	}
	if len(f.store.objects) != 0 {
		t.Errorf("a refused plan still published %d manifests", len(f.store.objects))
	}
}

func TestDeletedArtifactStaysUntilItsCompositeGoes(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, f.site, 50, nil)
	resp, err := f.svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
	if err != nil {
		t.Fatal(err)
	}

	artifacts := repository.NewArtifactRepository(f.db)
	if ok, err := f.svc.DeleteArtifact(ctx, base.UUID, f.site); !ok || err != nil {
		t.Fatalf("site delete: %v %v", ok, err)
	}
	expired := func() bool {
		rows, err := artifacts.FindExpiredSoftDeleted(ctx, time.Now().Add(time.Hour), 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range rows {
			if a.UUID == base.UUID {
				return true
			}
		}
		return false
	}
	if expired() {
		t.Fatal("artifact-gc would purge an artifact a live composite still serves")
	}
	if got, err := f.svc.Get(ctx, resp.ID, f.site, ""); err != nil || got.Artifacts[0] != base.UUID {
		t.Fatalf("Get after the source was deleted = %+v %v", got, err)
	}

	if err := f.svc.Delete(ctx, resp.ID, f.site); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := f.store.objects["c/"+f.site+"/"+resp.ID]; ok {
		t.Error("manifest object survived the delete")
	}
	if !expired() {
		t.Error("artifact stayed pinned after its only composite was deleted")
	}
	if err := f.svc.Delete(ctx, resp.ID, f.site); !stderrors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestTakedownDeletesTheCompositesOverAnArtifact(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, f.site, 50, nil)
	resp, err := f.svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
	if err != nil {
		t.Fatal(err)
	}

	noStorage := NewCompositeService(repository.NewCompositeRepository(f.db), nil, nil)
	if _, _, err := noStorage.TakeDown(ctx, layer, ""); !stderrors.Is(err, ErrStorageUnconfigured) {
		t.Fatalf("TakeDown without storage = %v, want ErrStorageUnconfigured", err)
	}
	var live int64
	f.db.Model(&model.Artifact{}).Where("id = ?", layer.ID).Count(&live)
	if live != 1 {
		t.Fatal("a takedown that could not remove the composites still soft-deleted the artifact")
	}

	deleted, n, err := f.svc.TakeDown(ctx, layer, "")
	if err != nil || !deleted || n != 1 {
		t.Fatalf("TakeDown = %v, %d, %v; want true, 1", deleted, n, err)
	}
	if _, err := f.svc.Get(ctx, resp.ID, f.site, ""); !stderrors.Is(err, ErrNotFound) {
		t.Errorf("composite survived the takedown: %v", err)
	}
	if deleted, n, err := f.svc.TakeDown(ctx, layer, ""); err != nil || deleted || n != 0 {
		t.Errorf("second TakeDown = %v, %d, %v; want false, 0", deleted, n, err)
	}
}

func TestForeignSourcesFollowTheAllowList(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	other := "other" + f.site
	f.svc.foreignSources = map[string][]string{f.site: {other}}
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, other, 50, nil)

	resp, err := f.svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
	if err != nil {
		t.Fatalf("Create over a listed site's artifact: %v", err)
	}
	if resp.Artifacts[1] != layer.UUID {
		t.Errorf("artifacts = %v, want the foreign layer pinned second", resp.Artifacts)
	}

	cases := []struct {
		name        string
		site        string
		base, layer *model.Artifact
	}{
		{"the reverse direction", other, f.artifact(t, other, 100, nil), f.artifact(t, f.site, 50, nil)},
		{"a site off the list", f.site, base, f.artifact(t, "third"+f.site, 50, nil)},
		{"a private foreign artifact", f.site, base, f.artifact(t, other, 50, func(a *model.Artifact) { a.Public = false })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Create(ctx, f.plan(tc.base, tc.layer), CompositeParams{Site: tc.site})
			if !stderrors.Is(err, ErrInvalidPlan) {
				t.Fatalf("Create = %v, want ErrInvalidPlan", err)
			}
		})
	}
}

type failingDelete struct {
	*memStore
	fail bool
}

func (s *failingDelete) Delete(ctx context.Context, key string) error {
	if s.fail {
		s.fail = false
		return stderrors.New("storage unavailable")
	}
	return s.memStore.Delete(ctx, key)
}

func TestOwnerDeleteTakesDownOtherSitesCompositesOnly(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	other := "other" + f.site
	cleanup := &failingDelete{memStore: f.store}
	svc := NewCompositeService(repository.NewCompositeRepository(f.db), f.store, cleanup)
	svc.foreignSources = map[string][]string{f.site: {other}}

	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, other, 50, nil)
	merged, err := svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
	if err != nil {
		t.Fatal(err)
	}
	own, err := svc.Create(ctx, dto.CreateCompositeRequest{
		Filename: "p.zip", ContentType: "application/zip", Size: 50, BLAKE3: strings.Repeat("e", 64),
		Sources:  []dto.CompositeSourceInput{{ArtifactUUID: layer.UUID}},
		Segments: []dto.CompositeSegment{seg(0, 0, 50)},
	}, CompositeParams{Site: other})
	if err != nil {
		t.Fatal(err)
	}

	if ok, err := svc.DeleteArtifact(ctx, layer.UUID, f.site); ok || err != nil {
		t.Fatalf("a site deleted an artifact it does not own: %v, %v", ok, err)
	}

	cleanup.fail = true
	if _, err := svc.DeleteArtifact(ctx, layer.UUID, other); err == nil {
		t.Fatal("DeleteArtifact reported success though a manifest delete failed")
	}
	if _, err := svc.Get(ctx, merged.ID, f.site, ""); err != nil {
		t.Fatalf("the composite whose manifest delete failed lost its row: %v", err)
	}
	if ok, err := svc.DeleteArtifact(ctx, layer.UUID, other); !ok || err != nil {
		t.Fatalf("retried DeleteArtifact = %v, %v; want it to finish the cascade", ok, err)
	}
	if _, err := svc.Get(ctx, merged.ID, f.site, ""); !stderrors.Is(err, ErrNotFound) {
		t.Errorf("another site's composite survived the owner's delete: %v", err)
	}
	if _, ok := f.store.objects["c/"+f.site+"/"+merged.ID]; ok {
		t.Error("the foreign composite's manifest is still published")
	}
	if _, err := svc.Get(ctx, own.ID, other, ""); err != nil {
		t.Errorf("the owner's own composite went with its delete: %v", err)
	}
	if ok, err := svc.DeleteArtifact(ctx, layer.UUID, other); ok || err != nil {
		t.Errorf("third DeleteArtifact = %v, %v; want false, nil", ok, err)
	}
}

func waitForLockWait(t *testing.T, db *gorm.DB) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var n int64
		db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&n)
		if n > 0 {
			return
		}
	}
	t.Fatal("no session ever waited on the source row's lock")
}

func TestCreateLosesToADeleteThatCommitsFirst(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, f.site, 50, nil)

	tx := f.db.Begin()
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Delete(layer).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
		done <- err
	}()
	waitForLockWait(t, f.db)
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !stderrors.Is(err, ErrInvalidPlan) {
		t.Fatalf("Create racing a delete = %v, want ErrInvalidPlan", err)
	}
	var rows int64
	f.db.Model(&model.Composite{}).Where("site_key = ?", f.site).Count(&rows)
	if rows != 0 || len(f.store.objects) != 0 {
		t.Errorf("the losing create left %d rows and %d manifests", rows, len(f.store.objects))
	}
}

type gatedStore struct {
	*memStore
	entered chan struct{}
	release chan struct{}
}

func (g *gatedStore) Put(ctx context.Context, key string, body []byte, contentType string) error {
	close(g.entered)
	<-g.release
	return g.memStore.Put(ctx, key, body, contentType)
}

func TestTakedownWaitsForACreateHoldingItsSource(t *testing.T) {
	f := newCompositeFixture(t)
	ctx := context.Background()
	other := "other" + f.site
	store := &gatedStore{memStore: f.store, entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewCompositeService(repository.NewCompositeRepository(f.db), store, store)
	svc.foreignSources = map[string][]string{f.site: {other}}
	base := f.artifact(t, f.site, 100, nil)
	layer := f.artifact(t, other, 50, nil)

	created := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, f.plan(base, layer), CompositeParams{Site: f.site})
		created <- err
	}()
	<-store.entered

	type result struct {
		deleted bool
		removed int
		err     error
	}
	takedown := make(chan result, 1)
	go func() {
		d, n, err := svc.TakeDown(ctx, layer, other)
		takedown <- result{d, n, err}
	}()
	waitForLockWait(t, f.db)
	close(store.release)

	if err := <-created; err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r := <-takedown; r.err != nil || !r.deleted || r.removed != 1 {
		t.Fatalf("TakeDown = %+v; want it to wait out the create and remove its composite", r)
	}
	var rows int64
	f.db.Model(&model.Composite{}).Where("site_key = ?", f.site).Count(&rows)
	if rows != 0 || len(f.store.objects) != 0 {
		t.Errorf("a composite over the taken-down artifact survived: %d rows, %d manifests", rows, len(f.store.objects))
	}
}
