package imageshrink

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngBytes(t *testing.T, w, h int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			// Incompressible noise. An earlier fixture used x*y / x^y / x+y,
			// which is periodic in 256 and deflated a 2600x2000 image to 0.5 MB
			// — it never reached the shrink path the test claimed to exercise.
			n := uint32(y*w+x) * 2654435761
			n ^= n >> 15
			img.Set(x, y, color.NRGBA{uint8(n), uint8(n >> 8), uint8(n >> 16), a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestShrinkLeavesSmallPicturesAlone(t *testing.T) {
	in := pngBytes(t, 200, 120, false)
	out, name, err := Shrink(in, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, out) || name != "a.png" {
		t.Error("a picture under the limit must be uploaded as fetched")
	}
}

// Half of this image is fully transparent. Re-encoding it as JPEG would keep it
// under the limit and look fine in a byte count — and flatten the transparency
// onto black. A third of hihyou's oversized pictures really did use alpha.
func TestShrinkKeepsTransparency(t *testing.T) {
	in := pngBytes(t, 2600, 2000, true)
	if len(in) <= UploadLimitB {
		t.Fatalf("fixture is only %d bytes — it never reaches the shrink path", len(in))
	}
	out, name, err := Shrink(in, "big.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > UploadLimitB {
		t.Fatalf("shrunk to %d bytes, still over the %d limit", len(out), UploadLimitB)
	}
	if name != "big.png" {
		t.Errorf("name = %q, want a .png extension", name)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(2, 2).RGBA(); a != 0 {
		t.Errorf("top-left alpha = %d, want 0 — the transparency was flattened", a)
	}
	if _, _, _, a := img.At(img.Bounds().Dx()-3, 2).RGBA(); a != 0xffff {
		t.Errorf("top-right alpha = %d, want opaque", a)
	}
	if b := img.Bounds(); b.Dx() > fitW || b.Dy() > fitH {
		t.Errorf("refit to %dx%d, larger than the pipeline's box", b.Dx(), b.Dy())
	}
}

func TestShrinkRefitsOpaquePictures(t *testing.T) {
	in := pngBytes(t, 2600, 2000, false)
	out, name, err := Shrink(in, "big.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > UploadLimitB {
		t.Fatalf("shrunk to %d bytes", len(out))
	}
	if name != "big.jpg" {
		t.Errorf("name = %q, want the extension to follow the re-encode", name)
	}
}

func TestShrinkRefitsAPictureOverThePixelLimitEvenWhenItIsSmall(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 7200, 7000))); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > UploadLimitB {
		t.Fatalf("fixture is %d bytes; it must pass the byte check to test the pixel one", buf.Len())
	}
	out, _, err := Shrink(buf.Bytes(), "big.png")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels || cfg.Height > fitH {
		t.Fatalf("out = %dx%d, want it refit into the display box", cfg.Width, cfg.Height)
	}
}
