package handler

import (
	"context"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"
)

type activitySettingInput struct {
	ID int64 `path:"id" minimum:"1"`
}
type setActivitySettingInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body dto.ActivitySettingRequest
}
type activitySettingOutput struct {
	Body Envelope[dto.ActivitySettingView]
}

func (s *Server) getActivitySetting(ctx context.Context, in *activitySettingInput) (*activitySettingOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	st, err := s.activities.Setting(in.ID)
	if err != nil {
		return nil, mapErr("get activity setting", err)
	}
	return activitySetting(in.ID, st), nil
}

func (s *Server) setActivitySetting(ctx context.Context, in *setActivitySettingInput) (*activitySettingOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	st, err := s.activities.SetHidden(ctx, in.ID, in.Body.Hidden)
	if err != nil {
		return nil, mapErr("set activity setting", err)
	}
	return activitySetting(in.ID, st), nil
}

func activitySetting(userID int64, st service.ActivitySetting) *activitySettingOutput {
	return &activitySettingOutput{Body: okEnvelope(dto.ActivitySettingView{
		UserID: userID, Hidden: st.Hidden, UpdatedAt: st.UpdatedAt,
	})}
}
