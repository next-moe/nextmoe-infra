package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/telemetry/alert"
	"api/internal/platform/telemetry/dto"
	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

func (s *AdminServer) registerAlerts(api huma.API) {
	tags := []string{"telemetry-admin"}
	huma.Register(api, huma.Operation{OperationID: "listTelemetryAlertChannels", Method: http.MethodGet, Path: "/api/v1/admin/telemetry/alert-channels",
		Summary: "List telemetry alert channels", Tags: tags}, s.listAlertChannels)
	huma.Register(api, huma.Operation{OperationID: "createTelemetryAlertChannel", Method: http.MethodPost, Path: "/api/v1/admin/telemetry/alert-channels",
		Summary: "Create an alert channel", Tags: tags}, s.createAlertChannel)
	huma.Register(api, huma.Operation{OperationID: "updateTelemetryAlertChannel", Method: http.MethodPatch, Path: "/api/v1/admin/telemetry/alert-channels/{id}",
		Summary: "Update an alert channel", Tags: tags}, s.updateAlertChannel)
	huma.Register(api, huma.Operation{OperationID: "deleteTelemetryAlertChannel", Method: http.MethodDelete, Path: "/api/v1/admin/telemetry/alert-channels/{id}",
		Summary: "Delete an alert channel", Tags: tags}, s.deleteAlertChannel)
	huma.Register(api, huma.Operation{OperationID: "testTelemetryAlertChannel", Method: http.MethodPost, Path: "/api/v1/admin/telemetry/alert-channels/{id}/test",
		Summary: "Send a test email to an alert channel", Tags: tags}, s.testAlertChannel)
	huma.Register(api, huma.Operation{OperationID: "listTelemetryAlerts", Method: http.MethodGet, Path: "/api/v1/admin/telemetry/alerts",
		Summary: "List telemetry alerts, newest first", Tags: tags}, s.listAlerts)
}

type listAlertChannelsInput struct{}
type listAlertChannelsOutput struct {
	Body Envelope[[]dto.AlertChannelView]
}

func (s *AdminServer) listAlertChannels(ctx context.Context, _ *listAlertChannelsInput) (*listAlertChannelsOutput, error) {
	rows, err := s.store.ListAlertChannels(ctx)
	if err != nil {
		return nil, mapAdminErr("list alert channels", err)
	}
	out := make([]dto.AlertChannelView, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.AlertChannelViewFrom(r))
	}
	return &listAlertChannelsOutput{Body: okEnvelope(out)}, nil
}

type createAlertChannelInput struct {
	Body dto.CreateAlertChannelRequest
}
type createAlertChannelOutput struct {
	Body Envelope[dto.AlertChannelView]
}

func (s *AdminServer) createAlertChannel(ctx context.Context, in *createAlertChannelInput) (*createAlertChannelOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	kind := in.Body.Kind
	if kind == "" {
		kind = model.AlertKindEmail
	}
	if kind != model.AlertKindEmail {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "kind must be email")
	}
	target, err := model.ParseEmailTarget(in.Body.Target)
	if err != nil {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, err.Error())
	}
	enabled := true
	if in.Body.Enabled != nil {
		enabled = *in.Body.Enabled
	}
	row, err := s.store.CreateAlertChannel(ctx, kind, target, enabled)
	if err != nil {
		return nil, mapAdminErr("create alert channel", err)
	}
	return &createAlertChannelOutput{Body: okEnvelope(dto.AlertChannelViewFrom(*row))}, nil
}

type updateAlertChannelInput struct {
	ID   int64 `path:"id"`
	Body dto.UpdateAlertChannelRequest
}
type updateAlertChannelOutput struct {
	Body Envelope[dto.AlertChannelView]
}

func (s *AdminServer) updateAlertChannel(ctx context.Context, in *updateAlertChannelInput) (*updateAlertChannelOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	var target *string
	if in.Body.Target != nil {
		parsed, err := model.ParseEmailTarget(*in.Body.Target)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, err.Error())
		}
		target = &parsed
	}
	row, err := s.store.UpdateAlertChannel(ctx, in.ID, target, in.Body.Enabled)
	if err != nil {
		return nil, mapAdminErr("update alert channel", err)
	}
	return &updateAlertChannelOutput{Body: okEnvelope(dto.AlertChannelViewFrom(*row))}, nil
}

type deleteAlertChannelInput struct {
	ID int64 `path:"id"`
}
type deleteAlertChannelOutput struct {
	Body Envelope[map[string]any]
}

func (s *AdminServer) deleteAlertChannel(ctx context.Context, in *deleteAlertChannelInput) (*deleteAlertChannelOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	if err := s.store.DeleteAlertChannel(ctx, in.ID); err != nil {
		return nil, mapAdminErr("delete alert channel", err)
	}
	return &deleteAlertChannelOutput{Body: okEnvelope(map[string]any{})}, nil
}

type testAlertChannelInput struct {
	ID int64 `path:"id"`
}
type testAlertChannelOutput struct {
	Body Envelope[dto.AlertChannelTestResult]
}

func (s *AdminServer) testAlertChannel(ctx context.Context, in *testAlertChannelInput) (*testAlertChannelOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	row, err := s.store.AlertChannelByID(ctx, in.ID)
	if err != nil {
		return nil, mapAdminErr("test alert channel", err)
	}
	if s.notifier == nil {
		return &testAlertChannelOutput{Body: okEnvelope(dto.AlertChannelTestResult{OK: false, Error: "mail not configured"})}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	n := alert.Notification{
		To:      row.Target,
		Subject: "[NextMoe 监测] 测试",
		Heading: "告警测试",
		HTML:    `<p style="margin:0 0 10px; font-size:14px; color:#3e4c59;">这是一封监测告警测试邮件。</p>`,
	}
	if err := s.notifier.Send(ctx, n); err != nil {
		return &testAlertChannelOutput{Body: okEnvelope(dto.AlertChannelTestResult{OK: false, Error: err.Error()})}, nil
	}
	return &testAlertChannelOutput{Body: okEnvelope(dto.AlertChannelTestResult{OK: true, Error: ""})}, nil
}

type listAlertsInput struct {
	// huma panics on pointer query params; app_id 0 is engine alerts, so omit = empty string.
	AppID  string `query:"app_id"`
	Status string `query:"status"`
	Limit  int    `query:"limit" default:"50"`
	Offset int    `query:"offset"`
}
type listAlertsOutput struct {
	Body Envelope[[]dto.AlertView]
}

func (s *AdminServer) listAlerts(ctx context.Context, in *listAlertsInput) (*listAlertsOutput, error) {
	p := store.ListAlertsParams{Status: in.Status, Limit: in.Limit, Offset: in.Offset}
	if raw := strings.TrimSpace(in.AppID); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "app_id must be an integer")
		}
		p.AppID = &id
	}
	rows, err := s.store.ListAlerts(ctx, p)
	if err != nil {
		return nil, mapAdminErr("list alerts", err)
	}
	out := make([]dto.AlertView, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.AlertViewFrom(r))
	}
	return &listAlertsOutput{Body: okEnvelope(out)}, nil
}
