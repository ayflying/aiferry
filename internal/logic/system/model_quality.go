package system

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	defaultModelQualityEventsPageSize = 50
	maxModelQualityEventsPageSize     = 200
)

type ModelQualityEventInput struct {
	RequestID      string
	ChannelID      uint64
	CredentialID   uint64
	RequestedModel string
	ExpectedModel  string
	ObservedModel  string
	Reasons        []string
	QuestionChars  uint
	AnswerChars    uint
}

func (s *sSystem) GetModelQualitySettings(ctx context.Context) (adminapi.ModelQualitySettingsInput, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return adminapi.ModelQualitySettingsInput{}, err
	}
	return adminapi.ModelQualitySettingsInput{Enabled: settings.ModelQualityDetectionEnabled}, nil
}

func (s *sSystem) UpdateModelQualitySettings(ctx context.Context, input adminapi.ModelQualitySettingsInput) (adminapi.ModelQualitySettingsInput, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return adminapi.ModelQualitySettingsInput{}, err
	}
	settings.ModelQualityDetectionEnabled = input.Enabled
	if _, err = s.Update(ctx, settings); err != nil {
		return adminapi.ModelQualitySettingsInput{}, err
	}
	return adminapi.ModelQualitySettingsInput{Enabled: input.Enabled}, nil
}

func (s *sSystem) RecordModelQualityEvent(ctx context.Context, input ModelQualityEventInput) error {
	reasons := uniqueModelQualityReasons(input.Reasons)
	if len(reasons) == 0 {
		return nil
	}
	encoded, err := json.Marshal(reasons)
	if err != nil {
		return gerror.Wrap(err, "encode model quality event reasons")
	}
	_, err = dao.ModelQualityEvents.Ctx(ctx).Data(do.ModelQualityEvents{
		RequestId:      input.RequestID,
		ChannelId:      input.ChannelID,
		CredentialId:   input.CredentialID,
		RequestedModel: input.RequestedModel,
		ExpectedModel:  input.ExpectedModel,
		ObservedModel:  input.ObservedModel,
		ReasonsJson:    string(encoded),
		QuestionChars:  input.QuestionChars,
		AnswerChars:    input.AnswerChars,
	}).Insert()
	return gerror.Wrap(err, "record model quality event")
}

func (s *sSystem) ListModelQualityEvents(ctx context.Context, input adminapi.ModelQualityEventsInput) (adminapi.ModelQualityEventList, error) {
	page, pageSize := normalizeModelQualityEventsPage(input)
	model := dao.ModelQualityEvents.Ctx(ctx).OrderDesc(dao.ModelQualityEvents.Columns().Id)
	total, err := model.Count()
	if err != nil {
		return adminapi.ModelQualityEventList{}, gerror.Wrap(err, "count model quality events")
	}
	rows := make([]entity.ModelQualityEvents, 0)
	if err = model.Page(page, pageSize).Scan(&rows); err != nil {
		return adminapi.ModelQualityEventList{}, gerror.Wrap(err, "list model quality events")
	}
	items := make([]adminapi.ModelQualityEventView, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelQualityEventView(row))
	}
	return adminapi.ModelQualityEventList{Items: items, Total: total}, nil
}

func normalizeModelQualityEventsPage(input adminapi.ModelQualityEventsInput) (int, int) {
	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = defaultModelQualityEventsPageSize
	}
	if pageSize > maxModelQualityEventsPageSize {
		pageSize = maxModelQualityEventsPageSize
	}
	return page, pageSize
}

func modelQualityEventView(row entity.ModelQualityEvents) adminapi.ModelQualityEventView {
	reasons := make([]string, 0)
	_ = json.Unmarshal([]byte(row.ReasonsJson), &reasons)
	return adminapi.ModelQualityEventView{
		Id: row.Id, ChannelId: row.ChannelId, CredentialId: row.CredentialId,
		RequestedModel: row.RequestedModel, ExpectedModel: row.ExpectedModel, ObservedModel: row.ObservedModel,
		Reasons: uniqueModelQualityReasons(reasons), QuestionChars: row.QuestionChars, AnswerChars: row.AnswerChars, CreatedAt: row.CreatedAt,
	}
}

func uniqueModelQualityReasons(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		reason := strings.TrimSpace(value)
		if reason == "" {
			continue
		}
		if _, exists := seen[reason]; exists {
			continue
		}
		seen[reason] = struct{}{}
		result = append(result, reason)
	}
	return result
}
