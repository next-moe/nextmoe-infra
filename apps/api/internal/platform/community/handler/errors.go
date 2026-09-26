package handler

import (
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/community/service"
	"api/pkg/errors"
)

func mapErr(op string, err error) *houseError {
	var (
		sandbox   *service.SandboxError
		invalid   *service.InvalidError
		conflict  *service.ConflictError
		forbidden *service.ForbiddenError
	)
	switch {
	case stderrors.As(err, &sandbox):
		return apiErrMsg(http.StatusTooManyRequests, errors.ErrOperationFailed, "sandbox limit: "+sandbox.Reason)
	case stderrors.As(err, &invalid):
		return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, invalid.Reason)
	case stderrors.As(err, &conflict):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, conflict.Reason)
	case stderrors.As(err, &forbidden):
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden, forbidden.Reason)
	case stderrors.Is(err, service.ErrThreadNotFound),
		stderrors.Is(err, service.ErrPostNotFound),
		stderrors.Is(err, service.ErrReviewNotFound),
		stderrors.Is(err, service.ErrBoardNotFound),
		stderrors.Is(err, service.ErrActivityGroupNotFound):
		return apiErr(http.StatusNotFound, errors.ErrNotFound)
	case stderrors.Is(err, service.ErrNotFollowing):
		return apiErrMsg(http.StatusNotFound, errors.ErrNotFound, "not following")
	case stderrors.Is(err, service.ErrNothingToRestore):
		return apiErrMsg(http.StatusNotFound, errors.ErrNotFound, "no purge of this author in the last 30 days is left to restore")
	case stderrors.Is(err, service.ErrThreadNotOpen):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, "thread is not open")
	case stderrors.Is(err, service.ErrInvalidSearchQuery):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "search query must be 2-100 characters")
	case stderrors.Is(err, service.ErrInvalidNotificationLevel):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "notification level out of range")
	case stderrors.Is(err, service.ErrNotFeedback):
		return apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "thread is not a feedback thread")
	case stderrors.Is(err, service.ErrNotAuthor):
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden, "not the post author")
	case stderrors.Is(err, service.ErrPostNotEditable):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, "post is not editable")
	case stderrors.Is(err, service.ErrContentBlocked):
		return apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "content blocked by word list")
	default:
		slog.Error("community "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}
