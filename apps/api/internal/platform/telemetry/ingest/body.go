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

// RequestBodyReader is the only telemetry path that may inspect a request body.
// With StreamRequestBody, fasthttp's Body method reads the remaining stream into
// memory unbounded, so a later reader who calls it on a streamed request
// reintroduces unbounded buffering.
func RequestBodyReader(c fiber.Ctx) io.Reader {
	if s := c.Request().BodyStream(); s != nil {
		return s
	}
	if OnBufferedBody != nil {
		OnBufferedBody()
	}
	return bytes.NewReader(c.Request().Body())
}

var OnBufferedBody func()

func readJSONBody(c fiber.Ctx) (body []byte, nRead int64, status int, msg string) {
	if cl := c.Request().Header.ContentLength(); cl > maxBody {
		return nil, 0, fiber.StatusRequestEntityTooLarge, "body too large"
	}
	cr := &countingReader{r: io.LimitReader(RequestBodyReader(c), int64(maxBody)+1)}
	raw, err := io.ReadAll(cr)
	nRead = cr.n
	if err != nil {
		return nil, nRead, fiber.StatusBadRequest, "body unreadable"
	}
	if int64(len(raw)) > maxBody {
		return nil, nRead, fiber.StatusRequestEntityTooLarge, "body too large"
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

func drainRequest(c fiber.Ctx, max int64) {
	// fasthttp resets the connection if the handler returns with unread
	// BodyStream bytes: TestStreamedLogsBodyCapped got
	// `write: connection reset by peer` instead of 413.
	if s := c.Request().BodyStream(); s != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(s, max))
	}
}

func gzipJSONBytes(raw []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(raw)
	_ = w.Close()
	return buf.Bytes()
}
