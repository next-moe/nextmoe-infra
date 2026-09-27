package handler

import (
	"context"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/dto"

	"github.com/danielgtaylor/huma/v2"
)

type imageForm struct {
	File huma.FormFile `form:"file" required:"true" contentType:"application/octet-stream" doc:"The image bytes: JPEG, PNG, WebP or GIF."`
}

type uploadImageInput struct {
	RawBody huma.MultipartFormFiles[imageForm]
}

type ImageBody struct {
	Object string `json:"object" enum:"chat_image"`
	dto.Media
	URL string `json:"url" doc:"where the stored image is served"`
}

type uploadImageOutput struct{ Body ImageBody }

func (h *Handler) uploadImage(ctx context.Context, in *uploadImageInput) (*uploadImageOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	form := in.RawBody.Data()
	if form == nil || !form.File.IsSet {
		p := stamp(ctx, problem.New(problem.CodeValidationFailed, "", "", "a multipart file part named file is required."))
		p.Errors = []problem.FieldError{{Pointer: "/file", Reason: problem.ReasonRequired, Detail: "send multipart/form-data with a file part"}}
		return nil, p
	}
	defer form.File.Close()
	res, err := h.opt.Chat.UploadImage(ctx, a, form.File.Filename, form.File)
	if err != nil {
		return nil, fail(ctx, "upload image", err)
	}
	return &uploadImageOutput{Body: ImageBody{Object: "chat_image", Media: res.Media, URL: res.URL}}, nil
}
