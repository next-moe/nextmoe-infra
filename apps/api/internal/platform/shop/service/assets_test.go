package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"api/internal/platform/shop/model"
	"api/pkg/errors"
)

func framePNG(t *testing.T, side int, opaqueCorner bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	c := float64(side) / 2
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			if d := dx*dx + dy*dy; d > (c*0.84)*(c*0.84) && d < (c*0.95)*(c*0.95) {
				img.Set(x, y, color.NRGBA{R: 240, G: 120, B: 160, A: 255})
			}
		}
	}
	if opaqueCorner {
		img.Set(0, 0, color.NRGBA{A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func withAcTL(t *testing.T, p []byte) []byte {
	t.Helper()
	chunk := make([]byte, 0, 20)
	chunk = binary.BigEndian.AppendUint32(chunk, 8)
	body := append([]byte("acTL"), 0, 0, 0, 2, 0, 0, 0, 0)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	ihdrEnd := 8 + 8 + 13 + 4
	out := append([]byte{}, p[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, p[ihdrEnd:]...)
}

// A PNG whose IHDR declares 20000x20000: png.Decode would allocate the whole
// canvas before noticing the data is missing.
func hugePNGHeader(t *testing.T) []byte {
	t.Helper()
	p := framePNG(t, 192, false)
	out := append([]byte{}, p...)
	binary.BigEndian.PutUint32(out[16:20], 20000)
	binary.BigEndian.PutUint32(out[20:24], 20000)
	binary.BigEndian.PutUint32(out[29:33], crc32.ChecksumIEEE(out[12:29]))
	return out
}

func animatedWebPHeader(side int, flags byte) []byte {
	return animatedWebP(side, side, flags)
}

func animatedWebP(width, height int, flags byte) []byte {
	b := make([]byte, 64)
	copy(b[0:4], "RIFF")
	copy(b[8:12], "WEBP")
	copy(b[12:16], "VP8X")
	b[20] = flags
	w, h := width-1, height-1
	b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
	b[27], b[28], b[29] = byte(h), byte(h>>8), byte(h>>16)
	return b
}

func bannerJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	frameSpec      = kinds[model.KindAvatarFrame]
	backgroundSpec = kinds[model.KindProfileBackground]
)

func wantAssetErr(t *testing.T, spec kindSpec, data []byte, what string) {
	t.Helper()
	if _, err := inspectAsset(spec, data); !errors.Is(err, errors.ErrShopInvalidAsset) {
		t.Fatalf("%s: got %v, want ErrShopInvalidAsset", what, err)
	}
}

func TestInspectAsset(t *testing.T) {
	m, err := inspectAsset(frameSpec, framePNG(t, 384, false))
	if err != nil || m.contentType != "image/png" || m.width != 384 || m.animated {
		t.Fatalf("a valid static frame: %+v, %v", m, err)
	}
	m, err = inspectAsset(frameSpec, animatedWebPHeader(384, 0x12))
	if err != nil || m.contentType != "image/webp" || m.width != 384 || !m.animated {
		t.Fatalf("a valid animated WebP header: %+v, %v", m, err)
	}

	for _, side := range []int{192, 256, 512, 1024} {
		if m, err := inspectAsset(frameSpec, animatedWebPHeader(side, 0x12)); err != nil || m.width != side || m.height != side {
			t.Fatalf("a %dpx animated WebP read as %dx%d, %v", side, m.width, m.height, err)
		}
	}

	wantAssetErr(t, frameSpec, hugePNGHeader(t), "a small PNG declaring a huge canvas")
	wantAssetErr(t, frameSpec, framePNG(t, 384, true), "an opaque corner")
	wantAssetErr(t, frameSpec, withAcTL(t, framePNG(t, 384, false)), "an APNG passed off as the static image")
	wantAssetErr(t, frameSpec, framePNG(t, 128, false), "a frame below the minimum side")
	wantAssetErr(t, frameSpec, animatedWebPHeader(384, 0x10), "a WebP without the animation flag")
	wantAssetErr(t, frameSpec, animatedWebPHeader(384, 0x02), "an animated WebP without alpha")
	wantAssetErr(t, frameSpec, []byte("GIF89a...."), "a GIF")
	wantAssetErr(t, frameSpec, bannerJPEG(t, 384, 384), "a JPEG frame, which cannot be transparent")
}

func TestInspectProfileBackground(t *testing.T) {
	m, err := inspectAsset(backgroundSpec, bannerJPEG(t, 1500, 500))
	if err != nil || m.contentType != "image/jpeg" || m.ext != ".jpg" || m.width != 1500 || m.height != 500 {
		t.Fatalf("a 3:1 JPEG banner: %+v, %v", m, err)
	}
	if _, err := inspectAsset(backgroundSpec, animatedWebP(1500, 500, 0x02)); err != nil {
		t.Fatalf("an animated banner needs no alpha: %v", err)
	}
	if _, err := inspectAsset(backgroundSpec, bannerJPEG(t, 1920, 960)); err != nil {
		t.Fatalf("2:1 is the tallest a banner may be: %v", err)
	}

	wantAssetErr(t, backgroundSpec, bannerJPEG(t, 1024, 1024), "a square banner")
	wantAssetErr(t, backgroundSpec, bannerJPEG(t, 2000, 400), "a 5:1 strip")
	wantAssetErr(t, backgroundSpec, bannerJPEG(t, 900, 300), "a banner below the minimum width")
	wantAssetErr(t, backgroundSpec, animatedWebP(4000, 1000, 0x02), "a banner above the maximum width")
	wantAssetErr(t, frameSpec, animatedWebP(1500, 500, 0x12), "a banner uploaded as a frame")
}
