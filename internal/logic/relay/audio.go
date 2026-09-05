package relay

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

const (
	// TTS 输出与 ASR 输入都按 24 MiB 封顶，覆盖长文本语音与常见音频上传。
	maxAudioRequestBody = 24 << 20
	audioUpstreamTTL    = 180 * time.Second
	// chat 型适配下 base64 后的音频输入上限（MiMo 官方限制 10 MB）。
	maxChatAudioBase64Input = 10 << 20
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
	// UpstreamBody 按 upstreamModel 与适配器重建上游请求体，返回 (body, contentType, upstreamPath)。
	UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header, adapter string) ([]byte, string, string, error)
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

func (audioSpeechHandler) UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header, adapter string) ([]byte, string, string, error) {
	if adapter == channeltype.AudioAdapterChat {
		// chat 型适配：目标文本放 assistant 消息，风格指令放 user 消息，音频经 choices[0].message.audio.data 返回。
		var payload struct {
			Model string `json:"model"`
			Input string `json:"input"`
			Voice string `json:"voice"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", "", gerror.New("speech request body must be a JSON object")
		}
		text := payload.Input
		if strings.TrimSpace(text) == "" {
			text = "模型测试"
		}
		voice := payload.Voice
		if strings.TrimSpace(voice) == "" {
			voice = "mimo_default"
		}
		chatPayload := map[string]any{
			"model": upstreamModel,
			"messages": []map[string]any{
				{"role": "user", "content": "请以自然的语气朗读以下内容。"},
				{"role": "assistant", "content": text},
			},
			"audio": map[string]any{"format": "wav", "voice": voice},
		}
		encoded, err := json.Marshal(chatPayload)
		if err != nil {
			return nil, "", "", gerror.Wrap(err, "encode chat-adapted speech request")
		}
		return encoded, "application/json", "/chat/completions", nil
	}
	// 标准适配：TTS 请求体是纯 JSON，仅需把公开模型名替换为上游名。
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", "", gerror.New("speech request body must be a JSON object")
	}
	payload["model"] = upstreamModel
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", gerror.Wrap(err, "encode speech request")
	}
	return encoded, "application/json", "/audio/speech", nil
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

func (h audioTranscriptionHandler) UpstreamBody(body []byte, upstreamModel string, incomingHeaders http.Header, adapter string) ([]byte, string, string, error) {
	if adapter == channeltype.AudioAdapterChat {
		return h.chatAdaptedBody(body, upstreamModel)
	}
	fileName, fileContent, err := h.audioFile(body)
	if err != nil {
		return nil, "", "", err
	}
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	if err = writer.WriteField("model", upstreamModel); err != nil {
		return nil, "", "", gerror.Wrap(err, "encode transcriptions request")
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err == nil {
		_, err = part.Write(fileContent)
	}
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return nil, "", "", gerror.Wrap(err, "encode transcriptions request")
	}
	return buffer.Bytes(), writer.FormDataContentType(), "/audio/transcriptions", nil
}

// chatAdaptedBody 把 multipart 转写请求转为 chat completions 承载的 JSON：音频 base64 后经 input_audio 传入。
func (h audioTranscriptionHandler) chatAdaptedBody(body []byte, upstreamModel string) ([]byte, string, string, error) {
	fileName, fileContent, err := h.audioFile(body)
	if err != nil {
		return nil, "", "", err
	}
	if len(fileContent) > maxChatAudioBase64Input {
		return nil, "", "", gerror.New("audio file exceeds the chat-adapted transcription limit")
	}
	mimeType := "audio/wav"
	lowerName := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lowerName, ".mp3"):
		mimeType = "audio/mpeg"
	case strings.HasSuffix(lowerName, ".wav"):
		mimeType = "audio/wav"
	default:
		if len(fileContent) >= 4 && string(fileContent[:4]) == "RIFF" {
			mimeType = "audio/wav"
		} else {
			mimeType = "audio/mpeg"
		}
	}
	chatPayload := map[string]any{
		"model": upstreamModel,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "input_audio", "input_audio": map[string]string{
					"data": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(fileContent),
				}},
			}},
		},
		"asr_options": map[string]any{"language": "auto"},
	}
	encoded, err := json.Marshal(chatPayload)
	if err != nil {
		return nil, "", "", gerror.Wrap(err, "encode chat-adapted transcription request")
	}
	return encoded, "application/json", "/chat/completions", nil
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
	candidates, err := s.routeCached(ctx, requestedModel, key)
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
		attemptFlow   []usage.AttemptFlowStep
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
		attemptStartedAt := time.Now()
		result, attemptHandled := s.attemptAudioUpstream(ctx, writer, incomingHeaders, body, candidate, handler, startedAt, settings)
		result.latency = time.Since(attemptStartedAt)
		attemptFlow = append(attemptFlow, usage.AttemptFlowStep{ChannelName: candidate.ChannelName, DurationMs: result.latency.Milliseconds()})
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
	last.attemptFlow = attemptFlow
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
	adapter, adapterErr := s.channels.AudioAdapterFor(ctx, candidate.ChannelType)
	if adapterErr != nil {
		g.Log().Warningf(ctx, "resolve audio adapter for channel %s: %v", candidate.ChannelType, adapterErr)
		adapter = channeltype.AudioAdapterOpenAI
	}
	upstreamBody, contentType, upstreamPath, err := handler.UpstreamBody(body, upstreamModel, incomingHeaders, adapter)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	requestCtx, cancel := context.WithTimeout(ctx, audioUpstreamTTL)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, candidate.BaseURL+upstreamPath, bytes.NewReader(upstreamBody))
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
	result := attemptResult{upstreamEndpoint: upstreamPath}
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
	// chat 型适配下 TTS 响应是 chat completion JSON，音频 base64 藏在 choices[0].message.audio.data，
	// 需还原为标准 /audio/speech 期望的二进制音频体。
	if _, isSpeech := handler.(audioSpeechHandler); isSpeech && adapter == channeltype.AudioAdapterChat {
		responseBody, err = chatAudioToBinary(responseBody)
		if err != nil {
			result.errorMessage = err.Error()
			return result, false
		}
		contentType = "audio/wav"
		result.headers = result.headers.Clone()
		result.headers.Set("Content-Type", contentType)
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
		Attempts:            len(result.attemptFlow),
		AttemptFlow:         result.attemptFlow,
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

// chatAudioToBinary 从 chat 型 TTS 响应中提取 base64 音频并还原为二进制。
func chatAudioToBinary(body []byte) ([]byte, error) {
	audioData := gjson.GetBytes(body, "choices.0.message.audio.data").String()
	if audioData == "" {
		return nil, gerror.New("chat-adapted speech response does not contain audio data")
	}
	if comma := strings.Index(audioData, ","); strings.HasPrefix(audioData, "data:") && comma >= 0 {
		audioData = audioData[comma+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(audioData)
	if err != nil {
		return nil, gerror.Wrap(err, "decode chat-adapted speech audio")
	}
	return decoded, nil
}
