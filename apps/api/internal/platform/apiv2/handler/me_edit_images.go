package handler

import (
	"context"
	"errors"
	"io"
	"strconv"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/pkg/imageclient"
)

type EditImageUpload func(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)

var editImagePresets = map[string]string{
	"cover":      "catalog_cover",
	"screenshot": "catalog_screenshot",
}

func (c *Catalog) UploadEditImage(ctx context.Context, preset, filename string, body io.Reader) (repr.EditImage, error) {
	if c == nil || c.Uploads == nil {
		return repr.EditImage{}, problem.New(problem.CodeServiceUnavailable, "", "", "the editor image upload leg is not configured.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.EditImage{}, err
	}
	site, serr := requireSite(ctx)
	if serr != nil {
		return repr.EditImage{}, serr
	}
	target, ok := editImagePresets[preset]
	if !ok {
		p := problem.New(problem.CodeValidationFailed, "", "", "preset is not an editor image slot.")
		p.Errors = []problem.FieldError{{Pointer: "/preset", Reason: problem.ReasonUnknownValue,
			Detail: "expected one of: cover, screenshot",
			Params: &problem.FieldParams{Allowed: &[]string{"cover", "screenshot"}}}}
		return repr.EditImage{}, p
	}
	res, uerr := c.Uploads(ctx, body, filename, target, site+":"+strconv.FormatInt(uid, 10))
	if uerr != nil {
		return repr.EditImage{}, editImageErr(uerr)
	}
	return editImageFrom(preset, res), nil
}

func editImageErr(err error) error {
	switch {
	case errors.Is(err, imageclient.ErrQuotaExceeded):
		return problem.New(problem.CodeQuotaExceeded, "", "", "the image quota for this site is exhausted.")
	case errors.Is(err, imageclient.ErrModerationRejected):
		p := problem.New(problem.CodeValidationFailed, "", "", "these bytes were rejected by image moderation.")
		p.Errors = []problem.FieldError{{Pointer: "/file", Reason: problem.ReasonNotAllowedValue,
			Detail: "rejected by image moderation"}}
		return p
	}
	return problem.New(problem.CodeServiceUnavailable, "", "", "the image service did not accept the upload.")
}

func editImageFrom(preset string, res *imageclient.UploadResult) repr.EditImage {
	out := repr.EditImage{
		Object: "edit_image", Preset: preset, URL: res.URL, Hash: res.Hash,
		SizeBytes: res.SizeBytes, IsDeduplicated: res.Deduplicated,
	}
	if res.Width > 0 {
		w := res.Width
		out.Width = &w
	}
	if res.Height > 0 {
		h := res.Height
		out.Height = &h
	}
	if res.Thumbhash != "" {
		t := res.Thumbhash
		out.Thumbhash = &t
	}
	return out
}
