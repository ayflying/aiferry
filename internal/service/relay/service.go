package relay

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/channel"
	mailservice "github.com/yunloli/aiferry/internal/service/mail"
	"github.com/yunloli/aiferry/internal/service/pricingcache"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
	"github.com/yunloli/aiferry/internal/service/user"
)

const maxRequestBody = 16 << 20

type Service struct {
	app        *app.Service
	usage      *usage.Service
	resilience *system.Service
	users      *user.Service
	prices     *pricingcache.Service
	mail       *mailservice.Service
	channels   *channel.Service
}

type Candidate struct {
	ChannelModelID uint64 `orm:"channel_model_id"`
	ChannelID      uint64 `orm:"channel_id"`
	ChannelName    string `orm:"channel_name"`
	BaseURL        string `orm:"base_url"`
	APIKeyCipher   string `orm:"api_key_cipher"`
	OrganizationID string `orm:"organization_id"`
	ProjectID      string `orm:"project_id"`
	ProxyURLCipher string `orm:"proxy_url_cipher"`
	AdvancedConfig string `orm:"advanced_config"`
	Priority       int    `orm:"priority"`
	Weight         uint   `orm:"weight"`
	PublicName     string `orm:"public_name"`
	UpstreamName   string `orm:"upstream_name"`
	GroupIDs       []uint64
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
	status       int
	body         []byte
	tokens       usage.TokenUsage
	firstTokenMs *int64
	errorMessage string
	latency      time.Duration
	headers      http.Header
}

func New(appSvc *app.Service, usageSvc *usage.Service, resilienceSvc *system.Service, userSvc *user.Service, priceCache *pricingcache.Service, mailSvc *mailservice.Service, channelSvc *channel.Service) *Service {
	return &Service{app: appSvc, usage: usageSvc, resilience: resilienceSvc, users: userSvc, prices: priceCache, mail: mailSvc, channels: channelSvc}
}

func (s *Service) Models(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	var rows []struct {
		PublicName string `orm:"public_name"`
	}
	err := dao.ChannelModels.Ctx(ctx).As("m").
		Fields("DISTINCT m.public_name").
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id AND c.status=1").
		Where("m.enabled", 1).
		OrderAsc("m.public_name").
		Scan(&rows)
	if err != nil {
		return ModelList{}, gerror.Wrap(err, "list public models")
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		if len(key.AllowedModels) > 0 && !containsString(key.AllowedModels, row.PublicName) {
			continue
		}
		if !s.prices.IsPriced(row.PublicName) {
			continue
		}
		candidates, routeErr := s.route(ctx, row.PublicName, key)
		if routeErr != nil {
			return ModelList{}, routeErr
		}
		if len(candidates) > 0 {
			models = append(models, Model{ID: row.PublicName, Object: "model", Created: 0, OwnedBy: "aiferry"})
		}
	}
	return ModelList{Object: "list", Data: models}, nil
}

func (s *Service) Handle(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, key apikey.AuthKey) error {
	if len(body) > maxRequestBody {
		return gerror.New("request body exceeds 16 MiB")
	}
	if !gjson.ValidBytes(body) {
		return gerror.New("request body must be valid JSON")
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
		return gerror.New("no available channel for model " + requestedModel)
	}
	if !s.prices.IsPriced(requestedModel) {
		return gerror.New("当前模型未配置可用价格，无法计费")
	}
	if err = s.users.CheckBalance(ctx, key.UserId); err != nil {
		return err
	}
	requestID := newRequestID()
	startedAt := time.Now()
	settings, settingsErr := s.resilience.Get(ctx)
	if settingsErr != nil {
		settings = system.DefaultResilienceSettings()
	}
	maxAttempts := min(len(candidates), settings.MaxFailoverAttempts)
	var last attemptResult
	for index := 0; index < maxAttempts; index++ {
		candidate := candidates[index]
		attemptStartedAt := time.Now()
		attemptWriter := writer
		if !isStream {
			attemptWriter = nil
		}
		result, handled, attemptErr := s.attempt(ctx, attemptWriter, incomingHeaders, endpoint, body, candidate, isStream, startedAt, settings.RetryStatusCodes)
		result.latency = time.Since(attemptStartedAt)
		last = result
		if attemptErr != nil {
			last.errorMessage = attemptErr.Error()
			s.maybeAutoDisable(ctx, settings, candidate, last)
			s.markFailure(ctx, candidate.ChannelID)
			if index+1 < maxAttempts {
				continue
			}
			break
		} else {
			s.maybeAutoDisable(ctx, settings, candidate, result)
		}
		if handled {
			if result.status >= 200 && result.status < 300 {
				s.markSuccess(ctx, candidate.ChannelID)
			}
			if recordErr := s.record(ctx, requestID, key, candidate, endpoint, requestedModel, isStream, index+1, startedAt, result); recordErr != nil {
				if !isStream {
					s.writeBufferedResponse(writer, http.StatusPaymentRequired, openAIError("insufficient_balance", recordErr.Error()), http.Header{"Content-Type": []string{"application/json"}})
				}
				return nil
			}
			if !isStream {
				s.writeBufferedResponse(writer, result.status, result.body, result.headers)
			}
			return nil
		}
		if retryableStatusForRules(result.status, settings.RetryStatusCodes) && index+1 < maxAttempts {
			s.markFailure(ctx, candidate.ChannelID)
			continue
		}
		s.writeBufferedResponse(writer, result.status, result.body, result.headers)
		_ = s.record(ctx, requestID, key, candidate, endpoint, requestedModel, isStream, index+1, startedAt, result)
		return nil
	}
	if last.status == 0 {
		last.status = http.StatusBadGateway
		last.body = openAIError("upstream_error", "All eligible channels failed")
	}
	s.writeBufferedResponse(writer, last.status, last.body, http.Header{"Content-Type": []string{"application/json"}})
	return nil
}

func (s *Service) attempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, originalBody []byte, candidate Candidate, stream bool, startedAt time.Time, retryStatusCodes string) (attemptResult, bool, error) {
	advancedConfig, err := channel.ParseAdvancedConfig([]byte(candidate.AdvancedConfig))
	if err != nil {
		return attemptResult{}, false, err
	}
	body, err := prepareRequestBody(endpoint, originalBody, candidate.UpstreamName, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
	}
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return attemptResult{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, candidate.BaseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "create upstream request")
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if candidate.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", candidate.OrganizationID)
	}
	if candidate.ProjectID != "" {
		req.Header.Set("OpenAI-Project", candidate.ProjectID)
	}
	client, err := s.channels.HTTPClientForProxy(candidate.ProxyURLCipher)
	if err != nil {
		return attemptResult{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "call upstream")
	}
	if !stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		responseBody = normalizeResponseBody(endpoint, responseBody, candidate.UpstreamName, advancedConfig)
		result := attemptResult{status: resp.StatusCode, body: responseBody, tokens: parseJSONUsage(responseBody), headers: resp.Header.Clone()}
		if readErr != nil {
			return result, false, gerror.Wrap(readErr, "read upstream response")
		}
		if retryableStatusForRules(resp.StatusCode, retryStatusCodes) {
			result.errorMessage = upstreamError(responseBody, resp.Status)
			return result, false, nil
		}
		if writer != nil {
			s.writeBufferedResponse(writer, resp.StatusCode, responseBody, resp.Header)
		}
		return result, true, nil
	}
	defer resp.Body.Close()
	copyResponseHeaders(writer.Header(), resp.Header)
	writer.WriteHeader(resp.StatusCode)
	flusher, _ := writer.(http.Flusher)
	result := attemptResult{status: resp.StatusCode}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	firstChunk := true
	for scanner.Scan() {
		line := append(append([]byte(nil), scanner.Bytes()...), '\n')
		line = normalizeSSELine(endpoint, line, candidate.UpstreamName, advancedConfig)
		if firstChunk {
			first := time.Since(startedAt).Milliseconds()
			result.firstTokenMs = &first
			firstChunk = false
		}
		parseSSEUsage(line, &result.tokens)
		if _, err = writer.Write(line); err != nil {
			result.errorMessage = err.Error()
			return result, true, nil
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err = scanner.Err(); err != nil {
		result.errorMessage = err.Error()
	}
	return result, true, nil
}
