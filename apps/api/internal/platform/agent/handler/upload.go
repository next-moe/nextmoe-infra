package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/service"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
)

type UploadOption struct {
	Kind     string `json:"kind" enum:"image,text,document"`
	Label    string `json:"label"`
	Icon     string `json:"icon" doc:"A lucide icon name."`
	Accept   string `json:"accept" doc:"Ready for <input type=file accept>."`
	MaxBytes int64  `json:"max_bytes"`
}

type UploadsBody struct {
	MaxFiles int            `json:"max_files" doc:"Files one message may carry; 0 means no upload button."`
	Options  []UploadOption `json:"options" doc:"One entry per menu item, in display order."`
}

var uploadLabels = map[string][2]string{
	model.UploadImage:    {"图片", "image"},
	model.UploadText:     {"文本 / 代码", "file-text"},
	model.UploadDocument: {"文档", "file"},
}

func (s *Server) visionReady() bool { return s.Vision != nil && s.Vision() }

func (s *Server) uploadsBody(siteID int64) UploadsBody {
	p := service.PolicyFor(siteID, s.visionReady())
	out := UploadsBody{MaxFiles: p.MaxFiles, Options: []UploadOption{}}
	for _, k := range p.Kinds {
		l := uploadLabels[k.Kind]
		out.Options = append(out.Options, UploadOption{Kind: k.Kind, Label: l[0], Icon: l[1], Accept: strings.Join(k.Types, ","), MaxBytes: k.MaxBytes})
	}
	return out
}

type UploadBody struct {
	ID        string `json:"id"`
	Kind      string `json:"kind" enum:"image,text,document"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size" doc:"Bytes the user sent."`
	Chars     int    `json:"chars" doc:"Characters of text the model will read; 0 for a picture."`
	Truncated bool   `json:"truncated" doc:"The text was longer than one message may carry and was cut."`
	URL       string `json:"url" doc:"Where the file can be fetched back from the agent with the same credentials."`
}

func uploadBody(u *model.Upload) UploadBody {
	return UploadBody{ID: u.ID, Kind: u.Kind, MediaType: u.MediaType, Filename: u.Filename, Size: u.Size,
		Chars: len([]rune(u.Text)), Truncated: u.Truncated, URL: "/v2/agent/uploads/" + u.ID}
}

type uploadForm struct {
	File huma.FormFile `form:"file" required:"true"`
}

func (s *Server) registerUploads(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "createAgentUpload", Method: http.MethodPost, Path: "/v2/agent/uploads",
		Summary: "Attach a file", Tags: []string{"agent"}, DefaultStatus: http.StatusCreated,
		Description: "Stores one file for the caller's next message; reference it as {\"type\":\"file\",\"uploadId\":…} in the parts. Pictures are scaled down, documents keep only their text. Unused uploads are dropped after a day."},
		func(ctx context.Context, in *struct {
			RawBody huma.MultipartFormFiles[uploadForm]
		}) (*struct{ Body UploadBody }, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			f := in.RawBody.Data().File
			data, err := io.ReadAll(io.LimitReader(f, 31<<20))
			if err != nil {
				return nil, huma.Error400BadRequest("could not read the file")
			}
			u, err := service.PrepareUpload(service.PolicyFor(c.SiteID, s.visionReady()), f.Filename, f.ContentType, data)
			switch {
			case errors.Is(err, service.ErrUploadType):
				return nil, fail(http.StatusUnsupportedMediaType, CodeUnsupportedAttachment, "this kind of file cannot be attached here")
			case errors.Is(err, service.ErrUploadTooLarge):
				return nil, fail(http.StatusRequestEntityTooLarge, CodeFileTooLarge, "the file is larger than this site allows")
			case errors.Is(err, service.ErrUploadUnread):
				return nil, fail(http.StatusUnprocessableEntity, CodeUnreadableFile, err.Error())
			case err != nil:
				return nil, err
			}
			u.UserID, u.SiteID = c.UserID, c.SiteID
			if err := s.Store.CreateUpload(ctx, u); err != nil {
				return nil, err
			}
			return &struct{ Body UploadBody }{uploadBody(u)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getAgentUpload", Method: http.MethodGet, Path: "/v2/agent/uploads/{id}",
		Summary: "Fetch an attached file back", Tags: []string{"agent"},
		Description: "The scaled-down picture, or the text the model read for a text file or document."},
		func(ctx context.Context, in *struct {
			ID string `path:"id" maxLength:"36"`
		}) (*struct {
			ContentType  string `header:"Content-Type"`
			CacheControl string `header:"Cache-Control"`
			Body         []byte
		}, error) {
			c, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			u, err := s.Store.Upload(ctx, c.UserID, c.SiteID, in.ID)
			if errors.Is(err, service.ErrNotFound) {
				return nil, huma.Error404NotFound("no such upload")
			}
			if err != nil {
				return nil, err
			}
			out := &struct {
				ContentType  string `header:"Content-Type"`
				CacheControl string `header:"Cache-Control"`
				Body         []byte
			}{ContentType: u.MediaType, CacheControl: "private, max-age=86400", Body: u.Data}
			if u.Kind != model.UploadImage {
				out.ContentType, out.Body = "text/plain; charset=utf-8", []byte(u.Text)
			}
			return out, nil
		})
}

func (s *Server) validateParts(ctx context.Context, c *caller, conv *model.Conversation, in []service.Part) ([]service.Part, error) {
	typed, attached, files := 0, 0, 0
	out := make([]service.Part, 0, len(in))
	for _, p := range in {
		switch {
		case p.Type == "text":
			out = append(out, service.Part{Type: "text", Text: strings.Clone(p.Text)})
			typed += len([]rune(p.Text))
		case p.Type == "file" && p.UploadID != "":
			u, err := s.Store.Upload(ctx, c.UserID, c.SiteID, p.UploadID)
			if err != nil || (u.ConversationID != nil && *u.ConversationID != conv.ID) {
				return nil, fail(http.StatusBadRequest, CodeBadUpload, "uploadId is not an upload of yours for this conversation")
			}
			files++
			attached += len([]rune(u.Text))
			out = append(out, service.Part{Type: "file", UploadID: u.ID, MediaType: u.MediaType, Filename: u.Filename, URL: "/v2/agent/uploads/" + u.ID})
		case p.Type == "file":
			if !strings.HasPrefix(p.MediaType, "text/") && p.MediaType != "application/json" && p.MediaType != "application/x-yaml" {
				return nil, fail(http.StatusUnsupportedMediaType, CodeUnsupportedAttachment, "upload the file to /v2/agent/uploads and send its uploadId")
			}
			files++
			attached += len([]rune(p.Text))
			out = append(out, service.Part{Type: "file", Text: strings.Clone(p.Text), MediaType: strings.Clone(p.MediaType), Filename: strings.Clone(p.Filename)})
		default:
			return nil, huma.Error400BadRequest("unsupported part type " + p.Type)
		}
	}
	if limit := int(keys.AgentUploadMaxFiles.ForSite(uint(c.SiteID))); files > limit {
		return nil, fail(http.StatusBadRequest, CodeTooManyFiles, "too many files in one message")
	}
	if typed == 0 && files == 0 {
		return nil, fail(http.StatusBadRequest, CodeEmptyMessage, "the message is empty")
	}
	// The typed text and the attachments are capped separately: an upload is cut
	// to the whole cap, so one shared budget refused every question sent with a
	// cut file (letmoe local e2e, a 390 KB .txt).
	limit := int(keys.AgentTurnMaxInputChars.Get())
	if typed > limit {
		return nil, fail(http.StatusRequestEntityTooLarge, CodeInputTooLong, "the message is too long")
	}
	if attached > limit {
		return nil, fail(http.StatusRequestEntityTooLarge, CodeInputTooLong, "the attachments together are too long")
	}
	return out, nil
}
