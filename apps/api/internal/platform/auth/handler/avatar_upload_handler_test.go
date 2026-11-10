package handler

import (
	"fmt"
	"net/http"
	"testing"

	"api/pkg/errors"
	"api/pkg/imageclient"
)

func TestUploadErrorStatus(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   int
	}{
		{fmt.Errorf("%w: 该 preset 不接受此格式", imageclient.ErrMIMEDenied), http.StatusBadRequest, errors.ErrImageMIMEDenied},
		{fmt.Errorf("%w: truncated", imageclient.ErrDecodeFailed), http.StatusBadRequest, errors.ErrImageDecodeFailed},
		{fmt.Errorf("%w: nsfw", imageclient.ErrModerationRejected), http.StatusBadRequest, errors.ErrModerationRejected},
		{fmt.Errorf("%w: 413", imageclient.ErrFileTooLarge), http.StatusRequestEntityTooLarge, errors.ErrImageFileTooLarge},
		{fmt.Errorf("%w: daily", imageclient.ErrQuotaExceeded), http.StatusTooManyRequests, errors.ErrImageQuotaExceeded},
		{fmt.Errorf("dial tcp: connection refused"), http.StatusInternalServerError, errors.ErrOperationFailed},
		{fmt.Errorf("%w: bad secret", imageclient.ErrUnauthorized), http.StatusInternalServerError, errors.ErrOperationFailed},
	}
	for _, tc := range cases {
		status, code := uploadErrorStatus(tc.err)
		if status != tc.status || code != tc.code {
			t.Errorf("%v: got (%d, %d), want (%d, %d)", tc.err, status, code, tc.status, tc.code)
		}
	}
}
