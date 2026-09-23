package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/gorm/clause"
)

const (
	assetPrefix    = "decorations/"
	immutableCache = "public, max-age=31536000, immutable"
)

type assetMeta struct {
	contentType string
	ext         string
	width       int
	height      int
	animated    bool
}

func invalidAsset(msg string) error {
	return errors.New(errors.ErrShopInvalidAsset, msg)
}

func inspectAsset(spec kindSpec, data []byte) (assetMeta, error) {
	var m assetMeta
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		if isAPNG(data) {
			return m, invalidAsset("动图请上传动态 WebP,静态图只接受普通 PNG")
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return m, invalidAsset("PNG 无法解码")
		}
		m = assetMeta{contentType: "image/png", ext: ".png", width: cfg.Width, height: cfg.Height}
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return m, invalidAsset("JPEG 无法解码")
		}
		m = assetMeta{contentType: "image/jpeg", ext: ".jpg", width: cfg.Width, height: cfg.Height}
	case len(data) >= 30 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if string(data[12:16]) != "VP8X" || data[20]&0x02 == 0 {
			return m, invalidAsset("WebP 只接受动图")
		}
		if spec.animated.alpha && data[20]&0x10 == 0 {
			return m, invalidAsset(spec.label + "的动图必须带透明通道")
		}
		m = assetMeta{
			contentType: "image/webp", ext: ".webp", animated: true,
			width:  1 + (int(data[24]) | int(data[25])<<8 | int(data[26])<<16),
			height: 1 + (int(data[27]) | int(data[28])<<8 | int(data[29])<<16),
		}
	default:
		return m, invalidAsset("只接受 PNG、JPEG(静态)或动态 WebP")
	}

	rule := spec.static
	if m.animated {
		rule = spec.animated
	} else if !rule.accepts(m.contentType) {
		return m, invalidAsset(spec.label + "的静态图不接受这种格式")
	}
	if len(data) > rule.maxBytes {
		what := "静态图"
		if m.animated {
			what = "动图"
		}
		return m, invalidAsset(fmt.Sprintf("%s的%s不能超过 %s", spec.label, what, humanBytes(rule.maxBytes)))
	}
	if err := rule.fits(m.width, m.height); err != nil {
		return m, err
	}
	if rule.pixels != nil {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return m, invalidAsset("图片无法解码")
		}
		if err := rule.pixels(img); err != nil {
			return m, err
		}
	}
	return m, nil
}

func humanBytes(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}

func isAPNG(data []byte) bool {
	for off := 8; off+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[off : off+4]))
		switch string(data[off+4 : off+8]) {
		case "acTL":
			return true
		case "IDAT":
			return false
		}
		off += 12 + n
	}
	return false
}

func (s *Shop) UploadAsset(ctx context.Context, kind string, data []byte, uploadedBy uint) (*AssetView, error) {
	if s.store == nil {
		return nil, errors.NewWithCode(errors.ErrShopStorageUnavailable)
	}
	spec, err := specOf(kind)
	if err != nil {
		return nil, err
	}
	meta, err := inspectAsset(spec, data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	db := s.db.WithContext(ctx)

	var existing []model.Asset
	if err := db.Where("hash = ?", hash).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return &AssetView{Asset: existing[0], URL: s.assetURL(existing[0].Key)}, nil
	}
	a := model.Asset{
		Hash: hash, Key: assetPrefix + hash + meta.ext, ContentType: meta.contentType,
		Width: meta.width, Height: meta.height, Animated: meta.animated,
		Bytes: len(data), UploadedBy: uploadedBy,
	}
	if err := s.store.PutWithCacheControl(ctx, a.Key, data, a.ContentType, immutableCache); err != nil {
		return nil, err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; err != nil {
		return nil, err
	}
	return &AssetView{Asset: a, URL: s.assetURL(a.Key)}, nil
}

func (s *Shop) ListAssets(ctx context.Context) ([]AssetView, error) {
	var rows []model.Asset
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AssetView, len(rows))
	for i, a := range rows {
		out[i] = AssetView{Asset: a, URL: s.assetURL(a.Key)}
	}
	return out, nil
}

type AssetView struct {
	model.Asset
	URL string `json:"url"`
}
