package censoredcovers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestGhostDestroysDetailKeepsAspect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 700, 1000))
	// A hard 1px checkerboard: the highest-frequency detail an image can carry.
	for y := 0; y < 1000; y++ {
		for x := 0; x < 700; x++ {
			c := color.RGBA{A: 255}
			if (x+y)%2 == 0 {
				c = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			}
			src.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	out, w, h, err := ghostBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("ghostBytes: %v", err)
	}
	if w != 448 || h != 640 {
		t.Fatalf("dims = %dx%d, want 448x640 (aspect preserved, long side %d)", w, h, ghostOutSide)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode ghost: %v", err)
	}
	// The checkerboard averages to mid-grey; any surviving pixel-level contrast
	// means the tiny intermediate did not actually destroy the detail.
	var minL, maxL uint32 = 1 << 16, 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			r, g, bb, _ := img.At(x, y).RGBA()
			l := (r + g + bb) / 3
			minL, maxL = min(minL, l), max(maxL, l)
		}
	}
	if spread := maxL - minL; spread > 1<<12 {
		t.Fatalf("luma spread %d after ghosting, want a flat field (<= %d)", spread, 1<<12)
	}
}

func TestScaleTo(t *testing.T) {
	cases := []struct{ w, h, long, wantW, wantH int }{
		{700, 1000, 640, 448, 640},
		{1000, 700, 640, 640, 448},
		{190, 266, 24, 17, 24},
		{5000, 1, 24, 24, 1},
	}
	for _, c := range cases {
		if w, h := scaleTo(c.w, c.h, c.long); w != c.wantW || h != c.wantH {
			t.Fatalf("scaleTo(%d,%d,%d) = %dx%d, want %dx%d", c.w, c.h, c.long, w, h, c.wantW, c.wantH)
		}
	}
}

func TestBetterSource(t *testing.T) {
	if !betterSource(300, 450, 1920, 1080) {
		t.Fatal("a portrait must beat a sharper landscape")
	}
	if betterSource(1920, 1080, 300, 450) {
		t.Fatal("a landscape must not displace a portrait")
	}
	if !betterSource(700, 1000, 300, 450) {
		t.Fatal("within portraits the sharper one wins")
	}
}
