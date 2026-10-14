package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	MaxCompositeInlineBytes = 8 << 20
	compositeManifestType   = "application/vnd.nextmoe.composite"
	compositeKeyPrefix      = "c/"
)

var (
	ErrInvalidPlan         = stderrors.New("artifact: invalid composite plan")
	ErrStorageUnconfigured = stderrors.New("artifact: storage not configured")
)

func planErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidPlan, fmt.Sprintf(format, args...))
}

type ObjectStore interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Delete(ctx context.Context, key string) error
}

var foreignSourceSites = map[string][]string{"letmoe": {"moyu"}}

type CompositeService struct {
	repo           *repository.CompositeRepository
	store          ObjectStore
	cleanup        ObjectStore
	foreignSources map[string][]string
}

func NewCompositeService(repo *repository.CompositeRepository, store, cleanup ObjectStore) *CompositeService {
	return &CompositeService{repo: repo, store: store, cleanup: cleanup, foreignSources: foreignSourceSites}
}

type CompositeParams struct {
	Site    string
	Client  string
	CDNBase string
}

type resolvedSource struct {
	artifact *model.Artifact
	data     []byte
	size     int64
}

func (s *CompositeService) Create(ctx context.Context, req dto.CreateCompositeRequest, p CompositeParams) (*dto.CompositeResponse, error) {
	if _, _, err := mime.ParseMediaType(req.ContentType); err != nil {
		return nil, planErr("content_type %q is not a media type", req.ContentType)
	}
	sources, err := s.resolveSources(ctx, req.Sources, p.Site)
	if err != nil {
		return nil, err
	}
	if err := checkSegments(req, sources); err != nil {
		return nil, err
	}

	publicID := compositeID(p.Site, req)
	if existing, err := s.repo.Find(ctx, publicID, p.Site); err == nil {
		return s.respond(ctx, existing, p.CDNBase)
	} else if !stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	c := &model.Composite{
		PublicID:      publicID,
		SiteKey:       p.Site,
		CreatorClient: p.Client,
		Filename:      req.Filename,
		ContentType:   req.ContentType,
		Size:          req.Size,
		BLAKE3:        req.BLAKE3,
		SegmentCount:  len(req.Segments),
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
	}
	for i, src := range sources {
		if src.artifact != nil {
			c.Sources = append(c.Sources, model.CompositeSource{Position: i, ArtifactID: src.artifact.ID})
		} else {
			c.InlineBytes += src.size
		}
	}
	manifest, err := encodeManifest(c, req.Segments, sources)
	if err != nil {
		return nil, err
	}

	err = s.repo.Create(ctx, c, func() error {
		return s.store.Put(ctx, manifestKey(c), manifest, compositeManifestType)
	})
	if err != nil {
		if stderrors.Is(err, repository.ErrSourceGone) {
			return nil, planErr("a source artifact was deleted while the composite was being created")
		}
		if isUniqueViolation(err) {
			if existing, ferr := s.repo.Find(ctx, publicID, p.Site); ferr == nil {
				return s.respond(ctx, existing, p.CDNBase)
			}
		}
		return nil, err
	}
	return s.respond(ctx, c, p.CDNBase)
}

func (s *CompositeService) Get(ctx context.Context, publicID, site, cdnBase string) (*dto.CompositeResponse, error) {
	c, err := s.repo.Find(ctx, publicID, site)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.respond(ctx, c, cdnBase)
}

func (s *CompositeService) List(ctx context.Context, site, cdnBase string, offset, limit int) ([]dto.CompositeResponse, int64, error) {
	items, total, err := s.repo.ListBySite(ctx, site, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]uint, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	pinned, err := s.repo.PinnedUUIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.CompositeResponse, 0, len(items))
	for i := range items {
		out = append(out, toCompositeResponse(&items[i], pinned[items[i].ID], cdnBase))
	}
	return out, total, nil
}

func (s *CompositeService) Delete(ctx context.Context, publicID, site string) error {
	c, err := s.repo.Find(ctx, publicID, site)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.remove(ctx, c)
}

func (s *CompositeService) DeleteArtifact(ctx context.Context, artifactUUID, site string) (bool, error) {
	a, err := s.repo.FindArtifact(ctx, artifactUUID, site)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	deleted, removed, err := s.TakeDown(ctx, a, site)
	return deleted || removed > 0, err
}

// TakeDown soft-deletes the artifact before it looks up the composites over
// it, pairing with the FOR SHARE in CompositeRepository.Create: a create racing
// this either sees the artifact gone or commits before the lookup. Looking up
// first lets a create land in between and pin a taken-down artifact for good.
func (s *CompositeService) TakeDown(ctx context.Context, a *model.Artifact, spareSite string) (bool, int, error) {
	if s.cleanup == nil {
		items, err := s.repo.Referencing(ctx, a.ID, spareSite)
		if err != nil {
			return false, 0, err
		}
		if len(items) > 0 {
			return false, 0, ErrStorageUnconfigured
		}
	}
	deleted, err := s.repo.SoftDeleteArtifact(ctx, a.ID)
	if err != nil {
		return false, 0, err
	}
	items, err := s.repo.Referencing(ctx, a.ID, spareSite)
	if err != nil {
		return deleted, 0, err
	}
	for i := range items {
		if err := s.remove(ctx, &items[i]); err != nil {
			return deleted, i, err
		}
	}
	return deleted, len(items), nil
}

func (s *CompositeService) remove(ctx context.Context, c *model.Composite) error {
	if s.cleanup == nil {
		return ErrStorageUnconfigured
	}
	if err := s.cleanup.Delete(ctx, manifestKey(c)); err != nil {
		return err
	}
	return s.repo.Delete(ctx, c.ID)
}

func (s *CompositeService) respond(ctx context.Context, c *model.Composite, cdnBase string) (*dto.CompositeResponse, error) {
	pinned, err := s.repo.PinnedUUIDs(ctx, []uint{c.ID})
	if err != nil {
		return nil, err
	}
	resp := toCompositeResponse(c, pinned[c.ID], cdnBase)
	return &resp, nil
}

func (s *CompositeService) resolveSources(ctx context.Context, in []dto.CompositeSourceInput, site string) ([]resolvedSource, error) {
	var (
		uuids       []string
		inlineTotal int64
		seen        = map[string]bool{}
	)
	for i, src := range in {
		hasUUID, hasData := src.ArtifactUUID != "", len(src.Data) > 0
		if hasUUID == hasData {
			return nil, planErr("sources[%d] must carry exactly one of artifact_uuid and data", i)
		}
		if hasData {
			inlineTotal += int64(len(src.Data))
			continue
		}
		if _, err := uuid.Parse(src.ArtifactUUID); err != nil {
			return nil, planErr("sources[%d].artifact_uuid is not a UUID", i)
		}
		if seen[src.ArtifactUUID] {
			return nil, planErr("sources[%d] repeats artifact %s", i, src.ArtifactUUID)
		}
		seen[src.ArtifactUUID] = true
		uuids = append(uuids, src.ArtifactUUID)
	}
	if inlineTotal > MaxCompositeInlineBytes {
		return nil, planErr("inline sources total %d bytes, over the %d-byte cap; upload the larger blob as an artifact", inlineTotal, MaxCompositeInlineBytes)
	}

	byUUID := map[string]*model.Artifact{}
	if len(uuids) > 0 {
		found, err := s.repo.LiveArtifacts(ctx, uuids)
		if err != nil {
			return nil, err
		}
		for i := range found {
			byUUID[found[i].UUID] = &found[i]
		}
	}

	out := make([]resolvedSource, len(in))
	for i, src := range in {
		if len(src.Data) > 0 {
			out[i] = resolvedSource{data: src.Data, size: int64(len(src.Data))}
		} else {
			a := byUUID[src.ArtifactUUID]
			switch {
			case a == nil || !s.sourceAllowed(site, a.SiteKey):
				return nil, planErr("sources[%d]: artifact %s not found on this site", i, src.ArtifactUUID)
			case !a.IsReady():
				return nil, planErr("sources[%d]: artifact %s has not finished uploading", i, src.ArtifactUUID)
			case !a.Public:
				return nil, planErr("sources[%d]: artifact %s is not public, and a composite is served on a public URL", i, src.ArtifactUUID)
			}
			out[i] = resolvedSource{artifact: a, size: a.FileSize}
		}
		if src.Size != nil && *src.Size != out[i].size {
			return nil, planErr("sources[%d] is %d bytes, not the %d the plan was made against", i, out[i].size, *src.Size)
		}
	}
	return out, nil
}

func (s *CompositeService) sourceAllowed(site, source string) bool {
	return source == site || slices.Contains(s.foreignSources[site], source)
}

func checkSegments(req dto.CreateCompositeRequest, sources []resolvedSource) error {
	used := make([]bool, len(sources))
	var total int64
	for i, seg := range req.Segments {
		if seg.Source < 0 || seg.Source >= len(sources) {
			return planErr("segments[%d].source %d is out of range", i, seg.Source)
		}
		size := sources[seg.Source].size
		if seg.Offset < 0 || seg.Length <= 0 || seg.Offset > size || seg.Length > size-seg.Offset {
			return planErr("segments[%d] [%d, +%d) falls outside source %d (%d bytes)", i, seg.Offset, seg.Length, seg.Source, size)
		}
		total += seg.Length
		if total > req.Size {
			return planErr("segments add up to more than size %d", req.Size)
		}
		used[seg.Source] = true
	}
	if total != req.Size {
		return planErr("segments add up to %d bytes, size says %d", total, req.Size)
	}
	for i, ok := range used {
		if !ok {
			return planErr("sources[%d] is referenced by no segment", i)
		}
	}
	return nil
}

func compositeID(site string, req dto.CreateCompositeRequest) string {
	type canonSource struct {
		Artifact string `json:"a,omitempty"`
		Data     string `json:"d,omitempty"`
	}
	canon := struct {
		Site        string                 `json:"site"`
		Filename    string                 `json:"filename"`
		ContentType string                 `json:"content_type"`
		Size        int64                  `json:"size"`
		BLAKE3      string                 `json:"blake3"`
		Sources     []canonSource          `json:"sources"`
		Segments    []dto.CompositeSegment `json:"segments"`
	}{
		Site: site, Filename: req.Filename, ContentType: req.ContentType,
		Size: req.Size, BLAKE3: req.BLAKE3, Segments: req.Segments,
	}
	for _, src := range req.Sources {
		if len(src.Data) > 0 {
			sum := sha256.Sum256(src.Data)
			canon.Sources = append(canon.Sources, canonSource{Data: hex.EncodeToString(sum[:])})
		} else {
			canon.Sources = append(canon.Sources, canonSource{Artifact: strings.ToLower(src.ArtifactUUID)})
		}
	}
	b, _ := json.Marshal(canon)
	sum := sha256.Sum256(append([]byte("nextmoe composite v1\n"), b...))
	return hex.EncodeToString(sum[:16])
}

type manifestSource struct {
	Key    string `json:"key,omitempty"`
	Inline int64  `json:"inline,omitempty"`
}

type manifestHeader struct {
	V                  int              `json:"v"`
	Size               int64            `json:"size"`
	ContentType        string           `json:"content_type"`
	ContentDisposition string           `json:"content_disposition"`
	ETag               string           `json:"etag"`
	LastModified       string           `json:"last_modified"`
	Sources            []manifestSource `json:"sources"`
	Segments           [][3]int64       `json:"segments"`
}

// encodeManifest writes the object the dl.imoe.uk composite Worker serves
// from: one line of JSON, a newline, then every inline source's bytes in
// source order. The format is shared with workers/dl-composite/src/manifest.ts.
func encodeManifest(c *model.Composite, segments []dto.CompositeSegment, sources []resolvedSource) ([]byte, error) {
	h := manifestHeader{
		V:                  1,
		Size:               c.Size,
		ContentType:        c.ContentType,
		ContentDisposition: storage.ContentDisposition(c.Filename),
		ETag:               compositeETag(c),
		LastModified:       c.CreatedAt.UTC().Format(http.TimeFormat),
		Segments:           make([][3]int64, len(segments)),
	}
	var tail []byte
	for _, src := range sources {
		if src.artifact != nil {
			h.Sources = append(h.Sources, manifestSource{Key: src.artifact.FileKey})
		} else {
			h.Sources = append(h.Sources, manifestSource{Inline: src.size})
			tail = append(tail, src.data...)
		}
	}
	for i, seg := range segments {
		h.Segments[i] = [3]int64{int64(seg.Source), seg.Offset, seg.Length}
	}
	head, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(head)+1+len(tail))
	out = append(out, head...)
	out = append(out, '\n')
	return append(out, tail...), nil
}

func compositeETag(c *model.Composite) string {
	if c.BLAKE3 == "" {
		return `"` + c.PublicID + `"`
	}
	return `"` + c.BLAKE3 + `"`
}

func manifestKey(c *model.Composite) string {
	return compositeKeyPrefix + c.SiteKey + "/" + c.PublicID
}

func compositeURL(c *model.Composite, cdnBase string) string {
	if cdnBase == "" {
		return ""
	}
	return strings.TrimRight(cdnBase, "/") + "/" + manifestKey(c) + extForKey(c.Filename)
}

func toCompositeResponse(c *model.Composite, pinned []string, cdnBase string) dto.CompositeResponse {
	if pinned == nil {
		pinned = []string{}
	}
	return dto.CompositeResponse{
		ID:           c.PublicID,
		SiteKey:      c.SiteKey,
		URL:          compositeURL(c, cdnBase),
		Filename:     c.Filename,
		ContentType:  c.ContentType,
		Size:         c.Size,
		BLAKE3:       c.BLAKE3,
		Artifacts:    pinned,
		SegmentCount: c.SegmentCount,
		InlineBytes:  c.InlineBytes,
		CreatedAt:    c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return stderrors.As(err, &pgErr) && pgErr.Code == "23505"
}
