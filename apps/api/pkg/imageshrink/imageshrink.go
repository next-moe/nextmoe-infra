package imageshrink

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"

	"golang.org/x/image/draw"
)

// The image service accepts far less than its clients are told. fiber.New in
// internal/app/app.go sets no BodyLimit, so the real ceiling is Fiber's 4 MiB
// default, while the news client's image_max_file_size says 10 MiB and the
// handler check that would report it never runs. Over the limit the upload
// comes back as a 413 or, for the bigger ones, as a connection reset mid-body
// that reads exactly like a flaky network.
const UploadLimitB = 4 << 20

// fitW/fitH mirror main_pipeline in configs/image_presets.yaml: every upload is
// fitted into this box and re-encoded to webp anyway, so shrinking to the same
// box first discards only bytes the service was going to discard.
const (
	fitW = 1920
	fitH = 1080
)

// Shrink returns body unchanged when it is already within the limit. Otherwise
// it refits and re-encodes, keeping PNG for anything that actually uses its
// alpha channel — a third of hihyou's oversized pictures did, so encoding them
// all as JPEG would flatten real transparency onto black.
// MaxPixels mirrors processor.MaxDecodedPixels: a 10236x5756 Bangumi cover
// (58.9 MP in 4.05 MB) passed the byte check and the service refused it.
const MaxPixels = 50 * 1000 * 1000

func Shrink(body []byte, name string) ([]byte, string, error) {
	if len(body) <= UploadLimitB && withinPixels(body) {
		return body, name, nil
	}
	src, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("decode oversized picture: %w", err)
	}
	transparent := hasAlpha(src)

	for _, box := range []int{0, 2, 3} {
		w, h := fitW, fitH
		if box > 0 {
			w, h = fitW/box, fitH/box
		}
		out, err := encode(resize(src, w, h), transparent)
		if err != nil {
			return nil, "", err
		}
		if len(out) <= UploadLimitB {
			return out, replaceExt(name, transparent), nil
		}
	}
	return nil, "", fmt.Errorf("picture still over %d bytes at a third of the display box", UploadLimitB)
}

func resize(src image.Image, maxW, maxH int) image.Image {
	b := src.Bounds()
	if b.Dx() <= maxW && b.Dy() <= maxH {
		return src
	}
	scale := float64(maxW) / float64(b.Dx())
	if s := float64(maxH) / float64(b.Dy()); s < scale {
		scale = s
	}
	dst := image.NewNRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func encode(img image.Image, transparent bool) ([]byte, error) {
	var buf bytes.Buffer
	if transparent {
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func hasAlpha(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 3 {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			if _, _, _, a := img.At(x, y).RGBA(); a < 0xffff {
				return true
			}
		}
	}
	return false
}

func replaceExt(name string, transparent bool) string {
	ext := ".jpg"
	if transparent {
		ext = ".png"
	}
	if i := bytes.LastIndexByte([]byte(name), '.'); i > 0 {
		return name[:i] + ext
	}
	return name + ext
}

func withinPixels(body []byte) bool {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	return err != nil || int64(cfg.Width)*int64(cfg.Height) <= MaxPixels
}
