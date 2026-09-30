package symbols

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"api/internal/platform/telemetry/model"
)

const (
	maxEngineZip = 1 << 30
	engineGETTO  = 10 * time.Minute
)

type Clock interface {
	Now() time.Time
}

type sysClock struct{}

func (sysClock) Now() time.Time { return time.Now().UTC() }

type EngineRepo interface {
	NextPendingEngine(ctx context.Context, now time.Time) (*model.EngineSymbol, error)
	SaveEngineSymbol(ctx context.Context, row *model.EngineSymbol) error
	EnsureBlobRow(ctx context.Context, sha256 string, size int64) error
}

type Fetcher struct {
	repo   EngineRepo
	blobs  BlobStore
	base   string
	client *http.Client
	clock  Clock
	log    *slog.Logger
}

func NewFetcher(repo EngineRepo, blobs BlobStore, baseURL string, log *slog.Logger) *Fetcher {
	if log == nil {
		log = slog.Default()
	}
	return &Fetcher{
		repo:  repo,
		blobs: blobs,
		base:  strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: engineGETTO,
		},
		clock: sysClock{},
		log:   log,
	}
}

func (f *Fetcher) Cycle(ctx context.Context) error {
	row, err := f.repo.NextPendingEngine(ctx, f.clock.Now())
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	return f.fetch(ctx, row)
}

func (f *Fetcher) fetch(ctx context.Context, row *model.EngineSymbol) error {
	want, ok := variantMachine(row.Variant)
	if !ok {
		return f.fail(ctx, row, "unknown variant", false)
	}
	url := f.base + "/" + row.EngineRevision + "/" + row.Variant + "/symbols.zip"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return f.retry(ctx, row, err.Error())
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return f.retry(ctx, row, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return f.fail(ctx, row, "not found", false)
	}
	if resp.StatusCode != http.StatusOK {
		return f.retry(ctx, row, fmt.Sprintf("http %d", resp.StatusCode))
	}
	tmp, err := os.CreateTemp("", "eng-zip-*")
	if err != nil {
		return f.retry(ctx, row, err.Error())
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxEngineZip+1))
	closeErr := tmp.Close()
	if err != nil {
		return f.retry(ctx, row, err.Error())
	}
	if closeErr != nil {
		return f.retry(ctx, row, closeErr.Error())
	}
	if n > maxEngineZip {
		return f.fail(ctx, row, "zip too large", false)
	}
	zr, err := zip.OpenReader(tmpName)
	if err != nil {
		return f.retry(ctx, row, "bad zip")
	}
	defer zr.Close()
	var so *zip.File
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) == "libflutter.so" && !strings.HasSuffix(zf.Name, "/") {
			so = zf
			break
		}
	}
	if so == nil {
		return f.retry(ctx, row, "no libflutter.so")
	}
	rc, err := so.Open()
	if err != nil {
		return f.retry(ctx, row, err.Error())
	}
	extracted, err := os.CreateTemp("", "eng-so-*")
	if err != nil {
		_ = rc.Close()
		return f.retry(ctx, row, err.Error())
	}
	extName := extracted.Name()
	defer func() { _ = os.Remove(extName) }()
	_, copyErr := io.Copy(extracted, io.LimitReader(rc, maxEngineZip+1))
	_ = rc.Close()
	if copyErr != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, copyErr.Error())
	}
	if _, err := extracted.Seek(0, io.SeekStart); err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	buildID, machine, err := BuildIDFromELF(extracted)
	if err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	if machine != want {
		_ = extracted.Close()
		return f.fail(ctx, row, "machine mismatch", false)
	}
	if _, err := extracted.Seek(0, io.SeekStart); err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	hash := sha256.New()
	size, err := io.Copy(hash, extracted)
	if err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	if _, err := extracted.Seek(0, io.SeekStart); err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	if err := f.blobs.Put(ctx, BlobKey(sha), extracted); err != nil {
		_ = extracted.Close()
		return f.retry(ctx, row, err.Error())
	}
	_ = extracted.Close()
	if err := f.repo.EnsureBlobRow(ctx, sha, size); err != nil {
		return f.retry(ctx, row, err.Error())
	}
	now := f.clock.Now()
	row.Status = StatusReady
	row.BuildID = buildID
	row.SHA256 = sha
	row.Size = size
	row.LastError = ""
	row.UpdatedAt = now
	return f.repo.SaveEngineSymbol(ctx, row)
}

func variantMachine(variant string) (elf.Machine, bool) {
	switch variant {
	case VariantARM64Release:
		return elf.EM_AARCH64, true
	case VariantARMRelease:
		return elf.EM_ARM, true
	default:
		return 0, false
	}
}

func (f *Fetcher) fail(ctx context.Context, row *model.EngineSymbol, last string, retry bool) error {
	now := f.clock.Now()
	row.Attempts++
	row.LastError = last
	row.UpdatedAt = now
	if !retry {
		row.Status = StatusFailed
		return f.repo.SaveEngineSymbol(ctx, row)
	}
	if row.Attempts >= 10 {
		row.Status = StatusFailed
		return f.repo.SaveEngineSymbol(ctx, row)
	}
	row.Status = StatusPending
	row.NextAttemptAt = now.Add(engineBackoff(row.Attempts))
	return f.repo.SaveEngineSymbol(ctx, row)
}

func (f *Fetcher) retry(ctx context.Context, row *model.EngineSymbol, last string) error {
	return f.fail(ctx, row, last, true)
}

func engineBackoff(attempts int) time.Duration {
	mins := 1 << attempts
	max := 24 * 60
	if mins > max {
		mins = max
	}
	return time.Duration(mins) * time.Minute
}
