package system

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

const (
	defaultModelQualityEventsPageSize = 50
	maxModelQualityEventsPageSize     = 200
	maxStoredModelQualityEvents       = 999
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

type modelQualityEventRow struct {
	Id              uint64    `orm:"id"`
	ChannelId       uint64    `orm:"channel_id"`
	ChannelName     string    `orm:"channel_name"`
	CredentialId    uint64    `orm:"credential_id"`
	CredentialIndex uint      `orm:"credential_index"`
	APIKeyName      string    `orm:"api_key_name"`
	RequestedModel  string    `orm:"requested_model"`
	ExpectedModel   string    `orm:"expected_model"`
	ObservedModel   string    `orm:"observed_model"`
	ReasonsJson     string    `orm:"reasons_json"`
	QuestionChars   uint      `orm:"question_chars"`
	AnswerChars     uint      `orm:"answer_chars"`
	CreatedAt       time.Time `orm:"created_at"`
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
	if err != nil {
		return gerror.Wrap(err, "record model quality event")
	}
	return s.trimModelQualityEvents(ctx)
}

func (s *sSystem) trimModelQualityEvents(ctx context.Context) error {
	columns := dao.ModelQualityEvents.Columns()
	cutoff := modelQualityEventRow{}
	if err := dao.ModelQualityEvents.Ctx(ctx).
		Fields(columns.Id).
		OrderDesc(columns.Id).
		Limit(1, maxStoredModelQualityEvents).
		Scan(&cutoff); err != nil {
		return gerror.Wrap(err, "select model quality retention cutoff")
	}
	if cutoff.Id == 0 {
		return nil
	}
	_, err := dao.ModelQualityEvents.Ctx(ctx).WhereLTE(columns.Id, cutoff.Id).Delete()
	return gerror.Wrap(err, "trim model quality events")
}

func (s *sSystem) ListModelQualityEvents(ctx context.Context, input adminapi.ModelQualityEventsInput) (adminapi.ModelQualityEventList, error) {
	page, pageSize := normalizeModelQualityEventsPage(input)
	model := dao.ModelQualityEvents.Ctx(ctx).As("e")
	total, err := model.Count()
	if err != nil {
		return adminapi.ModelQualityEventList{}, gerror.Wrap(err, "count model quality events")
	}
	credentialIndexField := "(SELECT COUNT(*) FROM " + dao.ChannelCredentials.Table() + " cc WHERE cc.channel_id=e.channel_id AND cc.id<=e.credential_id) AS credential_index"
	rows := make([]modelQualityEventRow, 0)
	if err = model.Fields("e.*,COALESCE(c.name,'已删除渠道') AS channel_name,COALESCE(api_key.name,'未记录访问密钥') AS api_key_name,"+credentialIndexField).
		LeftJoin(dao.Channels.Table()+" c", "c.id=e.channel_id").
		LeftJoin(dao.UsageLogs.Table()+" u", "u.request_id=e.request_id").
		LeftJoin(dao.ApiKeys.Table()+" api_key", "api_key.id=u.api_key_id").
		OrderDesc("e.id").Page(page, pageSize).Scan(&rows); err != nil {
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

func modelQualityEventView(row modelQualityEventRow) adminapi.ModelQualityEventView {
	reasons := make([]string, 0)
	_ = json.Unmarshal([]byte(row.ReasonsJson), &reasons)
	return adminapi.ModelQualityEventView{
		Id: row.Id, ChannelId: row.ChannelId, ChannelName: row.ChannelName, CredentialId: row.CredentialId, CredentialIndex: row.CredentialIndex, APIKeyName: row.APIKeyName,
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
