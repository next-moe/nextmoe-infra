package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api/internal/platform/telemetry/symbols"
)

func withBlobs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	st.SetBlobStore(symbols.NewFSStore(dir))
	t.Cleanup(func() { st.SetBlobStore(nil) })
	return dir
}

func seedApp(t *testing.T) int64 {
	t.Helper()
	row, err := st.CreateApp(context.Background(), "kungal-app", "KUN")
	if err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func incoming(t *testing.T, dir, name, kind, arch, buildID string, data []byte) symbols.IncomingFile {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return symbols.IncomingFile{
		FileName: name,
		Kind:     kind,
		Arch:     arch,
		BuildID:  buildID,
		SHA256:   hex.EncodeToString(sum[:]),
		Size:     int64(len(data)),
		Path:     path,
	}
}

func TestSymbolUploadPersist(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	elf := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "aa", []byte("elf-a"))
	id, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf})
	if err != nil {
		t.Fatal(err)
	}
	if !created || id == 0 {
		t.Fatalf("created=%v id=%d", created, id)
	}
	rc, err := st.OpenBlob(ctx, elf.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "elf-a" {
		t.Fatalf("blob=%q", got)
	}
}

func TestSymbolIdempotency(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	elf := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "aa", []byte("elf-a"))
	mp := incoming(t, dir, symbols.FileR8Mapping, symbols.KindR8Mapping, "", "", []byte("com.Foo -> a.b:\n"))
	id1, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf, mp})
	if err != nil || !created {
		t.Fatalf("first: id=%d created=%v err=%v", id1, created, err)
	}
	id2, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf, mp})
	if err != nil || created || id2 != id1 {
		t.Fatalf("second: id=%d created=%v err=%v want %d", id2, created, err, id1)
	}
	var nUp, nFile, nBlob int64
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_upload`).Scan(&nUp)
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_file`).Scan(&nFile)
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_blob`).Scan(&nBlob)
	if nUp != 1 || nFile != 2 || nBlob != 2 {
		t.Fatalf("counts upload=%d file=%d blob=%d", nUp, nFile, nBlob)
	}
	elf2 := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "bb", []byte("elf-b"))
	id3, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf2, mp})
	if err != nil || !created || id3 == id1 {
		t.Fatalf("changed: id=%d created=%v err=%v", id3, created, err)
	}
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_upload`).Scan(&nUp)
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_file`).Scan(&nFile)
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_blob`).Scan(&nBlob)
	if nUp != 2 || nFile != 4 || nBlob != 3 {
		t.Fatalf("after change upload=%d file=%d blob=%d", nUp, nFile, nBlob)
	}
}

func TestEngineRevisionKnownness(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	elf := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "aa", []byte("elf-a"))
	rev := "0123456789abcdef0123456789abcdef01234567"
	id1, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf})
	if err != nil || !created {
		t.Fatalf("no engine: %v %v", created, err)
	}
	id2, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", rev, []symbols.IncomingFile{elf})
	if err != nil || !created || id2 == id1 {
		t.Fatalf("new engine: id=%d created=%v err=%v", id2, created, err)
	}
	id3, created, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", rev, []symbols.IncomingFile{elf})
	if err != nil || created || id3 != id2 {
		t.Fatalf("same engine: id=%d created=%v err=%v", id3, created, err)
	}
}

func TestDartSymbolsByBuildID(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	a := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "deadbeef", []byte("elf-1"))
	b := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "deadbeef", []byte("elf-2"))
	_, _, _ = st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{a})
	_, _, _ = st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{b})
	row, err := st.DartSymbolsByBuildID(ctx, appID, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if row.SHA256 != b.SHA256 {
		t.Fatalf("got %s want newest %s", row.SHA256, b.SHA256)
	}
}

func TestObfuscationMapSameUpload(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	elf1 := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "aaa", []byte("elf-1"))
	map1 := incoming(t, dir, symbols.FileObfuscation, symbols.KindDartObfuscationMap, "", "", []byte(`["a","one"]`))
	elf2 := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "bbb", []byte("elf-2"))
	map2 := incoming(t, dir, symbols.FileObfuscation, symbols.KindDartObfuscationMap, "", "", []byte(`["a","two"]`))
	id1, _, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf1, map1})
	if err != nil {
		t.Fatal(err)
	}
	id2, _, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{elf2, map2})
	if err != nil {
		t.Fatal(err)
	}
	sym, err := st.DartSymbolsByBuildID(ctx, appID, "aaa")
	if err != nil {
		t.Fatal(err)
	}
	if sym.UploadID != id1 {
		t.Fatalf("upload=%d want %d", sym.UploadID, id1)
	}
	m, err := st.ObfuscationMapForUpload(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if m.SHA256 != map1.SHA256 {
		t.Fatalf("map sha=%s", m.SHA256)
	}
	m2, err := st.ObfuscationMapForUpload(ctx, id2)
	if err != nil {
		t.Fatal(err)
	}
	if m2.SHA256 != map2.SHA256 {
		t.Fatalf("map2 sha=%s", m2.SHA256)
	}
}

func TestR8MappingNewest(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	m1 := incoming(t, dir, symbols.FileR8Mapping, symbols.KindR8Mapping, "", "", []byte("com.Foo -> a.b:\n"))
	m2 := incoming(t, dir, symbols.FileR8Mapping, symbols.KindR8Mapping, "", "", []byte("com.Bar -> c.d:\n"))
	_, _, _ = st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{m1})
	_, _, _ = st.IngestSymbolUpload(ctx, appID, "1.0.0", "", []symbols.IncomingFile{m2})
	row, err := st.R8MappingForVersion(ctx, appID, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if row.SHA256 != m2.SHA256 {
		t.Fatalf("got %s", row.SHA256)
	}
}

func TestEngineSymbolRows(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	rev := "0123456789abcdef0123456789abcdef01234567"
	elf := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "aa", []byte("elf"))
	_, _, err := st.IngestSymbolUpload(ctx, appID, "1.0.0", rev, []symbols.IncomingFile{elf})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row, err := st.NextPendingEngine(ctx, now)
	if err != nil || row == nil {
		t.Fatalf("pending: %v %+v", err, row)
	}
	row.Status = symbols.StatusReady
	row.BuildID = "engbuild"
	row.SHA256 = "abc"
	row.Size = 12
	row.LastError = ""
	row.UpdatedAt = now
	if err := st.SaveEngineSymbol(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := st.EngineSymbolByBuildID(ctx, "engbuild")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != symbols.StatusReady || got.Variant != row.Variant {
		t.Fatalf("%+v", got)
	}
	other, err := st.NextPendingEngine(ctx, now)
	if err != nil || other == nil {
		t.Fatalf("other pending: %v", err)
	}
	other.Status = symbols.StatusFailed
	other.LastError = "not found"
	other.Attempts = 1
	other.UpdatedAt = now
	if err := st.SaveEngineSymbol(ctx, other); err != nil {
		t.Fatal(err)
	}
	again, err := st.NextPendingEngine(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("still pending %+v", again)
	}
}

func TestSymbolRetention(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	oldKeep := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "keep", []byte("keep-elf"))
	shared := incoming(t, dir, symbols.FileR8Mapping, symbols.KindR8Mapping, "", "", []byte("com.Foo -> a.b:\n"))
	keepID, _, err := st.IngestSymbolUpload(ctx, appID, "keep-ver", "", []symbols.IncomingFile{oldKeep, shared})
	if err != nil {
		t.Fatal(err)
	}

	dropElf := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "drop", []byte("drop-elf"))
	dropID, _, err := st.IngestSymbolUpload(ctx, appID, "drop-ver", "0123456789abcdef0123456789abcdef01234567", []symbols.IncomingFile{dropElf})
	if err != nil {
		t.Fatal(err)
	}

	oldShared := incoming(t, dir, symbols.FileARM64Symbols, symbols.KindDartSymbols, symbols.ArchARM64, "oldshare", []byte("old-elf"))
	_, _, err = st.IngestSymbolUpload(ctx, appID, "share-ver", "", []symbols.IncomingFile{oldShared, shared})
	if err != nil {
		t.Fatal(err)
	}

	oldTime := now.AddDate(0, 0, -400)
	if err := testDB.Exec(`UPDATE telemetry_symbol_upload SET created_at = ? WHERE service_version IN ('drop-ver','keep-ver','share-ver')`, oldTime).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`INSERT INTO telemetry_daily_metric (
		app_id, service_version, environment, day, sessions, crashed_sessions, anr_sessions,
		unhandled_sessions, abnormal_sessions, exceptions, crash_java, crash_native, crash_anr,
		startup_count, jank_frames_over, jank_frames_total, updated_at)
		VALUES (?, 'keep-ver', 'direct', ?, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, ?)`,
		appID, now.AddDate(0, 0, -10).Format("2006-01-02"), now).Error; err != nil {
		t.Fatal(err)
	}

	if err := st.PurgeExpiredSymbols(ctx, now); err != nil {
		t.Fatal(err)
	}

	var n int64
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_upload WHERE id = ?`, keepID).Scan(&n)
	if n != 1 {
		t.Fatal("kept upload deleted")
	}
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_symbol_upload WHERE id = ?`, dropID).Scan(&n)
	if n != 0 {
		t.Fatal("drop upload remained")
	}
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_blob WHERE sha256 = ?`, dropElf.SHA256).Scan(&n)
	if n != 0 {
		t.Fatal("orphan blob row remained")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(symbols.BlobKey(dropElf.SHA256)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan blob object: %v", err)
	}
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_blob WHERE sha256 = ?`, shared.SHA256).Scan(&n)
	if n != 1 {
		t.Fatal("shared blob deleted")
	}
	_ = testDB.Raw(`SELECT COUNT(*) FROM telemetry_engine_symbol`).Scan(&n)
	if n != 0 {
		t.Fatalf("engine rows remained %d", n)
	}
}

func TestTokenLookup(t *testing.T) {
	truncate(t)
	appID := seedApp(t)
	ctx := context.Background()
	tok, err := st.RotateSymbolsToken(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("token len=%d", len(tok))
	}
	hash := symbols.HashToken(tok)
	got, err := st.LookupBySymbolsTokenHash(ctx, hash)
	if err != nil || got == nil || got.ID != appID {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	var stored *string
	if err := testDB.Raw(`SELECT symbols_token_hash FROM telemetry_app WHERE id = ?`, appID).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored == nil || *stored != hash {
		t.Fatalf("stored=%v", stored)
	}
	if *stored == tok {
		t.Fatal("plaintext stored")
	}
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_app WHERE ingest_key = ? OR symbols_token_hash = ?`, tok, tok).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("plaintext found in app row")
	}
}
