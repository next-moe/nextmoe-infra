package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"

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

func animatedWebPHeader(side int, flags byte) []byte {
	b := make([]byte, 64)
	copy(b[0:4], "RIFF")
	copy(b[8:12], "WEBP")
	copy(b[12:16], "VP8X")
	b[20] = flags
	w := side - 1
	b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
	b[27], b[28], b[29] = byte(w), byte(w>>8), byte(w>>16)
	return b
}

func wantAssetErr(t *testing.T, data []byte, what string) {
	t.Helper()
	if _, err := inspectAsset(data); !errors.Is(err, errors.ErrShopInvalidAsset) {
		t.Fatalf("%s: got %v, want ErrShopInvalidAsset", what, err)
	}
}

func TestInspectAsset(t *testing.T) {
	m, err := inspectAsset(framePNG(t, 384, false))
	if err != nil || m.contentType != "image/png" || m.width != 384 || m.animated {
		t.Fatalf("a valid static frame: %+v, %v", m, err)
	}
	m, err = inspectAsset(animatedWebPHeader(384, 0x12))
	if err != nil || m.contentType != "image/webp" || m.width != 384 || !m.animated {
		t.Fatalf("a valid animated WebP header: %+v, %v", m, err)
	}

	wantAssetErr(t, framePNG(t, 384, true), "an opaque corner")
	wantAssetErr(t, withAcTL(t, framePNG(t, 384, false)), "an APNG passed off as the static image")
	wantAssetErr(t, framePNG(t, 128, false), "a frame below the minimum side")
	wantAssetErr(t, animatedWebPHeader(384, 0x10), "a WebP without the animation flag")
	wantAssetErr(t, animatedWebPHeader(384, 0x02), "an animated WebP without alpha")
	wantAssetErr(t, []byte("GIF89a...."), "a GIF")
}
