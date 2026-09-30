package ingest

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const maxBody = 2 << 20

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func mediaTypeJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.EqualFold(mt, "application/json")
}

func encodingOK(enc string) bool {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return true
	}
	return strings.EqualFold(enc, "gzip") || strings.EqualFold(enc, "identity")
}

func isGzip(enc string) bool {
	return strings.EqualFold(strings.TrimSpace(enc), "gzip")
}

func readJSONBody(c fiber.Ctx) (body []byte, nRead int64, status int, msg string) {
	raw := c.Request().Body()
	if cl := c.Request().Header.ContentLength(); cl > maxBody {
		return nil, 0, fiber.StatusRequestEntityTooLarge, "body too large"
	}
	if len(raw) > maxBody {
		return nil, int64(len(raw)), fiber.StatusRequestEntityTooLarge, "body too large"
	}
	if isGzip(c.Get("Content-Encoding")) {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, 0, fiber.StatusBadRequest, "gzip stream corrupt"
		}
		defer zr.Close()
		cr := &countingReader{r: io.LimitReader(zr, int64(maxBody)+1)}
		body, err = io.ReadAll(cr)
		nRead = cr.n
		if err != nil {
			return nil, nRead, fiber.StatusBadRequest, "gzip stream corrupt"
		}
		if int64(len(body)) > maxBody {
			return nil, nRead, fiber.StatusRequestEntityTooLarge, "body too large"
		}
		return body, nRead, 0, ""
	}
	return raw, int64(len(raw)), 0, ""
}

func gzipJSONBytes(raw []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(raw)
	_ = w.Close()
	return buf.Bytes()
}
