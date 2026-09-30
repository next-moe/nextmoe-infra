package worker

import (
	"context"
	"errors"
	"io"
	"os"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbolicate"
)

type Lookup interface {
	DartSymbolsByBuildID(ctx context.Context, appID int64, buildID string) (*model.SymbolFile, error)
	ObfuscationMapForUpload(ctx context.Context, uploadID int64) (*model.SymbolFile, error)
	R8MappingForVersion(ctx context.Context, appID int64, version string) (*model.SymbolFile, error)
	EngineSymbolByBuildID(ctx context.Context, buildID string) (*model.EngineSymbol, error)
	Materialize(ctx context.Context, sha256 string) (string, error)
	TextStart(path string) (uint64, error)
}

type StoreLookup struct {
	Store *store.Store
	Cache *SymbolCache
}

func (l StoreLookup) DartSymbolsByBuildID(ctx context.Context, appID int64, buildID string) (*model.SymbolFile, error) {
	return l.Store.DartSymbolsByBuildID(ctx, appID, buildID)
}

func (l StoreLookup) ObfuscationMapForUpload(ctx context.Context, uploadID int64) (*model.SymbolFile, error) {
	return l.Store.ObfuscationMapForUpload(ctx, uploadID)
}

func (l StoreLookup) R8MappingForVersion(ctx context.Context, appID int64, version string) (*model.SymbolFile, error) {
	return l.Store.R8MappingForVersion(ctx, appID, version)
}

func (l StoreLookup) EngineSymbolByBuildID(ctx context.Context, buildID string) (*model.EngineSymbol, error) {
	return l.Store.EngineSymbolByBuildID(ctx, buildID)
}

func (l StoreLookup) Materialize(ctx context.Context, sha256 string) (string, error) {
	return l.Cache.Get(ctx, sha256, func(ctx context.Context) (io.ReadCloser, error) {
		return l.Store.OpenBlob(ctx, sha256)
	})
}

func (l StoreLookup) TextStart(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return symbolicate.TextStart(f)
}

func missing(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
