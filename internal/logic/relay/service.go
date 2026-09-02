package relay

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/iplocation"
	mailservice "github.com/yunloli/aiferry/internal/logic/mail"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/logic/user"
)

const maxRequestBody = 16 << 20

type sRelay struct {
	app        *app.Service
	usage      *usage.Service
	resilience *system.Service
	users      *user.Service
	prices     *pricingcache.Service
	mail       *mailservice.Service
	channels   *channel.Service
	locations  *iplocation.Service
}

type Candidate struct {
	ChannelModelID      uint64 `orm:"channel_model_id"`
	ChannelID           uint64 `orm:"channel_id"`
	ChannelName         string `orm:"channel_name"`
	ChannelType         string `orm:"channel_type"`
	BaseURL             string `orm:"base_url"`
	BackupBaseURLs      []string
	ChannelCredentialID uint64
	APIKeyCipher        string
	OrganizationID      string `orm:"organization_id"`
	ProjectID           string `orm:"project_id"`
	ProxyURLCipher      string `orm:"proxy_url_cipher"`
	DirectHTTP          bool   `json:"-" orm:"-"`
	AdvancedConfig      string `orm:"advanced_config"`
	Priority            int    `orm:"priority"`
	Weight              uint   `orm:"weight"`
	PublicName          string `orm:"public_name"`
	UpstreamName        string `orm:"upstream_name"`
	GroupIDs            []uint64
	ReasoningEffort     string `orm:"-"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type attemptResult struct {
	status             int
	body               []byte
	tokens             usage.TokenUsage
	firstTokenMs       *int64
	errorMessage       string
	latency            time.Duration
	headers            http.Header
	wroteBytes         bool
	timedOut           bool
	upstreamEndpoint   string
	protocolConversion string
	responseText       string
	responseModel      string
	streamCompleted    bool
}

func New(appSvc *app.Service, usageSvc *usage.Service, resilienceSvc *system.Service, userSvc *user.Service, priceCache *pricingcache.Service, mailSvc *mailservice.Service, channelSvc *channel.Service, locationSvc *iplocation.Service) *sRelay {
	return &sRelay{app: appSvc, usage: usageSvc, resilience: resilienceSvc, users: userSvc, prices: priceCache, mail: mailSvc, channels: channelSvc, locations: locationSvc}
}

func (s *sRelay) Models(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	modelColumns := dao.ChannelModels.Columns()
	rows := make([]struct {
		ChannelId  uint64 `orm:"channel_id"`
		PublicName string `orm:"public_name"`
	}, 0)
	err := dao.ChannelModels.Ctx(ctx).
		Fields(modelColumns.ChannelId, modelColumns.PublicName).
		Where(modelColumns.Enabled, 1).
		WhereNull(modelColumns.AutoDisabledAt).
		Scan(&rows)
	if err != nil {
		return ModelList{}, gerror.Wrap(err, "list public models")
	}
	channelIDs := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		channelIDs[row.ChannelId] = struct{}{}
	}
	activeChannels, err := activeRouteChannels(ctx, sortedRouteIDs(channelIDs))
	if err != nil {
		return ModelList{}, err
	}
	publicNames := make(map[string]struct{})
	for _, row := range rows {
		if _, active := activeChannels[row.ChannelId]; active {
			publicNames[row.PublicName] = struct{}{}
		}
	}
	names := make([]string, 0, len(publicNames))
	for name := range publicNames {
		names = append(names, name)
	}
	sort.Strings(names)
	models := make([]Model, 0, len(names))
	for _, name := range names {
		if len(key.AllowedModels) > 0 && !containsString(key.AllowedModels, name) {
			continue
		}
		candidates, routeErr := s.route(ctx, name, key)
		if routeErr != nil {
			return ModelList{}, routeErr
		}
		if len(candidates) > 0 {
			models = append(models, Model{ID: name, Object: "model", Created: 0, OwnedBy: "aiferry"})
		}
	}
	return ModelList{Object: "list", Data: models}, nil
}

func (s *sRelay) Handle(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, clientIP, gatewayHost, endpoint string, body []byte, key apikey.AuthKey) error {
	if len(body) > maxRequestBody {
		return gerror.New("request body exceeds 16 MiB")
	}
	if !gjson.ValidBytes(body) {
		return gerror.New("request body must be valid JSON")
	}
	securitySettings, err := s.resilience.GetSensitiveWordSettings(ctx)
	if err != nil {
		return err
	}
	body, incomingHeaders = redactGatewayRequest(body, incomingHeaders, gatewayHost, securitySettings)
	if err := s.resilience.CheckSensitivePrompt(ctx, endpoint, body); err != nil {
		return err
	}
	body, sensitiveDataRestorer, err := redactSensitiveDataWithRestore(body, securitySettings)
	if err != nil {
		return err
	}
	requestedModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestedModel == "" {
		return gerror.New("model is required")
	}
	isStream := gjson.GetBytes(body, "stream").Bool()
	if endpoint == "/chat/completions" && isStream {
		body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
	}
	if !keyAllowsModel(key, requestedModel) {
		return gerror.New("API key is not allowed to use model " + requestedModel)
	}
	candidates, err := s.route(ctx, requestedModel, key)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return gerror.Wrapf(ErrNoAvailableChannel, "no available channel for model %s", requestedModel)
	}
	if s.requiresBalanceCheck(requestedModel) {
		if err = s.users.CheckBalance(ctx, key.UserId); err != nil {
			return err
		}
	}
	requestID := newRequestID()
	startedAt := time.Now()
	settings, settingsErr := s.resilience.Get(ctx)
	if settingsErr != nil {
		settings = system.DefaultResilienceSettings()
	}
	var (
		last                attemptResult
		lastCandidate       Candidate
		attempts            int
		excludedCredentials = make(map[uint64]struct{})
	)
	for index := range candidates {
		for {
			outcome := s.attemptChannel(ctx, writer, incomingHeaders, endpoint, body, candidates[index], isStream, startedAt, key.UserId, key.Id, settings, excludedCredentials, sensitiveDataRestorer)
			attempts += outcome.attempts
			if outcome.attempts > 0 {
				last = outcome.result
				lastCandidate = outcome.candidate
			}
			if !outcome.handled {
				break
			}
			candidate := outcome.candidate
			result := outcome.result
			if !result.wroteBytes && s.missingBillableUsage(candidate, endpoint, result) {
				last = failedAttemptResult(result, ErrUpstreamUsageNotBillable.Error())
				lastCandidate = candidate
				s.maybeAutoDisable(ctx, settings, candidate, last)
				excludedCredentials[candidate.ChannelCredentialID] = struct{}{}
				continue
			}
			if recordErr := s.record(ctx, requestID, key, candidate, clientIP, endpoint, requestedModel, isStream, attempts, startedAt, result); recordErr != nil {
				if !result.wroteBytes && errors.Is(recordErr, ErrUpstreamUsageNotBillable) {
					last = failedAttemptResult(result, recordErr.Error())
					lastCandidate = candidate
					s.maybeAutoDisable(ctx, settings, candidate, last)
					excludedCredentials[candidate.ChannelCredentialID] = struct{}{}
					continue
				}
				if !isStream {
					s.writeBufferedResponse(writer, http.StatusPaymentRequired, openAIError("insufficient_balance", recordErr.Error()), http.Header{"Content-Type": []string{"application/json"}})
				}
				return nil
			}
			if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices && result.errorMessage == "" && !result.timedOut {
				s.resilience.ClearAutoDisableFailures(ctx, candidate.ChannelCredentialID)
				// 成功请求按上游响应速度加分：响应越快，模型健康分增长越多。
				_, _ = s.resilience.ApplyModelHealthScore(ctx, settings, system.ModelDisableInput{
					ChannelID: candidate.ChannelID,
					ModelID:   candidate.ChannelModelID,
					Source:    system.AutoDisableSourceRelayRequest,
					Status:    result.status,
					Latency:   result.latency,
				})
			}
			if !isStream {
				responseBody := result.body
				if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices {
					responseBody = sensitiveDataRestorer.restoreBufferedResponse(responseBody)
				}
				s.writeBufferedResponse(writer, result.status, responseBody, result.headers)
			}
			s.scheduleModelQualityAnalysis(ctx, requestID, candidate, requestedModel, endpoint, body, isStream, settings.ModelQualityDetectionEnabled, result)
			return nil
		}
	}
	if attempts > 0 {
		last = failedAttemptResult(last, "All eligible channels failed")
		if recordErr := s.record(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, isStream, attempts, startedAt, last); recordErr != nil {
			g.Log().Errorf(ctx, "record failed request %s: %v", requestID, recordErr)
		}
		if !last.wroteBytes && last.status >= http.StatusBadRequest && last.status < http.StatusInternalServerError && !retryableStatusForRules(last.status, settings.RetryStatusCodes) {
			s.writeBufferedResponse(writer, last.status, sensitiveDataRestorer.restoreBufferedResponse(last.body), last.headers)
			return nil
		}
	} else {
		last = failedAttemptResult(last, "All eligible channels failed")
	}
	return gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible channels failed")
}
