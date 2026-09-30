package symbols

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/telemetry/ingest"

	"github.com/gofiber/fiber/v3"
)

const (
	defaultMaxBody     = 100 << 20
	defaultMaxFile     = 64 << 20
	defaultSlotWait    = 5 * time.Second
	defaultUploadSlots = 2
	maxTextField       = 256
)

var (
	errBodyTooLarge = errors.New("body too large")
	serviceVerRe    = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,127}$`)
	engineRevRe     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type UploadApp struct {
	ID          int64
	ServiceName string
	Enabled     bool
}

type IncomingFile struct {
	FileName string
	Kind     string
	Arch     string
	BuildID  string
	SHA256   string
	Size     int64
	Path     string
}

type TokenLookup interface {
	LookupBySymbolsTokenHash(ctx context.Context, hash string) (*UploadApp, error)
}

type UploadRepo interface {
	IngestSymbolUpload(ctx context.Context, appID int64, version, engineRev string, files []IncomingFile) (uploadID int64, created bool, err error)
}

type Handler struct {
	tokens   TokenLookup
	repo     UploadRepo
	log      *slog.Logger
	sem      chan struct{}
	slotWait time.Duration
	maxBody  int64
	maxFile  int64
	tempRoot string
}

func NewHandler(tokens TokenLookup, repo UploadRepo, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		tokens:   tokens,
		repo:     repo,
		log:      log,
		sem:      make(chan struct{}, defaultUploadSlots),
		slotWait: defaultSlotWait,
		maxBody:  defaultMaxBody,
		maxFile:  defaultMaxFile,
	}
}

type uploadResult struct {
	UploadID int64        `json:"upload_id"`
	Created  bool         `json:"created"`
	Files    []fileResult `json:"files"`
}

type fileResult struct {
	FileName string `json:"file_name"`
	Kind     string `json:"kind"`
	Arch     string `json:"arch"`
	BuildID  string `json:"build_id"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type errBody struct {
	Message string `json:"message"`
}

func (h *Handler) Symbols(c fiber.Ctx) error {
	auth := c.Get("Authorization")
	tok, ok := parseBearerToken(auth)
	if !ok {
		return h.reply(c, fiber.StatusUnauthorized, "unauthorized")
	}
	hash := HashToken(tok)
	app, err := h.tokens.LookupBySymbolsTokenHash(c.Context(), hash)
	if err != nil {
		h.log.Error("telemetry symbols token lookup", "err", err)
		return h.retry(c, "lookup failed")
	}
	if app == nil {
		return h.reply(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !app.Enabled {
		return h.reply(c, fiber.StatusForbidden, "app disabled")
	}
	ct := c.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.EqualFold(mt, "multipart/form-data") || params["boundary"] == "" {
		return h.reply(c, fiber.StatusUnsupportedMediaType, "content type must be multipart/form-data")
	}

	timer := time.NewTimer(h.slotWait)
	defer timer.Stop()
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	case <-timer.C:
		return h.retry(c, "upload overloaded")
	case <-c.Context().Done():
		return h.retry(c, "upload overloaded")
	}

	dir, err := os.MkdirTemp(h.tempRoot, "tel-sym-*")
	if err != nil {
		h.log.Error("telemetry symbols temp dir", "err", err)
		return h.retry(c, "storage failed")
	}
	defer os.RemoveAll(dir)

	parsed, status, msg := h.parseMultipart(c, params["boundary"], dir, app.ServiceName)
	if status != 0 {
		return h.reply(c, status, msg)
	}

	id, created, err := h.repo.IngestSymbolUpload(c.Context(), app.ID, parsed.version, parsed.engine, parsed.files)
	if err != nil {
		h.log.Error("telemetry symbols persist", "app_id", app.ID, "err", err)
		return h.retry(c, "storage failed")
	}
	names := make([]string, 0, len(parsed.files))
	out := make([]fileResult, 0, len(parsed.files))
	for _, f := range parsed.files {
		names = append(names, f.FileName)
		out = append(out, fileResult{
			FileName: f.FileName,
			Kind:     f.Kind,
			Arch:     f.Arch,
			BuildID:  f.BuildID,
			SHA256:   f.SHA256,
			Size:     f.Size,
		})
	}
	h.log.Info("telemetry symbols upload", "app_id", app.ID, "version", parsed.version, "files", names, "created", created)
	payload, err := json.Marshal(uploadResult{UploadID: id, Created: created, Files: out})
	if err != nil {
		return h.retry(c, "encode failed")
	}
	c.Set("Content-Type", "application/json")
	c.Status(fiber.StatusOK)
	return c.Send(payload)
}

type parsedUpload struct {
	version string
	engine  string
	files   []IncomingFile
}

func (h *Handler) parseMultipart(c fiber.Ctx, boundary, dir, serviceName string) (parsedUpload, int, string) {
	body := &limitedReader{r: ingest.RequestBodyReader(c), max: h.maxBody}
	mr := multipart.NewReader(body, boundary)
	var version, engine string
	var sawName, sawVersion, sawEngine bool
	seenFile := map[string]bool{}
	var files []IncomingFile
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, errBodyTooLarge) || errors.Is(body.err, errBodyTooLarge) {
				return parsedUpload{}, fiber.StatusRequestEntityTooLarge, "body too large"
			}
			return parsedUpload{}, fiber.StatusBadRequest, "malformed multipart"
		}
		name := part.FormName()
		filename := filepath.Base(part.FileName())
		if filename == "." || filename == string(filepath.Separator) {
			filename = ""
		}
		if filename != "" {
			if name != "file" {
				_ = part.Close()
				return parsedUpload{}, fiber.StatusBadRequest, "unexpected part"
			}
			if seenFile[filename] {
				_ = part.Close()
				return parsedUpload{}, fiber.StatusBadRequest, "duplicate file"
			}
			if _, _, ok := ClassifyFileName(filename); !ok {
				_ = part.Close()
				return parsedUpload{}, fiber.StatusBadRequest, "unexpected file name"
			}
			seenFile[filename] = true
			inc, status, msg := h.slurpFile(dir, filename, part)
			_ = part.Close()
			if status != 0 {
				return parsedUpload{}, status, msg
			}
			files = append(files, inc)
			continue
		}
		switch name {
		case "service.name", "service.version", "flutter.engine_revision":
		default:
			_ = part.Close()
			return parsedUpload{}, fiber.StatusBadRequest, "unexpected part"
		}
		raw, err := io.ReadAll(io.LimitReader(part, maxTextField+1))
		_ = part.Close()
		if err != nil {
			if errors.Is(err, errBodyTooLarge) || errors.Is(body.err, errBodyTooLarge) {
				return parsedUpload{}, fiber.StatusRequestEntityTooLarge, "body too large"
			}
			return parsedUpload{}, fiber.StatusBadRequest, "malformed multipart"
		}
		if int64(len(raw)) > maxTextField {
			return parsedUpload{}, fiber.StatusBadRequest, "field too large"
		}
		val := string(raw)
		switch name {
		case "service.name":
			if sawName {
				return parsedUpload{}, fiber.StatusBadRequest, "repeated field"
			}
			sawName = true
			if val != serviceName {
				return parsedUpload{}, fiber.StatusBadRequest, "service.name mismatch"
			}
		case "service.version":
			if sawVersion {
				return parsedUpload{}, fiber.StatusBadRequest, "repeated field"
			}
			sawVersion = true
			if !serviceVerRe.MatchString(val) {
				return parsedUpload{}, fiber.StatusBadRequest, "invalid service.version"
			}
			version = val
		case "flutter.engine_revision":
			if sawEngine {
				return parsedUpload{}, fiber.StatusBadRequest, "repeated field"
			}
			sawEngine = true
			if !engineRevRe.MatchString(val) {
				return parsedUpload{}, fiber.StatusBadRequest, "invalid flutter.engine_revision"
			}
			engine = val
		}
	}
	if body.err != nil && errors.Is(body.err, errBodyTooLarge) {
		return parsedUpload{}, fiber.StatusRequestEntityTooLarge, "body too large"
	}
	if !sawName {
		return parsedUpload{}, fiber.StatusBadRequest, "service.name mismatch"
	}
	if !sawVersion || version == "" {
		return parsedUpload{}, fiber.StatusBadRequest, "invalid service.version"
	}
	if len(files) == 0 && engine == "" {
		return parsedUpload{}, fiber.StatusBadRequest, "no files"
	}
	return parsedUpload{version: version, engine: engine, files: files}, 0, ""
}

func (h *Handler) slurpFile(dir, filename string, r io.Reader) (IncomingFile, int, string) {
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		return IncomingFile{}, fiber.StatusServiceUnavailable, "storage failed"
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r, h.maxFile+1))
	closeErr := f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, errBodyTooLarge) {
			return IncomingFile{}, fiber.StatusRequestEntityTooLarge, "body too large"
		}
		return IncomingFile{}, fiber.StatusBadRequest, "malformed multipart"
	}
	if closeErr != nil {
		return IncomingFile{}, fiber.StatusServiceUnavailable, "storage failed"
	}
	if n > h.maxFile {
		return IncomingFile{}, fiber.StatusRequestEntityTooLarge, "file too large"
	}
	kind, arch, buildID, err := ValidateFile(path, filename)
	if err != nil {
		return IncomingFile{}, fiber.StatusBadRequest, filename + ": " + err.Error()
	}
	return IncomingFile{
		FileName: filename,
		Kind:     kind,
		Arch:     arch,
		BuildID:  buildID,
		SHA256:   hex.EncodeToString(hash.Sum(nil)),
		Size:     n,
		Path:     path,
	}, 0, ""
}

func (h *Handler) reply(c fiber.Ctx, status int, msg string) error {
	// fasthttp resets the connection if the handler returns with unread
	// BodyStream bytes: TestUploadAuthBeforeBody got
	// `write: connection reset by peer` instead of 401.
	if s := c.Request().BodyStream(); s != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(s, h.maxBody+1))
	}
	c.Set("Content-Type", "application/json")
	c.Status(status)
	payload, _ := json.Marshal(errBody{Message: msg})
	return c.Send(payload)
}

func (h *Handler) retry(c fiber.Ctx, msg string) error {
	c.Set("Retry-After", strconv.Itoa(60))
	return h.reply(c, fiber.StatusServiceUnavailable, msg)
}

func parseBearerToken(h string) (string, bool) {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	tok := h[len(p):]
	if len(tok) != 64 {
		return "", false
	}
	for i := 0; i < 64; i++ {
		c := tok[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return tok, true
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type limitedReader struct {
	r   io.Reader
	n   int64
	max int64
	err error
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	if l.n > l.max {
		l.err = errBodyTooLarge
		return n, errBodyTooLarge
	}
	if err != nil {
		return n, err
	}
	return n, nil
}
