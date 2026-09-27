package service

import (
	"context"
	"io"
	"time"

	"api/internal/platform/chat/dto"
	"api/pkg/imageclient"
)

const (
	uploadsPerMinute = 20
	pingBatch        = 1000
)

type ImageResult struct {
	Media dto.Media
}

func (s *Service) UploadImage(ctx context.Context, a Actor, filename string, r io.Reader) (*ImageResult, error) {
	if s.images == nil {
		return nil, ErrImagesDisabled
	}
	if err := s.allow(ctx, "upload:"+itoa(a.UserID), uploadsPerMinute, time.Minute); err != nil {
		return nil, err
	}
	img, err := s.images.Upload(ctx, r, filename, "chat:"+itoa(a.UserID))
	if err != nil {
		return nil, err
	}
	return &ImageResult{Media: dto.Media{Type: "photo", ImageHash: img.Hash, Width: img.Width, Height: img.Height,
		Thumbhash: img.Thumbhash, URL: s.imageURL(img.Hash)}}, nil
}

func (s *Service) imageURL(hash string) string { return imageclient.MainURL(s.imageBase, hash, "webp") }

// Reference pings are scoped to the image client that uploaded, so only
// chat's own client can keep chat's photos out of the image GC.
func (s *Service) PingImages(ctx context.Context) (int64, int, error) {
	if s.images == nil {
		return 0, 0, nil
	}
	var hashes []string
	if err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT media->>'image_hash' FROM chat_message
		 WHERE media IS NOT NULL AND deleted_at IS NULL AND media->>'image_hash' IS NOT NULL`).
		Scan(&hashes).Error; err != nil {
		return 0, 0, err
	}
	var updated int64
	var missing int
	for start := 0; start < len(hashes); start += pingBatch {
		end := min(start+pingBatch, len(hashes))
		u, m, err := s.images.Ping(ctx, hashes[start:end])
		if err != nil {
			return updated, missing, err
		}
		updated += u
		missing += m
	}
	return updated, missing, nil
}
