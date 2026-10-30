package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	_ "image/gif"

	"api/internal/platform/agent/model"
	"api/internal/platform/settings/keys"

	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

var (
	ErrUploadType     = errors.New("agent: this kind of file cannot be attached")
	ErrUploadTooLarge = errors.New("agent: the file is too large")
	ErrUploadUnread   = errors.New("agent: the file could not be read")
)

const visionEdge = 1568

type UploadKind struct {
	Kind     string
	MaxBytes int64
	Types    []string
}

type UploadPolicy struct {
	MaxFiles int
	Kinds    []UploadKind
	MaxChars int
}

func PolicyFor(siteID int64, vision bool) UploadPolicy {
	site := uint(siteID)
	p := UploadPolicy{MaxFiles: int(keys.AgentUploadMaxFiles.ForSite(site)), MaxChars: int(keys.AgentTurnMaxInputChars.Get())}
	if vision && keys.AgentUploadImageEnabled.ForSite(site) {
		p.Kinds = append(p.Kinds, UploadKind{model.UploadImage, keys.AgentUploadImageMaxBytes.ForSite(site), keys.AgentUploadImageTypes.ForSite(site)})
	}
	if keys.AgentUploadTextEnabled.ForSite(site) {
		p.Kinds = append(p.Kinds, UploadKind{model.UploadText, keys.AgentUploadTextMaxBytes.ForSite(site), keys.AgentUploadTextTypes.ForSite(site)})
	}
	if keys.AgentUploadDocumentEnabled.ForSite(site) {
		p.Kinds = append(p.Kinds, UploadKind{model.UploadDocument, keys.AgentUploadDocumentMaxBytes.ForSite(site), keys.AgentUploadDocumentTypes.ForSite(site)})
	}
	if p.MaxFiles == 0 {
		p.Kinds = nil
	}
	return p
}

func (p UploadPolicy) classify(filename, mediaType string) (UploadKind, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	mt := strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	for _, k := range p.Kinds {
		if k.Kind == model.UploadImage && slices.Contains(k.Types, mt) {
			return k, true
		}
		if k.Kind != model.UploadImage && slices.Contains(k.Types, ext) {
			return k, true
		}
	}
	return UploadKind{}, false
}

func PrepareUpload(p UploadPolicy, filename, mediaType string, data []byte) (*model.Upload, error) {
	k, ok := p.classify(filename, mediaType)
	if !ok {
		return nil, ErrUploadType
	}
	if int64(len(data)) > k.MaxBytes {
		return nil, ErrUploadTooLarge
	}
	u := &model.Upload{ID: uuid.NewString(), Kind: k.Kind, Filename: filepath.Base(filename), Size: int64(len(data)), CreatedAt: time.Now()}
	switch k.Kind {
	case model.UploadImage:
		raw, mt, err := fitForVision(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUploadUnread, err)
		}
		u.Data, u.MediaType = raw, mt
	case model.UploadText:
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("%w: not UTF-8 text", ErrUploadUnread)
		}
		u.MediaType = "text/plain; charset=utf-8"
		u.Text, u.Truncated = clipChars(string(data), p.MaxChars)
	case model.UploadDocument:
		text, err := extractDocument(filepath.Ext(filename), data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUploadUnread, err)
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%w: no text found; a scanned document has only pictures", ErrUploadUnread)
		}
		u.MediaType = documentType(filepath.Ext(filename))
		u.Text, u.Truncated = clipChars(text, p.MaxChars)
	}
	return u, nil
}

func clipChars(s string, n int) (string, bool) {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s, false
	}
	return string([]rune(s)[:n]), true
}

func fitForVision(data []byte) ([]byte, string, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > visionEdge || h > visionEdge {
		if w >= h {
			w, h = visionEdge, max(1, h*visionEdge/w)
		} else {
			w, h = max(1, w*visionEdge/h), visionEdge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var out bytes.Buffer
	if opaque(dst) {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
		return out.Bytes(), "image/jpeg", err
	}
	err = png.Encode(&out, dst)
	return out.Bytes(), "image/png", err
}

func opaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}

func documentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	return "application/octet-stream"
}

func extractDocument(ext string, data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("malformed document: %v", r)
		}
	}()
	switch strings.ToLower(ext) {
	case ".pdf":
		return pdfText(data)
	case ".docx":
		return docxText(data)
	}
	return "", errors.New("unsupported document")
}

func pdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		t, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(strings.TrimSpace(t))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	f, err := zr.Open("word/document.xml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	dec := xml.NewDecoder(io.LimitReader(f, 64<<20))
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Store) CreateUpload(ctx context.Context, u *model.Upload) error {
	return s.db.WithContext(ctx).Create(u).Error
}

func (s *Store) Upload(ctx context.Context, userID, siteID int64, id string) (*model.Upload, error) {
	var u model.Upload
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND site_id = ?", id, userID, siteID).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) UploadsByID(ctx context.Context, ids []string) (map[string]*model.Upload, error) {
	out := map[string]*model.Upload{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.Upload
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}

func (s *Store) SweepUploads(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("conversation_id IS NULL AND created_at < ?", before).Delete(&model.Upload{})
	return res.RowsAffected, res.Error
}
