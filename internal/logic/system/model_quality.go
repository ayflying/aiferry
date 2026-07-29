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
	"github.com/yunloli/aiferry/internal/model/entity"
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
	RequestId       string    `orm:"request_id"`
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

type modelQualityEventReferences struct {
	channelNames      map[uint64]string
	credentialIndexes map[uint64]uint
	requestAPIKeyIDs  map[string]uint64
	apiKeyNames       map[uint64]string
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
	offset, limit := modelQualityRetentionCutoffWindow()
	if err := dao.ModelQualityEvents.Ctx(ctx).
		Fields(columns.Id).
		OrderDesc(columns.Id).
		Limit(offset, limit).
		Scan(&cutoff); err != nil {
		return gerror.Wrap(err, "select model quality retention cutoff")
	}
	if cutoff.Id == 0 {
		return nil
	}
	_, err := dao.ModelQualityEvents.Ctx(ctx).WhereLTE(columns.Id, cutoff.Id).Delete()
	return gerror.Wrap(err, "trim model quality events")
}

func modelQualityRetentionCutoffWindow() (offset, limit int) {
	return maxStoredModelQualityEvents, 1
}

func (s *sSystem) ListModelQualityEvents(ctx context.Context, input adminapi.ModelQualityEventsInput) (adminapi.ModelQualityEventList, error) {
	page, pageSize := normalizeModelQualityEventsPage(input)
	eventColumns := dao.ModelQualityEvents.Columns()
	total, err := dao.ModelQualityEvents.Ctx(ctx).Count()
	if err != nil {
		return adminapi.ModelQualityEventList{}, gerror.Wrap(err, "count model quality events")
	}
	rows := make([]modelQualityEventRow, 0)
	if err = dao.ModelQualityEvents.Ctx(ctx).
		OrderDesc(eventColumns.Id).Page(page, pageSize).Scan(&rows); err != nil {
		return adminapi.ModelQualityEventList{}, gerror.Wrap(err, "list model quality events")
	}
	if err = s.populateModelQualityEventReferences(ctx, rows); err != nil {
		return adminapi.ModelQualityEventList{}, err
	}
	items := make([]adminapi.ModelQualityEventView, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelQualityEventView(row))
	}
	return adminapi.ModelQualityEventList{Items: items, Total: total}, nil
}

func (s *sSystem) populateModelQualityEventReferences(ctx context.Context, rows []modelQualityEventRow) error {
	if len(rows) == 0 {
		return nil
	}
	references := modelQualityEventReferences{
		channelNames:      make(map[uint64]string),
		credentialIndexes: make(map[uint64]uint),
		requestAPIKeyIDs:  make(map[string]uint64),
		apiKeyNames:       make(map[uint64]string),
	}
	channelIDSet := make(map[uint64]struct{})
	requestIDSet := make(map[string]struct{})
	for _, row := range rows {
		if row.ChannelId > 0 {
			channelIDSet[row.ChannelId] = struct{}{}
		}
		if row.RequestId != "" {
			requestIDSet[row.RequestId] = struct{}{}
		}
	}
	channelIDs := modelQualityReferenceIDs(channelIDSet)
	requestIDs := modelQualityReferenceStrings(requestIDSet)
	if err := s.loadModelQualityChannels(ctx, channelIDs, &references); err != nil {
		return err
	}
	if err := s.loadModelQualityCredentialIndexes(ctx, channelIDs, &references); err != nil {
		return err
	}
	if err := s.loadModelQualityAPIKeyNames(ctx, requestIDs, &references); err != nil {
		return err
	}
	applyModelQualityEventReferences(rows, references)
	return nil
}

func (s *sSystem) loadModelQualityChannels(ctx context.Context, channelIDs []uint64, references *modelQualityEventReferences) error {
	if len(channelIDs) == 0 {
		return nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	if err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		WhereIn(columns.Id, channelIDs).
		Scan(&channels); err != nil {
		return gerror.Wrap(err, "load model quality channels")
	}
	for _, channel := range channels {
		references.channelNames[channel.Id] = channel.Name
	}
	return nil
}

func (s *sSystem) loadModelQualityCredentialIndexes(ctx context.Context, channelIDs []uint64, references *modelQualityEventReferences) error {
	if len(channelIDs) == 0 {
		return nil
	}
	columns := dao.ChannelCredentials.Columns()
	credentials := make([]entity.ChannelCredentials, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).Unscoped().
		Fields(columns.Id, columns.ChannelId).
		WhereIn(columns.ChannelId, channelIDs).
		OrderAsc(columns.ChannelId).
		OrderAsc(columns.Id).
		Scan(&credentials); err != nil {
		return gerror.Wrap(err, "load model quality credential indexes")
	}
	indexes := make(map[uint64]uint)
	for _, credential := range credentials {
		indexes[credential.ChannelId]++
		references.credentialIndexes[credential.Id] = indexes[credential.ChannelId]
	}
	return nil
}

func (s *sSystem) loadModelQualityAPIKeyNames(ctx context.Context, requestIDs []string, references *modelQualityEventReferences) error {
	if len(requestIDs) == 0 {
		return nil
	}
	usageColumns := dao.UsageLogs.Columns()
	usageLogs := make([]entity.UsageLogs, 0, len(requestIDs))
	if err := dao.UsageLogs.Ctx(ctx).
		Fields(usageColumns.Id, usageColumns.RequestId, usageColumns.ApiKeyId).
		WhereIn(usageColumns.RequestId, requestIDs).
		OrderDesc(usageColumns.Id).
		Scan(&usageLogs); err != nil {
		return gerror.Wrap(err, "load model quality usage logs")
	}
	apiKeyIDSet := make(map[uint64]struct{})
	for _, usageLog := range usageLogs {
		if _, exists := references.requestAPIKeyIDs[usageLog.RequestId]; exists {
			continue
		}
		references.requestAPIKeyIDs[usageLog.RequestId] = usageLog.ApiKeyId
		if usageLog.ApiKeyId > 0 {
			apiKeyIDSet[usageLog.ApiKeyId] = struct{}{}
		}
	}
	apiKeyIDs := modelQualityReferenceIDs(apiKeyIDSet)
	if len(apiKeyIDs) == 0 {
		return nil
	}
	apiKeyColumns := dao.ApiKeys.Columns()
	apiKeys := make([]entity.ApiKeys, 0, len(apiKeyIDs))
	if err := dao.ApiKeys.Ctx(ctx).
		Fields(apiKeyColumns.Id, apiKeyColumns.Name).
		WhereIn(apiKeyColumns.Id, apiKeyIDs).
		Scan(&apiKeys); err != nil {
		return gerror.Wrap(err, "load model quality API keys")
	}
	for _, apiKey := range apiKeys {
		references.apiKeyNames[apiKey.Id] = apiKey.Name
	}
	return nil
}

func applyModelQualityEventReferences(rows []modelQualityEventRow, references modelQualityEventReferences) {
	for index := range rows {
		row := &rows[index]
		row.ChannelName = references.channelNames[row.ChannelId]
		if row.ChannelName == "" {
			row.ChannelName = "已删除渠道"
		}
		row.CredentialIndex = references.credentialIndexes[row.CredentialId]
		row.APIKeyName = references.apiKeyNames[references.requestAPIKeyIDs[row.RequestId]]
		if row.APIKeyName == "" {
			row.APIKeyName = "未记录访问密钥"
		}
	}
}

func modelQualityReferenceIDs(values map[uint64]struct{}) []uint64 {
	result := make([]uint64, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}

func modelQualityReferenceStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
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
