package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

const (
	// TTS 输出与 ASR 输入都按 24 MiB 封顶，覆盖长文本语音与常见音频上传。
	maxAudioRequestBody = 24 << 20
	audioUpstreamTTL    = 180 * time.Second
)

type audioUpstreamResult struct {
	status  int
	body    []byte
	headers http.Header
	err     error
}

// HandleAudioSpeech 代理 POST /v1/audio/speech：JSON 请求，上游返回二进制音频。
func (s *sRelay) HandleAudioSpeech(ctx context.Context, incomingHeaders http.Header, clientIP, endpoint string, body []byte, key apikey.AuthKey, writer http.ResponseWriter) error {
	return s.handleAudio(ctx, incomingHeaders, clientIP, endpoint, body, key, writer, audioSpeechHandler{})
}

// HandleAudioTranscriptions 代理 POST /v1/audio/transcriptions：multipart 上传，上游返回 JSON 转写。
func (s *sRelay) HandleAudioTranscriptions(ctx context.Context, incomingHeaders http.Header, clientIP, endpoint string, body []byte, contentType string, key apikey.AuthKey, writer http.ResponseWriter) error {
	return s.handleAudio(ctx, incomingHeaders, clientIP, endpoint, body, key, writer, audioTranscriptionHandler{contentType: contentType})
}

// audioRequest 抽象 TTS/ASR 两类请求的共性行为：模型提取与上游请求体重建。
type audioRequest interface {
	RequestedModel(body []byte) (string, error)
	UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header) ([]byte, string, error)
}

type audioSpeechHandler struct{}

func (audioSpeechHandler) RequestedModel(body []byte) (string, error) {
	if !gjson.ValidBytes(body) {
		return "", gerror.New("speech request body must be valid JSON")
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" {
		return "", gerror.New("model is required")
	}
	return model, nil
}

func (audioSpeechHandler) UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header) ([]byte, string, error) {
	// TTS 请求体是纯 JSON，仅需把公开模型名替换为上游名。
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", gerror.New("speech request body must be a JSON object")
	}
	payload["model"] = upstreamModel
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", gerror.Wrap(err, "encode speech request")
	}
	return encoded, "application/json", nil
}

type audioTranscriptionHandler struct {
	contentType string
}

func (h audioTranscriptionHandler) RequestedModel(body []byte) (string, error) {
	// 普通表单字段的值在 content 返回（fileName 为空），取内容作为模型名。
	_, content, err := audioMultipartPart(body, h.contentType, "model", false)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func (h audioTranscriptionHandler) UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header) ([]byte, string, error) {
	fileName, fileContent, err := h.audioFile(body)
	if err != nil {
		return nil, "", err
	}
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	if err = writer.WriteField("model", upstreamModel); err != nil {
		return nil, "", gerror.Wrap(err, "encode transcriptions request")
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err == nil {
		_, err = part.Write(fileContent)
	}
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return nil, "", gerror.Wrap(err, "encode transcriptions request")
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func (h audioTranscriptionHandler) audioFile(body []byte) (string, []byte, error) {
	// 文件字段的文件名在 fileName 返回，内容在 content。
	return audioMultipartPart(body, h.contentType, "file", true)
}

// audioMultipartPart 从 multipart 请求体中提取指定字段；asFile 为 true 时保留文件名并提高读取上限。
func audioMultipartPart(body []byte, contentType, field string, asFile bool) (string, []byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return "", nil, gerror.New("transcriptions request content type must be multipart/form-data")
	}
	return readMultipartField(body, params["boundary"], field, asFile)
}

// readMultipartField 读取 multipart 体中指定字段的值与文件名（非文件字段文件名为空）。
func readMultipartField(body []byte, boundary, field string, asFile bool) (string, []byte, error) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return "", nil, gerror.Wrap(partErr, "read audio multipart request")
		}
		if part.FormName() != field {
			part.Close()
			continue
		}
		limit := int64(4096)
		if asFile {
			limit = maxAudioRequestBody
		}
		content, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		fileName := part.FileName()
		part.Close()
		if readErr != nil {
			return "", nil, gerror.Wrap(readErr, "read audio multipart field")
		}
		if int64(len(content)) > limit {
			return "", nil, gerror.New("audio multipart field exceeds limit")
		}
		return fileName, content, nil
	}
	return "", nil, gerror.New("audio multipart field " + field + " is required")
}

func (s *sRelay) handleAudio(ctx context.Context, incomingHeaders http.Header, clientIP, endpoint string, body []byte, key apikey.AuthKey, writer http.ResponseWriter, handler audioRequest) error {
	if len(body) > maxAudioRequestBody {
		return gerror.New("audio request body exceeds 24 MiB")
	}
	requestedModel, err := handler.RequestedModel(body)
	if err != nil {
		return err
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
	settings, settingsErr := s.resilience.Get(ctx)
	if settingsErr != nil {
		settings = system.DefaultResilienceSettings()
	}
	requestID := newRequestID()
	startedAt := time.Now()

	var (
		last          attemptResult
		lastCandidate Candidate
		handled       bool
	)
	for index := range candidates {
		candidate := candidates[index]
		credential, credentialErr := s.channels.SelectCredential(ctx, key.Id, candidate.ChannelID, nil)
		if credentialErr != nil {
			last = attemptResult{status: 0, errorMessage: credentialErr.Error()}
			lastCandidate = candidate
			continue
		}
		candidate.ChannelCredentialID = credential.ID
		candidate.APIKeyCipher = credential.APIKeyCipher
		result, attemptHandled := s.attemptAudioUpstream(ctx, writer, incomingHeaders, body, candidate, handler, startedAt, settings)
		if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices && result.errorMessage == "" {
			// 成功请求按上游响应速度加分，与 chat 链路保持一致。
			_, _ = s.resilience.ApplyModelHealthScore(ctx, settings, system.ModelDisableInput{
				ChannelID: candidate.ChannelID,
				ModelID:   candidate.ChannelModelID,
				Source:    system.AutoDisableSourceRelayRequest,
				Status:    result.status,
				Latency:   result.latency,
			})
		} else {
			s.maybeAutoDisable(ctx, settings, candidate, result)
		}
		last, lastCandidate, handled = result, candidate, attemptHandled
		if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices {
			break
		}
	}
	if last.status == 0 && last.errorMessage != "" {
		// 凭据选择等前置失败也记录用量日志，保持失败可追溯。
		s.recordAudioUsage(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, startedAt, last)
		return gerror.Wrap(gerror.New(last.errorMessage), "audio upstream failed")
	}

	if recordErr := s.recordAudioUsage(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, startedAt, last); recordErr != nil {
		if !handled {
			return recordErr
		}
		g.Log().Warningf(ctx, "record audio usage %s: %v", requestID, recordErr)
	}
	if !handled {
		if last.status >= http.StatusMultipleChoices || last.status == 0 {
			return gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible audio channels failed")
		}
		return gerror.New(last.errorMessage)
	}
	// attemptAudioUpstream 成功时已直接写出响应。
	return nil
}

func (s *sRelay) attemptAudioUpstream(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, body []byte, candidate Candidate, handler audioRequest, startedAt time.Time, settings adminapi.SystemResilienceSettingsInput) (attemptResult, bool) {
	upstreamModel := candidate.UpstreamName
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = candidate.PublicName
	}
	upstreamBody, contentType, err := handler.UpstreamBody(body, upstreamModel, incomingHeaders)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	requestCtx, cancel := context.WithTimeout(ctx, audioUpstreamTTL)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, candidate.BaseURL+audioEndpointPath(handler), bytes.NewReader(upstreamBody))
	if err != nil {
		return attemptResult{errorMessage: gerror.Wrap(err, "create audio upstream request").Error()}, false
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", contentType)
	if candidate.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", candidate.OrganizationID)
	}
	if candidate.ProjectID != "" {
		req.Header.Set("OpenAI-Project", candidate.ProjectID)
	}
	client, err := s.channels.HTTPClientForProxy(candidate.ProxyURLCipher)
	if candidate.DirectHTTP {
		client = s.app.HTTPDirect
	}
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	resp, err := client.Do(req)
	result := attemptResult{upstreamEndpoint: audioEndpointPath(handler)}
	result.latency = time.Since(startedAt)
	if err != nil {
		result.errorMessage = gerror.Wrap(err, "call audio upstream").Error()
		return result, false
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAudioRequestBody+1))
	result.status = resp.StatusCode
	result.headers = resp.Header.Clone()
	if readErr != nil {
		result.errorMessage = gerror.Wrap(readErr, "read audio upstream response").Error()
		return result, false
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.body = responseBody
		result.errorMessage = upstreamError(responseBody, resp.Status)
		return result, false
	}
	// 音频响应直接回写客户端：TTS 是二进制流，ASR 是 JSON 转写结果。
	copyResponseHeaders(writer.Header(), result.headers)
	writer.WriteHeader(resp.StatusCode)
	if _, err = writer.Write(responseBody); err != nil {
		result.errorMessage = err.Error()
		return result, true
	}
	result.body = responseBody
	result.wroteBytes = true
	return result, true
}

func audioEndpointPath(handler audioRequest) string {
	if _, ok := handler.(audioTranscriptionHandler); ok {
		return "/audio/transcriptions"
	}
	return "/audio/speech"
}

func (s *sRelay) recordAudioUsage(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, clientIP, endpoint, requestedModel string, startedAt time.Time, result attemptResult) error {
	upstreamEndpoint := result.upstreamEndpoint
	if upstreamEndpoint == "" {
		upstreamEndpoint = endpoint
	}
	// 音频接口上游不返回 token usage：TTS 按字符计价的规则在定价层处理，
	// 这里把 TTS 二进制响应的 tokens 留空；若模型已定价但估算不出成本，则按免费记录，不阻塞响应。
	billingDetails := s.prices.EstimateBreakdown(candidate.PublicName, upstreamEndpoint, result.tokens)
	recordStatus := result.status
	recordError := result.errorMessage
	if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices && billingDetails == nil && s.requiresBalanceCheck(candidate.PublicName) {
		// 已定价模型无法估算音频成本时记录 0 成本，避免误判为不可计费而中断已成功的请求。
		g.Log().Debugf(ctx, "audio usage %s: no billing breakdown for priced model %s", requestID, candidate.PublicName)
	}
	if err := s.usage.Record(ctx, usage.RecordInput{
		RequestID:           requestID,
		UserID:              key.UserId,
		APIKeyID:            key.Id,
		ChannelID:           candidate.ChannelID,
		ChannelCredentialID: candidate.ChannelCredentialID,
		Endpoint:            endpoint,
		UpstreamEndpoint:    upstreamEndpoint,
		ClientIP:            clientIP,
		IPLocation:          s.location(clientIP),
		RequestedModel:      requestedModel,
		UpstreamModel:       candidate.UpstreamName,
		HTTPStatus:          recordStatus,
		Stream:              false,
		Tokens:              result.tokens,
		EstimatedCost:       audioCost(billingDetails),
		BillingDetails:      billingDetails,
		DurationMs:          time.Since(startedAt).Milliseconds(),
		Attempts:            1,
		ErrorMessage:        recordError,
	}); err != nil {
		g.Log().Errorf(ctx, "record audio usage %s: %v", requestID, err)
	}
	return nil
}

func audioCost(billingDetails *usage.BillingBreakdown) *decimal.Decimal {
	if billingDetails == nil {
		return nil
	}
	cost := billingDetails.Cost()
	return &cost
}
