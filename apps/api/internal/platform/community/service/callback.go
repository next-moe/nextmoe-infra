package service

import (
	"context"
	"log/slog"
	"strconv"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

const (
	trustActionNone   = trustclient.ActionNone
	trustActionHide   = trustclient.ActionHide
	trustActionRemove = trustclient.ActionRemove
)

type TrustCallback = trustclient.Callback

type CallbackResult int

const (
	CallbackEnforced CallbackResult = iota
	CallbackUnsupported
)

type CallbackService struct{ db *gorm.DB }

func NewCallbackService(db *gorm.DB) *CallbackService { return &CallbackService{db: db} }

func (s *CallbackService) Handle(ctx context.Context, cb TrustCallback) (CallbackResult, error) {
	if cb.Action != trustActionNone && cb.Action != trustActionHide && cb.Action != trustActionRemove {
		return CallbackUnsupported, nil
	}
	postID, err := strconv.ParseInt(cb.SubjectID, 10, 64)
	if err != nil {
		slog.Warn("trust callback: non-numeric subject_id", "subject_id", cb.SubjectID, "disposition_id", cb.DispositionID)
		return CallbackEnforced, nil
	}

	return CallbackEnforced, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		post, err := repository.GetPostTx(tx, postID)
		if err != nil {
			return err
		}
		if post == nil {
			return nil
		}

		closeStatus := model.ReviewStatusRejected
		switch cb.Action {
		case trustActionHide:
			if post.Status == model.PostStatusVisible {
				if err := repository.SetPostStatusTx(tx, postID, model.PostStatusHidden); err != nil {
					return err
				}
			}
		case trustActionRemove:
			if post.Status != model.PostStatusDeleted {
				if err := repository.SetPostStatusTx(tx, postID, model.PostStatusDeleted); err != nil {
					return err
				}
			}
		case trustActionNone:
			closeStatus = model.ReviewStatusApproved
			if post.Status == model.PostStatusHidden {
				if err := repository.SetPostStatusTx(tx, postID, model.PostStatusVisible); err != nil {
					return err
				}
			}
		}
		return repository.CloseReviewItemsForPostTx(tx, postID, closeStatus)
	})
}
