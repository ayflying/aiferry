package channel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/upstreamerror"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type TestResult struct {
	Success      bool   `json:"success"`
	Endpoint     string `json:"endpoint"`
	Stream       bool   `json:"stream"`
	Model        string `json:"model"`
	LatencyMs    int64  `json:"latencyMs"`
	HTTPStatus   int    `json:"httpStatus"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Message      string `json:"message"`
}

func (s *sChannel) TestModel(ctx context.Context, input adminapi.ModelTestInput, userID uint64) (TestResult, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, input.ModelID).Scan(&model); err != nil {
		return TestResult{}, gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return TestResult{}, gerror.New("model not found")
	}
	if userID != usage.SystemUserID && s.prices.IsPriced(model.PublicName) {
		if err := s.users.CheckBalance(ctx, userID); err != nil {
			return TestResult{}, err
		}
	}
	channel, err := s.Get(ctx, model.ChannelId)
	if err != nil {
		return TestResult{}, err
	}
	credential, err := s.CredentialForTest(ctx, channel.Id, input.ChannelCredentialID)
	if err != nil {
		return TestResult{}, err
	}
	_, typeConfig, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return TestResult{}, err
	}
	// 全局协议设置参与测试端点判定：与转发链路同一语义（类型级名单优先、
	// 全局名单兜底），保证「测试与正式一致」。
	settings, err := s.resilience.Get(ctx)
	if err != nil {
		return TestResult{}, err
	}
	endpoints := testEndpoints(input.Endpoint, model.UpstreamName, typeConfig, settings)
	advancedConfig, err := ParseAdvancedConfig([]byte(channel.AdvancedConfig))
	if err != nil {
		return TestResult{}, err
	}
	baseURLs := advancedConfig.UpstreamBaseURLs(channel.BaseUrl)
	var (
		result      TestResult
		billingErr  error
		path        string
		tokens      usage.TokenUsage
		attemptFlow []usage.AttemptFlowStep
	)
	tested := false
	finished := false
	for index, endpoint := range endpoints {
		for _, baseURL := range baseURLs {
			current, currentPath, currentTokens, requestErr := s.testModelEndpoint(ctx, channel, credential, typeConfig, settings, model, advancedConfig, baseURL, endpoint, input.Stream)
			if requestErr != nil {
				return TestResult{}, requestErr
			}
			tested = true
			result, path, tokens = current, currentPath, currentTokens
			// 端点/地址的回退尝试逐条记入调用流程：只留最终结果会让「测试最终
			// 通过、但中间换过端点」这类信息不可见。
			attemptFlow = append(attemptFlow, testAttemptFlowStep(channel.Name, currentPath, current))
			if result.Success {
				finished = true
				break
			}
		}
		if finished || index == len(endpoints)-1 || !canTryAlternativeEndpoint(result) {
			break
		}
	}
	if tested {
		billingErr = s.recordTestUsage(ctx, userID, channel, credential.ID, model, path, &result, tokens, attemptFlow)
	}
	if result.Success {
		s.clearCredentialTransient(ctx, credential.ID)
		s.resilience.ClearAutoDisableFailures(ctx, credential.ID)
		_, _ = s.resilience.RecoverCredentialIfAllowed(ctx, credential.ID)
		_, _ = s.resilience.RecoverIfAllowed(ctx, channel.Id)
		// 测试成功同时恢复该模型本身并加分，让被禁用的模型有机会重新可用。
		_ = s.resilience.BumpComboHealthScore(ctx, model.Id, credential.ID, system.ModelHealthTestSuccess)
	} else {
		_, _ = s.resilience.DisableIfNeeded(ctx, system.AutoDisableInput{
			ChannelID:           channel.Id,
			ChannelCredentialID: credential.ID,
			ChannelModelID:      model.Id,
			Source:              system.AutoDisableSourceModelTest,
			Status:              result.HTTPStatus,
			Latency:             time.Duration(result.LatencyMs) * time.Millisecond,
			Message:             result.Message,
		})
	}
	s.saveTestResult(ctx, channel.Id, model.Id, result.Endpoint, result)
	if billingErr != nil {
		return result, billingErr
	}
	return result, nil
}

// buildTestRequest 按 payload 类型构造测试请求：asrMultipartRequest 走 multipart 表单，其余走 JSON。
// JSON 分支与转发链路共用 ApplyPromptCachePolicy，保证测试发出去的缓存字段与正式请求一致。
func buildTestRequest(ctx context.Context, url string, payload any, config AdvancedConfig, identity string) (*http.Request, error) {
	if asr, ok := payload.(asrMultipartRequest); ok {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		_ = writer.WriteField("model", asr.Model)
		part, err := writer.CreateFormFile("file", asr.Filename)
		if err == nil {
			_, err = part.Write(asr.Content)
		}
		if err == nil {
			err = writer.Close()
		}
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	body, err = ApplyPromptCachePolicy(body, config, identity)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (s *sChannel) testModelEndpoint(ctx context.Context, channel entity.Channels, credential RouteCredential, typeConfig channeltype.Config, settings adminapi.SystemResilienceSettingsInput, model entity.ChannelModels, config AdvancedConfig, baseURL, endpoint string, stream bool) (TestResult, string, usage.TokenUsage, error) {
	path, payload, streamed := testPayload(endpoint, model.UpstreamName, stream, typeConfig, settings)
	if typeConfig.Audio.Adapter == channeltype.AudioAdapterChat {
		path, payload = chatAdapterPayload(endpoint, model.UpstreamName, payload)
	}
	// 模型测试复用转发链路的缓存字段处置，避免「测试通过、正式被上游拒绝」：
	// 渠道声明 off 时测试同样不下发缓存字段，缺省时同样注入稳定键。
	identity := fmt.Sprintf("v1|test|m:%s|c:%d|k:%d", model.UpstreamName, channel.Id, credential.ID)
	req, err := buildTestRequest(ctx, resolveTestURL(baseURL, path), payload, config, identity)
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "create model test request")
	}
	if err = s.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{
		Spec: UpstreamAuthSpec{
			AuthType:     typeConfig.Models.AuthType,
			HeaderName:   typeConfig.Models.HeaderName,
			HeaderPrefix: typeConfig.Models.HeaderPrefix,
		},
		CredentialCipher:    credential.APIKeyCipher,
		ManagementKeyCipher: channel.ManagementKeyCipher,
		OrganizationID:      channel.OrganizationId,
		ProjectID:           channel.ProjectId,
	}); err != nil {
		return TestResult{}, path, usage.TokenUsage{}, err
	}
	// Anthropic Messages 端点与转发链路同样要求显式协议版本头。
	if endpoint == "messages" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	// OpenCode Go 等上游要求稳定的客户端标识头，缺失时会直接返回 400
	// MissingSessionID，模型测试与巡检链路同样必须补齐。
	ApplyUpstreamClientHeaders(req.Header, nil, UpstreamClientIdentity{
		ChannelType:  channel.Type,
		ChannelID:    channel.Id,
		CredentialID: credential.ID,
		ModelName:    model.UpstreamName,
	})
	startedAt := time.Now()
	client, clientErr := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if clientErr != nil {
		return TestResult{}, path, usage.TokenUsage{}, clientErr
	}
	resp, requestErr := client.Do(req)
	latency := time.Since(startedAt).Milliseconds()
	result := TestResult{Endpoint: endpoint, Stream: streamed, Model: model.PublicName, LatencyMs: latency}
	if requestErr != nil {
		result.Message = requestErr.Error()
		return result, path, usage.TokenUsage{}, nil
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	result.HTTPStatus = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	tokens := parseTestUsage(responseBody, streamed)
	result.InputTokens = int64(testTokenValue(tokens.Input))
	result.OutputTokens = int64(testTokenValue(tokens.Output))
	if result.Success {
		result.Message = "模型响应正常"
	} else {
		result.Message = upstreamerror.Message(responseBody, resp.Status)
	}
	return result, path, tokens, nil
}

func (s *sChannel) recordTestUsage(ctx context.Context, userID uint64, channel entity.Channels, credentialID uint64, model entity.ChannelModels, path string, result *TestResult, tokens usage.TokenUsage, attemptFlow []usage.AttemptFlowStep) error {
	if s.usage == nil {
		return nil
	}
	cost := s.prices.Estimate(model.PublicName, path, tokens, time.Now())
	recordStatus := result.HTTPStatus
	recordMessage := result.Message
	var chargeErr error
	if result.Success {
		if cost != nil {
			if applyErr := s.ApplyCredentialUsageCost(ctx, channel.Id, credentialID, *cost); applyErr != nil {
				g.Log().Warningf(ctx, "apply channel %d test usage cost: %v", channel.Id, applyErr)
			}
			if userID != usage.SystemUserID {
				if debitErr := s.users.Debit(ctx, userID, *cost); debitErr != nil {
					chargeErr = debitErr
				} else if s.mail != nil {
					s.mail.NotifyLowBalance(ctx, userID)
				}
			}
		}
		if chargeErr != nil {
			recordStatus = http.StatusPaymentRequired
			recordMessage = chargeErr.Error()
		}
	}
	recordErr := s.usage.Record(ctx, usage.RecordInput{
		RequestID:            usage.NewRequestID("aftest"),
		UserID:               userID,
		ChannelID:            channel.Id,
		ChannelCredentialID:  credentialID,
		Endpoint:             "test:" + path,
		RequestedModel:       model.PublicName,
		UpstreamModel:        model.UpstreamName,
		HealthScoreAtRequest: &model.HealthScore,
		HTTPStatus:           recordStatus,
		Stream:               result.Stream,
		Tokens:               tokens,
		EstimatedCost:        cost,
		DurationMs:           result.LatencyMs,
		Attempts:             len(attemptFlow),
		AttemptFlow:          attemptFlow,
		ErrorMessage:         recordMessage,
	})
	if recordErr != nil {
		result.Message = truncate(result.Message+"；用量记录失败："+recordErr.Error(), 1024)
	}
	return chargeErr
}

// testAttemptFlowStep 把一次渠道测试的端点尝试快照成调用流程步骤。测试链路会按
// 端点/地址逐个回退（如 /chat/completions 失败后改试 /responses），这些尝试必须
// 能在「调用流程」里看到，而不是只留一个最终结果。
func testAttemptFlowStep(channelName, endpoint string, result TestResult) usage.AttemptFlowStep {
	step := usage.AttemptFlowStep{ChannelName: channelName, Endpoint: endpoint, DurationMs: result.LatencyMs}
	if !result.Success {
		if result.HTTPStatus > 0 {
			status := uint(result.HTTPStatus)
			step.Status = &status
		}
		step.Error = truncate(result.Message, 240)
	}
	return step
}

func testEndpoints(endpoint, model string, typeConfig channeltype.Config, settings adminapi.SystemResilienceSettingsInput) []string {
	if endpoint != "auto" {
		return []string{endpoint}
	}
	modelName := strings.ToLower(strings.TrimSpace(model))
	// Messages 模型名单命中（渠道类型声明优先、全局名单兜底）：该模型只认
	// Anthropic Messages 端点，放在序列首位；后续端点仅在 404/405 等可回退
	// 失败时才会尝试。
	if channeltype.ResolveMessagesEndpoint(typeConfig, settings, model) != "" {
		return []string{"messages", "chat", "responses", "embeddings"}
	}
	switch {
	case containsAny(modelName, "tts", "speech"):
		return []string{"tts"}
	case containsAny(modelName, "asr", "whisper", "transcribe", "stt"):
		return []string{"asr"}
	case strings.Contains(modelName, "image"):
		return []string{"images"}
	case strings.Contains(modelName, "embedding"):
		return []string{"embeddings"}
	case strings.HasPrefix(modelName, "gpt-5"):
		return []string{"responses", "chat"}
	default:
		return []string{"chat", "responses", "embeddings"}
	}
}

// resolveTestURL 与转发链路的 resolveUpstreamURL 同一语义：端点是完整 URL
// （如渠道类型声明的 Messages 地址）时原样使用。
func resolveTestURL(baseURL, endpoint string) string {
	if channeltype.IsAbsoluteHTTPURL(endpoint) {
		return endpoint
	}
	return baseURL + endpoint
}

func containsAny(model string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(model, keyword) {
			return true
		}
	}
	return false
}

func canTryAlternativeEndpoint(result TestResult) bool {
	switch result.HTTPStatus {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		message := strings.ToLower(result.Message)
		for _, marker := range []string{"endpoint", "not support", "unsupported", "not compatible", "only supports", "chat completion", "responses api", "embedding model"} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

func testPayload(endpoint, model string, stream bool, typeConfig channeltype.Config, settings adminapi.SystemResilienceSettingsInput) (string, any, bool) {
	switch endpoint {
	case "messages":
		payload := map[string]any{
			"model":      model,
			"max_tokens": 16,
			"messages":   []map[string]string{{"role": "user", "content": "Reply with exactly OK."}},
		}
		if stream {
			payload["stream"] = true
		}
		// 名单命中时用解析出的端点（类型级/全局）；显式指定 messages 但名单
		// 未配置时退回类型级或协议缺省值，保持手动测试可用。
		url := channeltype.ResolveMessagesEndpoint(typeConfig, settings, model)
		if url == "" {
			url = channeltype.MessagesEndpointURL(typeConfig)
		}
		return url, payload, stream
	case "tts":
		return "/audio/speech", map[string]any{
			"model":           model,
			"input":           "模型测试",
			"voice":           "alloy",
			"response_format": "mp3",
		}, false
	case "asr":
		return "/audio/transcriptions", asrTestPayload(model), false
	case "responses":
		payload := map[string]any{
			"model":             model,
			"input":             "Reply with exactly OK.",
			"max_output_tokens": 16,
		}
		if stream {
			payload["stream"] = true
		}
		return "/responses", payload, stream
	case "embeddings":
		return "/embeddings", map[string]any{"model": model, "input": "AiFerry model check"}, false
	case "images":
		// size 不传：各上游默认值不同（火山 seedream 要求总像素
		// >= 3686400，OpenAI 默认 1024x1024），写死任何一个都会被
		// 其他上游拒绝，缺省让上游走自己的默认分辨率。
		return "/images/generations", map[string]any{
			"model":  model,
			"prompt": "A small white ferry sailing on calm blue water.",
			"n":      1,
		}, false
	default:
		payload := map[string]any{
			"model":                 model,
			"messages":              []map[string]string{{"role": "user", "content": "Reply with exactly OK."}},
			"max_completion_tokens": 16,
			"stream":                stream,
		}
		if stream {
			payload["stream_options"] = map[string]bool{"include_usage": true}
		}
		return "/chat/completions", payload, stream
	}
}

// asrMultipartRequest 描述 ASR 测试的 multipart 表单：最小合法 WAV + 模型名。
type asrMultipartRequest struct {
	Model    string
	Filename string
	Content  []byte
}

// chatAdapterPayload 把标准 TTS/ASR 测试请求转换为 chat completions 承载的等价请求，
// 供 audio.adapter = "chat" 的渠道类型（如小米 MiMo）使用。返回的 payload 一律为 JSON。
func chatAdapterPayload(endpoint, model string, payload any) (string, any) {
	switch endpoint {
	case "tts":
		text, voice := "模型测试", "mimo_default"
		if values, ok := payload.(map[string]any); ok {
			if value, exists := values["input"].(string); exists && value != "" {
				text = value
			}
			if value, exists := values["voice"].(string); exists && value != "" {
				voice = value
			}
		}
		return "/chat/completions", map[string]any{
			"model": model,
			"messages": []map[string]any{
				{"role": "user", "content": "请以自然的语气朗读以下内容。"},
				{"role": "assistant", "content": text},
			},
			"audio": map[string]any{"format": "wav", "voice": voice},
		}
	case "asr":
		audioBase64 := ""
		filename := "aiferry-test.wav"
		if request, ok := payload.(asrMultipartRequest); ok {
			audioBase64 = base64.StdEncoding.EncodeToString(request.Content)
			if request.Filename != "" {
				filename = request.Filename
			}
		}
		mimeType := "audio/wav"
		if strings.HasSuffix(strings.ToLower(filename), ".mp3") {
			mimeType = "audio/mpeg"
		}
		return "/chat/completions", map[string]any{
			"model": model,
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]any{
					{"type": "input_audio", "input_audio": map[string]string{"data": "data:" + mimeType + ";base64," + audioBase64}},
				}},
			},
			"asr_options": map[string]any{"language": "auto"},
		}
	default:
		return "/chat/completions", payload
	}
}

// asrTestPayload 构造最小 WAV（44 字节头 + 1 个静音采样），足以让上游校验通过并返回转写结果。
func asrTestPayload(model string) asrMultipartRequest {
	return asrMultipartRequest{
		Model:    model,
		Filename: "aiferry-test.wav",
		Content:  minimalWAV(),
	}
}

// minimalWAV 返回单声道 8kHz 16bit 的 1 采样静音 WAV。
func minimalWAV() []byte {
	const (
		channels   = 1
		sampleRate = 8000
		bitsPerSam = 16
	)
	data := []byte{0x00, 0x00}
	byteRate := sampleRate * channels * bitsPerSam / 8
	blockAlign := channels * bitsPerSam / 8
	buffer := bytes.NewBuffer(nil)
	buffer.WriteString("RIFF")
	binary.Write(buffer, binary.LittleEndian, uint32(36+len(data)))
	buffer.WriteString("WAVE")
	buffer.WriteString("fmt ")
	binary.Write(buffer, binary.LittleEndian, uint32(16))
	binary.Write(buffer, binary.LittleEndian, uint16(1))
	binary.Write(buffer, binary.LittleEndian, uint16(channels))
	binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buffer, binary.LittleEndian, uint32(byteRate))
	binary.Write(buffer, binary.LittleEndian, uint16(blockAlign))
	binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSam))
	buffer.WriteString("data")
	binary.Write(buffer, binary.LittleEndian, uint32(len(data)))
	buffer.Write(data)
	return buffer.Bytes()
}

func parseTestUsage(body []byte, stream bool) usage.TokenUsage {
	tokens := usage.ParseJSONUsage(body)
	if !stream {
		return tokens
	}
	for _, line := range strings.Split(string(body), "\n") {
		usage.ParseSSEUsage([]byte(line), &tokens)
	}
	return tokens
}

func (s *sChannel) saveTestResult(ctx context.Context, channelID, modelID uint64, endpoint string, result TestResult) {
	status := "failed"
	if result.Success {
		status = "success"
	}
	message := truncate(result.Message, 1024)
	_, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Data(do.ChannelModels{
		LastTestEndpoint:  endpoint,
		LastTestStatus:    status,
		LastTestLatencyMs: result.LatencyMs,
		LastTestError:     message,
		LastTestAt:        time.Now(),
	}).Update()
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(do.Channels{
		LastTestStatus:    status,
		LastTestLatencyMs: result.LatencyMs,
		LastTestError:     message,
		LastTestAt:        time.Now(),
	}).Update(); err == nil {
		s.InvalidateListCache(ctx)
	}
}

func testTokenValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func failureKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:failures", channelID)
}

func cooldownKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:cooldown", channelID)
}
