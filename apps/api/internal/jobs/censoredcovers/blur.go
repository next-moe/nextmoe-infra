package censoredcovers

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// The intermediate is what makes the ghost irreversible: every pixel of
	// the output interpolates a 24px-long-side average of the source, so no
	// explicit detail survives — the result is a soft color field that keeps
	// only the palette identity of the art. Safe bytes for the SFW face, for
	// crawlers, and for content raters.
	ghostTinySide = 24

	ghostOutSide = 640

	jpegQuality = 90
)

func ghostBytes(src []byte) ([]byte, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode source art: %w", err)
	}
	out := ghost(img)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, 0, 0, fmt.Errorf("encode ghost: %w", err)
	}
	b := out.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

func ghost(src image.Image) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	tw, th := scaleTo(sw, sh, ghostTinySide)
	tiny := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(tiny, tiny.Bounds(), src, src.Bounds(), draw.Src, nil)

	ow, oh := scaleTo(sw, sh, ghostOutSide)
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	draw.CatmullRom.Scale(out, out.Bounds(), tiny, tiny.Bounds(), draw.Src, nil)
	return out
}

func scaleTo(w, h, long int) (int, int) {
	if w <= 0 || h <= 0 {
		return long, long
	}
	if w >= h {
		return long, max(1, (h*long+w/2)/w)
	}
	return max(1, (w*long+h/2)/h), long
}
