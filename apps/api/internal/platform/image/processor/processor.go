package processor

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"api/internal/platform/image/preset"

	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
	"go.n16f.net/thumbhash"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxDecodedPixels = 50 * 1000 * 1000

var (
	ErrTooLarge     = errors.New("processor: decoded image exceeds pixel limit")
	ErrInvalidInput = errors.New("processor: invalid input")
)

type Output struct {
	Data        []byte
	MIME        string
	Ext         string
	Width       int
	Height      int
	Thumbhash   string
	VariantName string
}

func Decode(r io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	b := img.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > MaxDecodedPixels {
		return nil, "", ErrTooLarge
	}
	return img, format, nil
}

func fitInside(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	ratio := float64(srcW) / float64(srcH)
	targetW, targetH := maxW, maxH
	if float64(maxW)/float64(maxH) > ratio {
		targetW = int(float64(maxH) * ratio)
	} else {
		targetH = int(float64(maxW) / ratio)
	}
	if targetW < 1 {
		targetW = 1
	}
	if targetH < 1 {
		targetH = 1
	}
	return targetW, targetH
}

func resizeInside(src image.Image, maxW, maxH int) *image.NRGBA {
	b := src.Bounds()
	dstW, dstH := fitInside(b.Dx(), b.Dy(), maxW, maxH)
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func resizeCover(src image.Image, targetW, targetH int) *image.NRGBA {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()

	srcAspect := float64(srcW) / float64(srcH)
	dstAspect := float64(targetW) / float64(targetH)

	var cropW, cropH int
	if srcAspect > dstAspect {
		cropH = srcH
		cropW = int(float64(srcH) * dstAspect)
	} else {
		cropW = srcW
		cropH = int(float64(srcW) / dstAspect)
	}
	if cropW < 1 {
		cropW = 1
	}
	if cropH < 1 {
		cropH = 1
	}
	cropX := b.Min.X + (srcW-cropW)/2
	cropY := b.Min.Y + (srcH-cropH)/2
	cropRect := image.Rect(cropX, cropY, cropX+cropW, cropY+cropH)

	dst := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
	return dst
}

func encodeWebP(img image.Image, quality int) ([]byte, error) {
	if quality < 1 {
		quality = 77
	}
	if quality > 100 {
		quality = 100
	}
	opts, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, float32(quality))
	if err != nil {
		return nil, fmt.Errorf("webp encoder options: %w", err)
	}
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, opts); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}
	return buf.Bytes(), nil
}

func ProcessMain(src image.Image, main preset.MainPipelineConfig) (*Output, error) {
	resized := resizeInside(src, main.FitWidth, main.FitHeight)
	data, err := encodeWebP(resized, main.Quality)
	if err != nil {
		return nil, err
	}
	bounds := resized.Bounds()
	return &Output{
		Data:      data,
		MIME:      "image/webp",
		Ext:       "webp",
		Width:     bounds.Dx(),
		Height:    bounds.Dy(),
		Thumbhash: Thumbhash(resized),
	}, nil
}

func Thumbhash(img image.Image) string {
	return base64.StdEncoding.EncodeToString(thumbhash.EncodeImage(img))
}

func ProcessVariant(src image.Image, v preset.VariantSpec) (*Output, error) {
	var out *image.NRGBA
	switch v.Fit {
	case preset.FitCover:
		out = resizeCover(src, v.Width, v.Height)
	default:
		out = resizeInside(src, v.Width, v.Height)
	}
	data, err := encodeWebP(out, v.Quality)
	if err != nil {
		return nil, err
	}
	bounds := out.Bounds()
	return &Output{
		Data:        data,
		MIME:        "image/webp",
		Ext:         "webp",
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		VariantName: v.Name,
	}, nil
}

func DecodeFromBytes(b []byte) (image.Image, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxDecodedPixels {
		return nil, "", ErrTooLarge
	}
	return Decode(bytes.NewReader(b))
}
