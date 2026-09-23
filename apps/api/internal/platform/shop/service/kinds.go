package service

import (
	"fmt"
	"image"
	"slices"

	"api/internal/platform/shop/model"
)

type assetRule struct {
	maxBytes int
	types    []string
	alpha    bool
	fits     func(w, h int) error
	pixels   func(img image.Image) error
}

type kindSpec struct {
	slot     string
	label    string
	static   assetRule
	animated assetRule
}

var kinds = map[string]kindSpec{
	model.KindAvatarFrame: {
		slot:  model.SlotAvatarFrame,
		label: "头像框",
		static: assetRule{
			maxBytes: 512 << 10, types: []string{"image/png"},
			fits: squareBetween(192, 1024), pixels: transparentCornersAndCentre,
		},
		animated: assetRule{maxBytes: 2 << 20, alpha: true, fits: squareBetween(192, 1024)},
	},
	model.KindProfileBackground: {
		slot:  model.SlotProfileBackground,
		label: "主页背景",
		static: assetRule{
			maxBytes: 1 << 20, types: []string{"image/png", "image/jpeg"},
			fits: wideBetween(960, 3840),
		},
		animated: assetRule{maxBytes: 3 << 20, fits: wideBetween(960, 3840)},
	},
}

const MaxUploadBytes = 3 << 20

func specOf(kind string) (kindSpec, error) {
	spec, ok := kinds[kind]
	if !ok {
		return kindSpec{}, invalidItem("未知的物品类型")
	}
	return spec, nil
}

func slotOf(kind string) (string, bool) {
	spec, ok := kinds[kind]
	return spec.slot, ok
}

func (r assetRule) accepts(contentType string) bool {
	return slices.Contains(r.types, contentType)
}

func squareBetween(lo, hi int) func(w, h int) error {
	return func(w, h int) error {
		if w != h {
			return invalidAsset("头像框必须是正方形")
		}
		if w < lo || w > hi {
			return invalidAsset(fmt.Sprintf("头像框边长必须在 %d 到 %d 像素之间", lo, hi))
		}
		return nil
	}
}

func wideBetween(lo, hi int) func(w, h int) error {
	return func(w, h int) error {
		if w < lo || w > hi {
			return invalidAsset(fmt.Sprintf("主页背景宽度必须在 %d 到 %d 像素之间", lo, hi))
		}
		if w < 2*h || w > 4*h {
			return invalidAsset("主页背景的宽高比必须在 2:1 到 4:1 之间,推荐 3:1")
		}
		return nil
	}
}

func transparentCornersAndCentre(img image.Image) error {
	b := img.Bounds()
	for _, p := range [][2]int{
		{b.Min.X, b.Min.Y}, {b.Max.X - 1, b.Min.Y}, {b.Min.X, b.Max.Y - 1}, {b.Max.X - 1, b.Max.Y - 1},
		{b.Min.X + b.Dx()/2, b.Min.Y + b.Dy()/2},
	} {
		if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0 {
			return invalidAsset("头像框的四角和中心必须是透明的")
		}
	}
	return nil
}
