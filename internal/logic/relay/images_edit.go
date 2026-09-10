package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

const (
	// 图片编辑请求带参考图上传，与图片生成链路一致按 32 MiB 封顶。
	maxImagesEditRequestBody = 32 << 20
	imagesEditEndpoint       = "/images/edits"
)

// imagesEditPart 是 /images/edits multipart 请求里的一个字段。
// fileName 非空表示文件字段（参考图），否则是普通文本字段。
type imagesEditPart struct {
	name        string
	value       []byte
	fileName    string
	contentType string
}

// HandleImagesEdit 代理 POST /v1/images/edits：multipart 上传参考图与文本字段，
// 上游返回与 /v1/images/generations 同构的 JSON。
//
// 请求体不是 JSON，无法复用通用 Handle（它要求 body 为合法 JSON），
// 因此参照音频转写链路（relay/audio.go）单独解析 multipart 后按渠道重建请求，
// 路由、凭据轮换、计费与用量记录仍复用同一套组件。
func (s *sRelay) HandleImagesEdit(ctx context.Context, incomingHeaders http.Header, clientIP, endpoint string, body []byte, contentType string, key apikey.AuthKey, writer http.ResponseWriter) error {
	if len(body) > maxImagesEditRequestBody {
		return gerror.New("images/edits request body exceeds 32 MiB")
	}
	parts, err := parseImagesEditMultipart(body, contentType)
	if err != nil {
		return err
	}
	requestedModel := imagesEditTextPart(parts, "model")
	if requestedModel == "" {
		return gerror.New("model is required")
	}
	if !imagesEditHasImage(parts) {
		return gerror.New("images/edits request requires an image file")
	}
	if !keyAllowsModel(key, requestedModel) {
		return gerror.New("API key is not allowed to use model " + requestedModel)
	}
	// 与图片生成同一口径：把 multipart 中的 prompt 还原成 JSON 交给统一敏感词检查。
	if err = s.resilience.CheckSensitivePrompt(ctx, endpoint, imagesEditPromptProbe(requestedModel, parts)); err != nil {
		return err
	}
	candidates, err := s.routeCached(ctx, requestedModel, key)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		// 无可用渠道属无痕失败（未进入转发循环，不写用量），必须留日志便于排障。
		g.Log().Warningf(ctx, "relay %s: no available channel for model %s (images/edits; model auto-disabled, channel inactive, or group policy filtered)", clientIP, requestedModel)
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
		attempts      int
		attemptFlow   []usage.AttemptFlowStep
	)
	for index := range candidates {
		candidate := candidates[index]
		credential, credentialErr := s.channels.SelectCredential(ctx, key.Id, candidate.ChannelID, nil)
		if credentialErr != nil {
			last = attemptResult{errorMessage: credentialErr.Error()}
			lastCandidate = candidate
			attempts++
			continue
		}
		candidate.ChannelCredentialID = credential.ID
		candidate.APIKeyCipher = credential.APIKeyCipher
		attemptStartedAt := time.Now()
		result, handled := s.attemptImagesEditUpstream(ctx, writer, incomingHeaders, parts, candidate, settings, attemptStartedAt)
		result.latency = time.Since(attemptStartedAt)
		attempts++
		attemptFlow = append(attemptFlow, newAttemptFlowStep(candidate.ChannelName, result))
		if result.status >= http.StatusOK && result.status < http.StatusMultipleChoices && result.errorMessage == "" {
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
		last, lastCandidate = result, candidate
		if handled {
			break
		}
		// 上游明确失败但尚未回写响应时继续尝试下一个渠道（与通用转发链路的失败转移一致）。
	}
	last.attemptFlow = attemptFlow
	if recordErr := s.record(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, false, attempts, startedAt, last); recordErr != nil {
		if !last.wroteBytes {
			return recordErr
		}
		g.Log().Warningf(ctx, "record images/edits usage %s: %v", requestID, recordErr)
	}
	if last.wroteBytes {
		return nil
	}
	if last.status >= http.StatusMultipleChoices || last.status == 0 {
		return gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible images/edits channels failed")
	}
	return gerror.New(last.errorMessage)
}

// attemptImagesEditUpstream 把 multipart 请求按渠道重建并发往上游 /images/edits。
// 2xx 响应直接回写客户端并标记 handled，其余情况返回结果供上层决定是否换渠道重试。
func (s *sRelay) attemptImagesEditUpstream(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, parts []imagesEditPart, candidate Candidate, settings adminapi.SystemResilienceSettingsInput, startedAt time.Time) (attemptResult, bool) {
	upstreamModel := candidate.UpstreamName
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = candidate.PublicName
	}
	upstreamBody, contentType, err := encodeImagesEditParts(parts, upstreamModel)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return attemptResult{errorMessage: err.Error()}, false
	}
	timeout := time.Duration(settings.NonStreamTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	upstreamPath := imagesEditEndpoint
	upstreamURL := strings.TrimRight(strings.TrimSpace(candidate.BaseURL), "/") + upstreamPath
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return attemptResult{errorMessage: gerror.Wrap(err, "create images/edits upstream request").Error()}, false
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
	result := attemptResult{upstreamEndpoint: upstreamPath}
	resp, err := client.Do(req)
	result.latency = time.Since(startedAt)
	if err != nil {
		result.errorMessage = gerror.Wrap(err, "call images/edits upstream").Error()
		return result, false
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxImagesEditRequestBody+1))
	result.status = resp.StatusCode
	result.headers = resp.Header.Clone()
	if readErr != nil {
		result.errorMessage = gerror.Wrap(readErr, "read images/edits upstream response").Error()
		return result, false
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.body = responseBody
		result.errorMessage = upstreamError(responseBody, resp.Status)
		return result, false
	}
	result.body = responseBody
	result.tokens = parseJSONUsage(responseBody)
	copyResponseHeaders(writer.Header(), result.headers)
	writer.WriteHeader(resp.StatusCode)
	if _, err = writer.Write(responseBody); err != nil {
		result.errorMessage = err.Error()
		return result, true
	}
	result.wroteBytes = true
	return result, true
}

// parseImagesEditMultipart 把 multipart 请求体解析为有序字段列表，保持原始字段顺序。
func parseImagesEditMultipart(body []byte, contentType string) ([]imagesEditPart, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, gerror.New("images/edits request content type must be multipart/form-data")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	parts := make([]imagesEditPart, 0, 8)
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return nil, gerror.Wrap(partErr, "read images/edits multipart request")
		}
		content, readErr := io.ReadAll(io.LimitReader(part, maxImagesEditRequestBody+1))
		if readErr != nil {
			_ = part.Close()
			return nil, gerror.Wrap(readErr, "read images/edits multipart field")
		}
		if len(content) > maxImagesEditRequestBody {
			_ = part.Close()
			return nil, gerror.New("images/edits multipart field exceeds 32 MiB")
		}
		parts = append(parts, imagesEditPart{
			name:        part.FormName(),
			value:       content,
			fileName:    part.FileName(),
			contentType: strings.TrimSpace(part.Header.Get("Content-Type")),
		})
		_ = part.Close()
	}
	if len(parts) == 0 {
		return nil, gerror.New("images/edits request body is empty")
	}
	return parts, nil
}

// encodeImagesEditParts 按上游模型名重建 multipart 请求体，其余字段与文件原样透传。
func encodeImagesEditParts(parts []imagesEditPart, upstreamModel string) ([]byte, string, error) {
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	for _, part := range parts {
		if part.name == "" {
			continue
		}
		if part.fileName == "" {
			value := string(part.value)
			if part.name == "model" {
				value = upstreamModel
			}
			if err := writer.WriteField(part.name, value); err != nil {
				return nil, "", gerror.Wrap(err, "encode images/edits request")
			}
			continue
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipartQuotes(part.name), escapeMultipartQuotes(part.fileName)))
		fileType := part.contentType
		if fileType == "" {
			fileType = "application/octet-stream"
		}
		header.Set("Content-Type", fileType)
		partWriter, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", gerror.Wrap(err, "encode images/edits request")
		}
		if _, err = partWriter.Write(part.value); err != nil {
			return nil, "", gerror.Wrap(err, "encode images/edits request")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", gerror.Wrap(err, "encode images/edits request")
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

// escapeMultipartQuotes 按 multipart 规范转义头部内的反斜杠与双引号。
func escapeMultipartQuotes(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

// imagesEditTextPart 返回指定文本字段的值（同名多值时取第一个）。
func imagesEditTextPart(parts []imagesEditPart, name string) string {
	for _, part := range parts {
		if part.name == name && part.fileName == "" {
			return strings.TrimSpace(string(part.value))
		}
	}
	return ""
}

// imagesEditHasImage 判断请求是否携带参考图文件（image / image[] / image_1 等命名均接受）。
func imagesEditHasImage(parts []imagesEditPart) bool {
	for _, part := range parts {
		if part.fileName != "" && strings.HasPrefix(part.name, "image") {
			return true
		}
	}
	return false
}

// imagesEditPromptProbe 把 multipart 关键字段还原成 JSON，供统一敏感词检查使用。
func imagesEditPromptProbe(model string, parts []imagesEditPart) []byte {
	probe, err := json.Marshal(map[string]string{
		"model":  model,
		"prompt": imagesEditTextPart(parts, "prompt"),
	})
	if err != nil {
		return nil
	}
	return probe
}
