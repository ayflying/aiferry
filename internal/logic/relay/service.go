package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/yunloli/aiferry/internal/logic/channeltype"
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
	types      *channeltype.Service
	locations  *iplocation.Service
	// slots 按「渠道 × 密钥」统计在途转发请求，实现渠道的并发限制。
	slots *keySlots
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
	// ManagementKeyCipher 是渠道管理密钥密文，供 authType=management_key 的
	// 渠道类型使用；与渠道类型声明的鉴权规则一起交给渠道层统一处理。
	ManagementKeyCipher string
	OrganizationID      string `orm:"organization_id"`
	ProjectID           string `orm:"project_id"`
	ProxyURLCipher      string `orm:"proxy_url_cipher"`
	DirectHTTP          bool   `json:"-" orm:"-"`
	AdvancedConfig      string `orm:"advanced_config"`
	Priority            int    `orm:"priority"`
	Weight              uint   `orm:"weight"`
	PublicName          string `orm:"public_name"`
	UpstreamName        string `orm:"upstream_name"`
	// ClosedWindow 是该渠道模型的定时关闭时段原始 JSON。关闭判定随时间变化，
	// 因此缓存里保存原值，由每次请求实时判定是否落在关闭时段内。
	ClosedWindow string `json:"closedWindow,omitempty"`
	// ConcurrencyLimit 是该渠道每把上游密钥的转发并发上限，0 表示不限制。
	// 它是渠道高级配置项，随路由缓存一起传递；渠道写操作会递增路由版本号，
	// 因此修改后无需等待缓存过期即生效。
	ConcurrencyLimit int `json:"concurrencyLimit,omitempty"`
	GroupIDs         []uint64
	ReasoningEffort  string `orm:"-"`
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
	attemptFlow        []usage.AttemptFlowStep
	// reasoningContent / reasoningField / reasoningToolCallIDs 是上游本轮返回的思考内容、
	// 其字段名（方言）以及绑定的工具调用 id，用于按需回传给要求回传思考内容的 thinking 模式上游。
	reasoningContent     string
	reasoningField       string
	reasoningToolCallIDs []string
}

func New(appSvc *app.Service, usageSvc *usage.Service, resilienceSvc *system.Service, userSvc *user.Service, priceCache *pricingcache.Service, mailSvc *mailservice.Service, channelSvc *channel.Service, channelTypeSvc *channeltype.Service, locationSvc *iplocation.Service) *sRelay {
	return &sRelay{app: appSvc, usage: usageSvc, resilience: resilienceSvc, users: userSvc, prices: priceCache, mail: mailSvc, channels: channelSvc, types: channelTypeSvc, locations: locationSvc, slots: newKeySlots()}
}

// modelsListCacheKey 复用历史键名 aiferry:models:list 并嵌入路由版本号：
// 所有渠道/模型/分组写路径都会递增 aiferry:routes:version，版本号变化后
// 旧列表键自然失效。短 TTL 兜底防止版本号丢失。
const modelsListCacheTTL = 60 * time.Second

func (s *sRelay) Models(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	version := s.routeCacheVersion(ctx)
	cacheKey := fmt.Sprintf("aiferry:models:list:%d:%d", key.Id, version)
	if cached, err := s.app.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var list ModelList
		if json.Unmarshal(cached, &list) == nil {
			return list, nil
		}
	}
	list, err := s.computeModels(ctx, key)
	if err != nil {
		return ModelList{}, err
	}
	if encoded, err := json.Marshal(list); err == nil {
		_ = s.app.Redis.Set(ctx, cacheKey, encoded, modelsListCacheTTL).Err()
	}
	return list, nil
}

// computeModels 逐模型解析可用渠道候选，得出该密钥可见的模型列表。
// routeCached 命中缓存时每个模型只消耗 1 次 Redis 读，无数据库查询。
func (s *sRelay) computeModels(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
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
		candidates, routeErr := s.routeCached(ctx, name, key)
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
	if endpoint == "/chat/completions" {
		// 客户端重建历史时只保留 role/content/tool_calls，会丢掉非标准的 reasoning_content，
		// 而 DeepSeek/Kimi 等 thinking 模式上游要求发生过工具调用后必须原样回传。这里按
		// tool_call id 补回本网关存档的思考内容，没有存档时保持原样。
		body = s.restoreReasoningContent(ctx, body, key.Id)
	}
	if !keyAllowsModel(key, requestedModel) {
		return gerror.New("API key is not allowed to use model " + requestedModel)
	}
	candidates, err := s.routeCached(ctx, requestedModel, key)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		// 无可用渠道属于无痕失败（尚未进入转发循环，不会写用量），必须留日志便于排障。
		g.Log().Warningf(ctx, "relay %s: no available channel for model %s (model auto-disabled, channel inactive, or group policy filtered)", clientIP, requestedModel)
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
		attemptFlow         []usage.AttemptFlowStep
		excludedCredentials = make(map[uint64]struct{})
	)
	for index := range candidates {
		for {
			outcome := s.attemptChannel(ctx, writer, incomingHeaders, endpoint, body, candidates[index], isStream, key.UserId, key.Id, settings, excludedCredentials, sensitiveDataRestorer)
			if outcome.concurrencyExhausted {
				// 该渠道每把密钥的并发额度都占满，且等待窗口内没有腾出空位。
				// 这是网关本地限流而非上游故障：不写用量、不参与渠道失败评分
				// （默认禁用状态码含 429），直接以 429 让客户端稍后重试。
				channelName := candidates[index].ChannelName
				g.Log().Warningf(ctx, "relay %s: channel %s (#%d) key concurrency exhausted for model %s", clientIP, channelName, candidates[index].ChannelID, requestedModel)
				return gerror.Wrapf(ErrChannelConcurrencyExhausted, "channel %s (#%d) key concurrency exhausted", channelName, candidates[index].ChannelID)
			}
			attempts += outcome.attempts
			if outcome.attempts > 0 {
				last = outcome.result
				lastCandidate = outcome.candidate
				attemptFlow = append(attemptFlow, outcome.result.attemptFlow...)
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
			result.attemptFlow = attemptFlow
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
				// 流式响应被客户端中断时不存档半截思考内容，避免下一轮回传出残缺的推理。
				if !isStream || result.streamCompleted {
					s.rememberReasoningContent(ctx, key.Id, result)
				}
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
		last.attemptFlow = attemptFlow
		if recordErr := s.record(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, isStream, attempts, startedAt, last); recordErr != nil {
			g.Log().Errorf(ctx, "record failed request %s: %v", requestID, recordErr)
		}
		if !last.wroteBytes && last.status >= http.StatusBadRequest && last.status < http.StatusInternalServerError && !retryableStatusForRules(last.status, settings.RetryStatusCodes) {
			s.writeBufferedResponse(writer, last.status, sensitiveDataRestorer.restoreBufferedResponse(last.body), last.headers)
			return nil
		}
	} else {
		last = failedAttemptResult(last, "All eligible channels failed")
		// attempts==0 意味着所有候选渠道在选凭证阶段就被跳过（凭证冷却或无可用密钥），
		// 请求从未到达上游、也不会出现在用量列表里。这里补一条用量记录（503）+ WARN 日志，
		// 避免客户端看到 503 而管理端查无此请求。渠道信息取第一个候选（仅为落库展示），
		// 计费按未定价处理，不会扣费。
		last.attemptFlow = attemptFlow
		last.status = http.StatusServiceUnavailable
		last.body = openAIError("server_error", retryableAvailabilityMessage)
		lastCandidate = candidates[0]
		lastCandidate.ChannelCredentialID = 0
		lastCandidate.APIKeyCipher = ""
		requestID := newRequestID()
		startedAt := time.Now()
		skippedBy := s.summarizeCredentialSkips(ctx, candidates)
		last.errorMessage = "全部候选渠道凭证不可用：" + skippedBy
		if recordErr := s.record(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, isStream, 0, startedAt, last); recordErr != nil {
			g.Log().Errorf(ctx, "record no-attempt request %s: %v", requestID, recordErr)
		}
		g.Log().Warningf(ctx, "relay %s: request rejected before any upstream attempt (model %s, candidates %d, skip reasons: %s)", clientIP, requestedModel, len(candidates), skippedBy)
	}
	return gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible channels failed")
}
