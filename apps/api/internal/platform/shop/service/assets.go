package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image/png"

	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/gorm/clause"
)

const (
	maxStaticBytes   = 512 << 10
	maxAnimatedBytes = 2 << 20
	minFrameSide     = 192
	maxFrameSide     = 1024
	assetPrefix      = "decorations/"
	immutableCache   = "public, max-age=31536000, immutable"
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

func inspectAsset(data []byte) (assetMeta, error) {
	var m assetMeta
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		if len(data) > maxStaticBytes {
			return m, invalidAsset("静态图不能超过 512 KB")
		}
		if isAPNG(data) {
			return m, invalidAsset("动图请上传动态 WebP,静态图只接受普通 PNG")
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return m, invalidAsset("PNG 无法解码")
		}
		b := img.Bounds()
		m = assetMeta{contentType: "image/png", ext: ".png", width: b.Dx(), height: b.Dy()}
		for _, p := range [][2]int{
			{b.Min.X, b.Min.Y}, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1},
			{b.Min.X + b.Dx()/2, b.Min.Y + b.Dy()/2},
		} {
			if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0 {
				return m, invalidAsset("头像框的四角和中心必须是透明的")
			}
		}
	case len(data) >= 30 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if len(data) > maxAnimatedBytes {
			return m, invalidAsset("动图不能超过 2 MB")
		}
		if string(data[12:16]) != "VP8X" {
			return m, invalidAsset("WebP 必须是带透明通道的动图")
		}
		flags := data[20]
		if flags&0x02 == 0 || flags&0x10 == 0 {
			return m, invalidAsset("WebP 必须是带透明通道的动图")
		}
		m = assetMeta{
			contentType: "image/webp", ext: ".webp", animated: true,
			width:  1 + int(data[24]) | int(data[25])<<8 | int(data[26])<<16,
			height: 1 + int(data[27]) | int(data[28])<<8 | int(data[29])<<16,
		}
	default:
		return m, invalidAsset("只接受 PNG(静态)或动态 WebP")
	}
	if m.width != m.height {
		return m, invalidAsset("头像框必须是正方形")
	}
	if m.width < minFrameSide || m.width > maxFrameSide {
		return m, invalidAsset("头像框边长必须在 192 到 1024 像素之间")
	}
	return m, nil
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

func (s *Shop) UploadAsset(ctx context.Context, data []byte, uploadedBy uint) (*AssetView, error) {
	if s.store == nil {
		return nil, errors.NewWithCode(errors.ErrShopStorageUnavailable)
	}
	meta, err := inspectAsset(data)
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
