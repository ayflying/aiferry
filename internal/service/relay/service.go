package relay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
)

const maxRequestBody = 16 << 20

type Service struct {
	app        *app.Service
	usage      *usage.Service
	resilience *system.Service
}

type Candidate struct {
	ChannelModelID   uint64   `orm:"channel_model_id"`
	ChannelID        uint64   `orm:"channel_id"`
	ChannelName      string   `orm:"channel_name"`
	BaseURL          string   `orm:"base_url"`
	APIKeyCipher     string   `orm:"api_key_cipher"`
	OrganizationID   string   `orm:"organization_id"`
	ProjectID        string   `orm:"project_id"`
	Priority         int      `orm:"priority"`
	Weight           uint     `orm:"weight"`
	PublicName       string   `orm:"public_name"`
	UpstreamName     string   `orm:"upstream_name"`
	InputPrice       *float64 `orm:"input_price"`
	CachedInputPrice *float64 `orm:"cached_input_price"`
	CacheWritePrice  *float64 `orm:"cache_write_price"`
	OutputPrice      *float64 `orm:"output_price"`
	ImageInputPrice  *float64 `orm:"image_input_price"`
	AudioInputPrice  *float64 `orm:"audio_input_price"`
	AudioOutputPrice *float64 `orm:"audio_output_price"`
	RequestPrice     *float64 `orm:"request_price"`
	BillingMode      string   `orm:"billing_mode"`
	GroupIDs         []uint64
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
}

func New(appSvc *app.Service, usageSvc *usage.Service, resilienceSvc *system.Service) *Service {
	return &Service{app: appSvc, usage: usageSvc, resilience: resilienceSvc}
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
		result, handled, attemptErr := s.attempt(ctx, writer, incomingHeaders, endpoint, body, candidate, isStream, startedAt, settings.RetryStatusCodes)
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
			s.record(ctx, requestID, key, candidate, endpoint, requestedModel, isStream, index+1, startedAt, result)
			return nil
		}
		if retryableStatusForRules(result.status, settings.RetryStatusCodes) && index+1 < maxAttempts {
			s.markFailure(ctx, candidate.ChannelID)
			continue
		}
		s.writeBufferedResponse(writer, result.status, result.body, http.Header{"Content-Type": []string{"application/json"}})
		s.record(ctx, requestID, key, candidate, endpoint, requestedModel, isStream, index+1, startedAt, result)
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
	body, err := sjson.SetBytes(originalBody, "model", candidate.UpstreamName)
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "map upstream model")
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
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "call upstream")
	}
	if !stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		result := attemptResult{status: resp.StatusCode, body: responseBody, tokens: parseJSONUsage(responseBody)}
		if readErr != nil {
			return result, false, gerror.Wrap(readErr, "read upstream response")
		}
		if retryableStatusForRules(resp.StatusCode, retryStatusCodes) {
			result.errorMessage = upstreamError(responseBody, resp.Status)
			return result, false, nil
		}
		s.writeBufferedResponse(writer, resp.StatusCode, responseBody, resp.Header)
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

func (s *Service) route(ctx context.Context, model string, key apikey.AuthKey) ([]Candidate, error) {
	var candidates []Candidate
	err := dao.ChannelModels.Ctx(ctx).As("m").Fields(`
		m.id AS channel_model_id,m.public_name,m.upstream_name,p.input_price,p.cached_input_price,p.cache_write_price,p.output_price,p.image_input_price,p.audio_input_price,p.audio_output_price,p.request_price,COALESCE(p.billing_mode,'token') AS billing_mode,
		c.id AS channel_id,c.name AS channel_name,c.base_url,c.api_key_cipher,
		c.organization_id,c.project_id,c.priority,c.weight`).
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id AND c.status=1").
		LeftJoin(dao.ModelPrices.Table()+" p", "p.public_name=m.public_name AND p.deleted_at IS NULL").
		Where("m.enabled", 1).
		Where("m.public_name", model).
		Scan(&candidates)
	if err != nil {
		return nil, gerror.Wrap(err, "load model routes")
	}
	available := candidates[:0]
	for _, candidate := range candidates {
		groupIDs, groupErr := s.channelGroupIDs(ctx, candidate.ChannelID)
		if groupErr != nil {
			return nil, groupErr
		}
		if !keyAllowsGroups(key, groupIDs) {
			continue
		}
		if exists, _ := s.app.Redis.Exists(ctx, cooldownKey(candidate.ChannelID)).Result(); exists == 0 {
			candidate.GroupIDs = groupIDs
			available = append(available, candidate)
		}
	}
	return weightedOrder(available), nil
}

func weightedOrder(candidates []Candidate) []Candidate {
	groups := make(map[int][]Candidate)
	priorities := make([]int, 0)
	for _, candidate := range candidates {
		if _, ok := groups[candidate.Priority]; !ok {
			priorities = append(priorities, candidate.Priority)
		}
		groups[candidate.Priority] = append(groups[candidate.Priority], candidate)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	ordered := make([]Candidate, 0, len(candidates))
	for _, priority := range priorities {
		pool := append([]Candidate(nil), groups[priority]...)
		for len(pool) > 0 {
			total := uint64(0)
			for _, item := range pool {
				total += uint64(max(item.Weight, 1))
			}
			pick := mathrand.Uint64N(total)
			selected := 0
			for index, item := range pool {
				weight := uint64(max(item.Weight, 1))
				if pick < weight {
					selected = index
					break
				}
				pick -= weight
			}
			ordered = append(ordered, pool[selected])
			pool = append(pool[:selected], pool[selected+1:]...)
		}
	}
	return ordered
}

func (s *Service) record(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) {
	cost := s.estimateCost(ctx, candidate, endpoint, result.tokens)
	_ = s.usage.Record(ctx, usage.RecordInput{
		RequestID:      requestID,
		UserID:         key.UserId,
		APIKeyID:       key.Id,
		ChannelID:      candidate.ChannelID,
		Endpoint:       endpoint,
		RequestedModel: requestedModel,
		UpstreamModel:  candidate.UpstreamName,
		HTTPStatus:     result.status,
		Stream:         stream,
		Tokens:         result.tokens,
		EstimatedCost:  cost,
		DurationMs:     time.Since(startedAt).Milliseconds(),
		FirstTokenMs:   result.firstTokenMs,
		Attempts:       attempts,
		ErrorMessage:   result.errorMessage,
	})
	if cost != nil && result.status >= 200 && result.status < 300 {
		_ = apikey.New(s.app).AddSpend(ctx, key, cost.InexactFloat64())
	}
}

func (s *Service) estimateCost(ctx context.Context, candidate Candidate, endpoint string, tokens usage.TokenUsage) *decimal.Decimal {
	switch candidate.BillingMode {
	case "rules":
		return s.estimateRuleCost(ctx, candidate.PublicName, endpoint, tokens)
	case "request":
		return usage.EstimateCost(tokens, usage.PriceRates{Request: candidate.RequestPrice})
	default:
		return usage.EstimateCost(tokens, usage.PriceRates{
			Input:       candidate.InputPrice,
			CachedInput: candidate.CachedInputPrice,
			CacheWrite:  candidate.CacheWritePrice,
			Output:      candidate.OutputPrice,
			ImageInput:  candidate.ImageInputPrice,
			AudioInput:  candidate.AudioInputPrice,
			AudioOutput: candidate.AudioOutputPrice,
		})
	}
}

func (s *Service) estimateRuleCost(ctx context.Context, modelName, endpoint string, tokens usage.TokenUsage) *decimal.Decimal {
	var rules []struct {
		ConditionsJSON string `orm:"conditions_json"`
		RatesJSON      string `orm:"rates_json"`
	}
	err := dao.ModelPriceRules.Ctx(ctx).
		Fields("conditions_json,rates_json").
		Where("model_name", modelName).
		Where("status", 1).
		OrderDesc("priority").
		OrderDesc("source = 'manual'").
		OrderDesc("id").
		Scan(&rules)
	if err == nil {
		for _, rule := range rules {
			if cost, ok := ruleCost(rule.ConditionsJSON, rule.RatesJSON, endpoint, tokens); ok {
				return cost
			}
		}
	}
	return nil
}

func ruleCost(conditionsJSON, ratesJSON, endpoint string, tokens usage.TokenUsage) (*decimal.Decimal, bool) {
	conditions := gjson.Parse(conditionsJSON)
	if configured := strings.TrimSpace(conditions.Get("endpoint").String()); configured != "" && configured != endpoint {
		return nil, false
	}
	input := uint64(0)
	if tokens.Input != nil {
		input = *tokens.Input
	}
	if min := conditions.Get("inputTokensAtLeast"); min.Exists() && input < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("inputTokensAtMost"); max.Exists() && input > max.Uint() {
		return nil, false
	}
	output := uint64(0)
	if tokens.Output != nil {
		output = *tokens.Output
	}
	if min := conditions.Get("outputTokensAtLeast"); min.Exists() && output < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("outputTokensAtMost"); max.Exists() && output > max.Uint() {
		return nil, false
	}
	total := input + output
	if min := conditions.Get("totalTokensAtLeast"); min.Exists() && total < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("totalTokensAtMost"); max.Exists() && total > max.Uint() {
		return nil, false
	}
	rates := gjson.Parse(ratesJSON)
	cost := usage.EstimateCost(tokens, usage.PriceRates{
		Input:       rateValue(rates.Get("inputPerMillion")),
		CachedInput: rateValue(rates.Get("cachedInputPerMillion")),
		CacheWrite:  rateValue(rates.Get("cacheWritePerMillion")),
		Output:      rateValue(rates.Get("outputPerMillion")),
		ImageInput:  rateValue(rates.Get("imageInputPerMillion")),
		AudioInput:  rateValue(rates.Get("audioInputPerMillion")),
		AudioOutput: rateValue(rates.Get("audioOutputPerMillion")),
		Request:     rateValue(rates.Get("request")),
	})
	return cost, cost != nil
}

func rateValue(value gjson.Result) *float64 {
	if !value.Exists() || value.Type != gjson.Number {
		return nil
	}
	result := value.Float()
	return &result
}

func (s *Service) channelGroupIDs(ctx context.Context, channelID uint64) ([]uint64, error) {
	var ids []uint64
	err := dao.ChannelGroupMembers.Ctx(ctx).As("m").
		Fields("m.channel_group_id").
		InnerJoin(dao.ChannelGroups.Table()+" g", "g.id=m.channel_group_id AND g.status=1").
		Where("m.channel_id", channelID).
		OrderAsc("m.channel_group_id").
		Scan(&ids)
	return ids, gerror.Wrap(err, "load channel groups")
}

func keyAllowsModel(key apikey.AuthKey, model string) bool {
	return len(key.AllowedModels) == 0 || containsString(key.AllowedModels, model)
}
func keyAllowsGroups(key apikey.AuthKey, groupIDs []uint64) bool {
	if len(key.ChannelGroupIDs) == 0 {
		return true
	}
	for _, groupID := range groupIDs {
		for _, allowed := range key.ChannelGroupIDs {
			if allowed == groupID {
				return true
			}
		}
	}
	return false
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) markFailure(ctx context.Context, channelID uint64) {
	key := failureKey(channelID)
	count, err := s.app.Redis.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	_ = s.app.Redis.Expire(ctx, key, 10*time.Minute).Err()
	if count >= s.app.Config.FailureThreshold {
		_ = s.app.Redis.Set(ctx, cooldownKey(channelID), "1", time.Duration(s.app.Config.ChannelCooldownSeconds)*time.Second).Err()
	}
}

func (s *Service) markSuccess(ctx context.Context, channelID uint64) {
	_ = s.app.Redis.Del(ctx, failureKey(channelID), cooldownKey(channelID)).Err()
}

func (s *Service) maybeAutoDisable(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, candidate Candidate, result attemptResult) {
	_, _ = s.resilience.DisableIfNeededWithSettings(ctx, settings, system.AutoDisableInput{
		ChannelID: candidate.ChannelID,
		Status:    result.status,
		Latency:   result.latency,
		Message:   result.errorMessage,
	})
}

func (s *Service) writeBufferedResponse(writer http.ResponseWriter, status int, body []byte, headers http.Header) {
	copyResponseHeaders(writer.Header(), headers)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func parseJSONUsage(body []byte) usage.TokenUsage {
	input := optionalUint(body, "usage.input_tokens", "usage.prompt_tokens", "response.usage.input_tokens")
	cached := optionalUint(body, "usage.input_tokens_details.cached_tokens", "usage.prompt_tokens_details.cached_tokens", "response.usage.input_tokens_details.cached_tokens")
	cacheWrite := optionalUint(body, "usage.cache_creation_input_tokens", "usage.cache_creation_tokens", "usage.input_tokens_details.cache_creation_tokens", "usage.prompt_tokens_details.cache_creation_tokens")
	imageInput := optionalUint(body, "usage.image_tokens", "usage.input_tokens_details.image_tokens", "usage.prompt_tokens_details.image_tokens")
	audioInput := optionalUint(body, "usage.audio_tokens", "usage.input_tokens_details.audio_tokens", "usage.prompt_tokens_details.audio_tokens")
	output := optionalUint(body, "usage.output_tokens", "usage.completion_tokens", "response.usage.output_tokens")
	audioOutput := optionalUint(body, "usage.output_audio_tokens", "usage.output_tokens_details.audio_tokens", "usage.completion_tokens_details.audio_tokens")
	total := optionalUint(body, "usage.total_tokens", "response.usage.total_tokens")
	if total == nil && input != nil && output != nil {
		value := *input + *output
		total = &value
	}
	return usage.TokenUsage{Input: input, CachedInput: cached, CacheWrite: cacheWrite, ImageInput: imageInput, AudioInput: audioInput, Output: output, AudioOutput: audioOutput, Total: total}
}

func parseSSEUsage(line []byte, target *usage.TokenUsage) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if payload == "" || payload == "[DONE]" || !gjson.Valid(payload) {
		return
	}
	parsed := parseJSONUsage([]byte(payload))
	if parsed.Input != nil || parsed.CachedInput != nil || parsed.CacheWrite != nil || parsed.ImageInput != nil || parsed.AudioInput != nil || parsed.Output != nil || parsed.AudioOutput != nil {
		*target = parsed
	}
}

func optionalUint(body []byte, paths ...string) *uint64 {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Type == gjson.Number {
			number := value.Uint()
			return &number
		}
	}
	return nil
}

func retryableStatus(status int) bool {
	return retryableStatusForRules(status, system.DefaultResilienceSettings().RetryStatusCodes)
}

func retryableStatusForRules(status int, rules string) bool {
	return system.MatchesStatusCodeRules(rules, status)
}

func copyRequestHeaders(target, source http.Header) {
	for _, name := range []string{"Accept", "User-Agent", "OpenAI-Beta", "Idempotency-Key"} {
		for _, value := range source.Values(name) {
			target.Add(name, value)
		}
	}
}

func copyResponseHeaders(target, source http.Header) {
	for name, values := range source {
		if hopByHopHeader(name) || strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			target.Add(name, value)
		}
	}
}

func hopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func openAIError(kind, message string) []byte {
	payload, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "message": message}})
	return payload
}

func upstreamError(body []byte, fallback string) string {
	if message := strings.TrimSpace(gjson.GetBytes(body, "error.message").String()); message != "" {
		return message
	}
	return fallback
}

func newRequestID() string {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	return "afreq_" + hex.EncodeToString(random)
}

func failureKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:failures", channelID)
}

func cooldownKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:cooldown", channelID)
}
