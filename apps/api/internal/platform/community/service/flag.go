package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/settings/keys"

	"gorm.io/gorm"
)

var flagBaseByLevel = [...]float32{1.0, 1.2, 1.5, 2.0, 2.5}

type FlagService struct {
	db   *gorm.DB
	sink EventSink
}

func NewFlagService(db *gorm.DB, sink EventSink) *FlagService {
	return &FlagService{db: db, sink: sink}
}

func (s *FlagService) Submit(ctx context.Context, postID, flaggerID int64, reason *int16, note *string) error {
	var crossed bool
	var enqueuedItemID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pc, found, err := repository.PostContextTx(tx, postID)
		if err != nil {
			return err
		}
		if !found {
			return ErrPostNotFound
		}
		if crossTenantCtx(ctx, pc.Site, pc.AnchorKind) {
			return ErrPostNotFound
		}
		reporter, err := repository.GetOrCreateTrustTx(tx, flaggerID)
		if err != nil {
			return err
		}
		weight := flagBase(reporter.Level) * accuracyFactor(reporter)
		inserted, err := repository.InsertFlagTx(tx, &model.CommunityFlag{
			PostID: postID, FlaggerID: flaggerID, Reason: reason, Note: note,
			Weight: weight, Status: model.FlagStatusPending,
		})
		if err != nil {
			return err
		}
		if !inserted || pc.Status != model.PostStatusVisible {
			return nil
		}

		hide, err := s.shouldHide(tx, reporter, pc.AuthorID, postID)
		if err != nil {
			return err
		}
		if !hide {
			return nil
		}
		if err := repository.SetPostStatusTx(tx, postID, model.PostStatusHidden); err != nil {
			return err
		}
		itemID, created, err := repository.EnqueueReviewIfAbsentTx(tx, pc.Site, postID, model.ReviewSourceFlags, nil)
		if err != nil {
			return err
		}
		if created {
			enqueuedItemID = itemID
		}
		crossed = true
		return nil
	})
	if err != nil {
		return err
	}
	if crossed {
		s.sink.Emit(Event{Kind: EventFlagThreshold, PostID: postID})
	}
	if enqueuedItemID != 0 {
		s.sink.Emit(Event{Kind: EventReviewEnqueued, PostID: postID, ReviewItemID: enqueuedItemID})
	}
	return nil
}

func (s *FlagService) shouldHide(tx *gorm.DB, reporter *model.CommunityTrust, authorID, postID int64) (bool, error) {
	if reporter.Level >= model.TrustLevelRegular {
		authorLevel := model.TrustLevelNew
		if at, err := repository.GetTrustTx(tx, authorID); err != nil {
			return false, err
		} else if at != nil {
			authorLevel = at.Level
		}
		if authorLevel == model.TrustLevelNew {
			return true, nil
		}
	}
	sum, err := repository.SumPendingFlagWeightTx(tx, postID)
	if err != nil {
		return false, err
	}
	return sum >= float32(keys.CommunityFlagHideThreshold.Get()), nil
}

func flagBase(level int16) float32 {
	if int(level) >= 0 && int(level) < len(flagBaseByLevel) {
		return flagBaseByLevel[level]
	}
	return flagBaseByLevel[len(flagBaseByLevel)-1]
}

func accuracyFactor(t *model.CommunityTrust) float32 {
	agreed, disagreed := nz(t.FlagsAgreed), nz(t.FlagsDisagreed)
	denom := agreed + disagreed
	if denom == 0 {
		return 1.0
	}
	return float32(agreed) / float32(denom)
}
