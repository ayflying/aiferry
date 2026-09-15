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
	"github.com/tidwall/sjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func (s *sRelay) attempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, originalBody []byte, candidate Candidate, stream bool, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, sensitiveDataRestorer *sensitiveDataRestorer) (attemptResult, bool, error) {
	advancedConfig, err := channel.ParseAdvancedConfig([]byte(candidate.AdvancedConfig))
	if err != nil {
		return attemptResult{}, false, err
	}
	// 协议转换开关：渠道高级配置显式指定时以渠道为准，缺省跟随系统设置。
	conversionEnabled := settings.ProtocolConversionEnabled
	if advancedConfig.ProtocolConversion != nil {
		conversionEnabled = *advancedConfig.ProtocolConversion
	}
	primary := s.preferredProtocolPlan(ctx, endpoint, candidate, conversionEnabled)
	result, handled, attemptErr := s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, userID, apiKeyID, settings, advancedConfig, primary, sensitiveDataRestorer)
	needsFallback := protocol.ShouldFallback(result.status, result.body) || s.missingBillableUsage(candidate, endpoint, result)
	// 关闭协议转换时不做端点回退：AlternatePlan 给出的备选端点必然要求转换，
	// 继续回退等于绕过开关。此时把上游的原始失败结果交还给客户端。
	if handled || attemptErr != nil || !needsFallback || !conversionEnabled {
		return result, handled, attemptErr
	}
	fallback, ok := protocol.AlternatePlan(endpoint, primary)
	if !ok {
		return result, handled, attemptErr
	}
	return s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, userID, apiKeyID, settings, advancedConfig, fallback, sensitiveDataRestorer)
}

// preferredProtocolPlan 决定这次转发用哪个上游端点。allowConversion 为 false 时
// 锁定直连，不再把 gpt-* 请求转投 /responses，但也不阻断转发。
func (s *sRelay) preferredProtocolPlan(ctx context.Context, endpoint string, candidate Candidate, allowConversion bool) protocol.Plan {
	if !allowConversion {
		return protocol.DirectPlan(endpoint)
	}
	// 只提供 Chat Completions 的聚合上游先于模型名推断判定：转投 /responses
	// 必然失败再回退，等于每次请求多一次上游往返。
	if s.chatCompletionsOnly(ctx, candidate) {
		return protocol.PreferredChatCompletionsPlan(endpoint)
	}
	if candidate.ChannelType == "zhipu" && isZhipuResponsesBaseURL(candidate.BaseURL) {
		return protocol.PreferredResponsesPlan(endpoint)
	}
	// 协议能力由实际接收请求的上游模型决定，公开映射名仅供客户端路由使用。
	return protocol.PreferredPlan(endpoint, candidate.UpstreamName)
}

// chatCompletionsOnly 判断渠道类型是否声明「该上游只提供 Chat Completions 端点」。
// 渠道类型配置读取失败按未声明处理，退回按模型名推断，不阻断转发。
func (s *sRelay) chatCompletionsOnly(ctx context.Context, candidate Candidate) bool {
	if s.types == nil {
		return false
	}
	_, typeConfig, err := s.types.GetByCode(ctx, candidate.ChannelType)
	if err != nil {
		return false
	}
	return typeConfig.Protocol.ChatCompletionsOnly
}

func isZhipuResponsesBaseURL(baseURL string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "https://open.bigmodel.cn/api/v1")
}

func (s *sRelay) attemptWithProtocol(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, originalBody []byte, candidate Candidate, stream bool, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, advancedConfig channel.AdvancedConfig, plan protocol.Plan, sensitiveDataRestorer *sensitiveDataRestorer) (attemptResult, bool, error) {
	convertedBody, err := plan.ConvertRequest(originalBody)
	if err != nil {
		return attemptResult{}, false, err
	}
	if plan.Converts() && plan.UpstreamEndpoint() == protocol.ChatCompletionsEndpoint {
		// 协议转换会把 Responses 的 function_call 还原成 assistant 的 tool_calls，此时
		// 客户端是否携带过思考内容已经不可知（Responses 用独立的 reasoning 输入项）。
		// 转换后再补一次存档内容，让 Responses 客户端也能满足 thinking 模式上游的要求。
		convertedBody = s.restoreReasoningContent(ctx, convertedBody, apiKeyID)
	}
	body, err := prepareRequestBody(plan.UpstreamEndpoint(), convertedBody, candidate.UpstreamName, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
	}
	body, err = applyPromptCachePolicy(body, candidate, userID, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
	}
	if stream && plan.UpstreamEndpoint() == protocol.ChatCompletionsEndpoint {
		body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
	}
	requestCtx := ctx
	cancel := func() {}
	if !stream {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(settings.NonStreamTimeoutSeconds)*time.Second)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, candidate.BaseURL+plan.UpstreamEndpoint(), bytes.NewReader(body))
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "create upstream request")
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	// 鉴权头与组织/项目头由渠道类型声明统一决定，与模型测试共用同一实现。
	if err = s.applyUpstreamAuthHeaders(ctx, req, candidate); err != nil {
		return attemptResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	// OpenCode Go 等上游要求稳定的会话标识头，缺失时直接 400。
	applyOpencodeGoHeaders(req.Header, incomingHeaders, candidate, userID)
	client, err := s.channels.HTTPClientForProxy(candidate.ProxyURLCipher)
	if err != nil {
		return attemptResult{}, false, err
	}
	requestStartedAt := time.Now()
	var resp *http.Response
	if stream {
		resp, err = doStreamRequest(ctx, client, req, time.Duration(settings.StreamFirstByteTimeoutSeconds)*time.Second)
	} else {
		resp, err = client.Do(req)
	}
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "call upstream")
	}
	if !stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		// 思考内容必须在响应改写前捕获：ReasoningToContent 会把 reasoning_content
		// 合并进 content 并删除原字段，改写后再读就拿不到了。
		reasoningContent, reasoningField, reasoningToolCallIDs := captureBufferedReasoning(plan.UpstreamEndpoint(), responseBody)
		responseBody = normalizeResponseBody(plan.UpstreamEndpoint(), responseBody, candidate.UpstreamName, advancedConfig)
		result := attemptResult{status: resp.StatusCode, body: plan.ConvertResponse(responseBody), tokens: parseJSONUsage(responseBody), headers: responseHeaders(resp.Header, plan)}
		result.upstreamEndpoint = plan.UpstreamEndpoint()
		result.protocolConversion = plan.Conversion()
		result.reasoningContent = reasoningContent
		result.reasoningField = reasoningField
		result.reasoningToolCallIDs = reasoningToolCallIDs
		result.responseText, result.responseModel = captureBufferedResponse(plan.UpstreamEndpoint(), responseBody)
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			result.errorMessage = upstreamError(responseBody, resp.Status)
		}
		if readErr != nil {
			return result, false, gerror.Wrap(readErr, "read upstream response")
		}
		return result, false, nil
	}
	defer resp.Body.Close()
	flusher, _ := writer.(http.Flusher)
	result := attemptResult{status: resp.StatusCode, headers: responseHeaders(resp.Header, plan), upstreamEndpoint: plan.UpstreamEndpoint(), protocolConversion: plan.Conversion()}
	if plan.Converts() {
		result.headers.Set("Content-Type", "text/event-stream")
	}
	converter := protocol.NewStreamConverter(plan)
	if !plan.Converts() {
		converter = nil
	}
	capture := newStreamResponseCapture(plan.UpstreamEndpoint())
	reasoning := newReasoningCapture(plan.UpstreamEndpoint())
	streamRestorer := newSensitiveDataStreamRestorer(sensitiveDataRestorer)
	pending := make([][]byte, 0)
	pendingSize := 0
	committed := false
	writeOutput := func(output []byte) error {
		if !result.wroteBytes {
			recordFirstStreamOutput(&result, requestStartedAt)
			copyResponseHeaders(writer.Header(), result.headers)
			writer.WriteHeader(resp.StatusCode)
		}
		if _, err = writer.Write(output); err != nil {
			// 写入失败通常意味着下游客户端已经断开，标记出来避免把客户端行为
			// 计入渠道与模型的失败评分。
			result.writerFailed = true
			return err
		}
		result.wroteBytes = true
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	flushPending := func() error {
		for _, output := range pending {
			if err = writeOutput(output); err != nil {
				return err
			}
		}
		pending = nil
		pendingSize = 0
		return nil
	}
	// terminateTruncated 在流已经被截断时补发显式终止帧（错误事件 + 结束标记）。
	// 只在系统设置允许、已经写出内容且客户端仍在连接时发送：客户端已断开时写什么
	// 都不会被收到。客户端也不能再收到重放，只能据此提示「上游中断」。
	terminateTruncated := func(terminal streamTerminal) {
		if !settings.StreamFailureEventEnabled || !result.wroteBytes || result.writerFailed || ctx.Err() != nil {
			return
		}
		for _, line := range streamTerminalLines(plan.ClientEndpoint(), terminal) {
			for _, restored := range streamRestorer.restoreSSELine(line) {
				if _, writeErr := writer.Write(restored); writeErr != nil {
					result.writerFailed = true
					return
				}
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	firstByteTimeout := time.Duration(settings.StreamFirstByteTimeoutSeconds)*time.Second - time.Since(requestStartedAt)
	if firstByteTimeout <= 0 {
		return result, false, upstreamTimeoutError{phase: "stream first-byte"}
	}
	scanner := bufio.NewScanner(newStreamTimeoutReader(resp.Body, firstByteTimeout, time.Duration(settings.StreamIdleTimeoutSeconds)*time.Second))
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		line := append(append([]byte(nil), scanner.Bytes()...), '\n')
		// 思考内容必须在响应改写前捕获：ReasoningToContent 会把 reasoning_content
		// 合并进 content 并删除原字段，改写后再观察就拿不到了。
		reasoning.Observe(line)
		line = normalizeSSELine(plan.UpstreamEndpoint(), line, candidate.UpstreamName, advancedConfig)
		capture.Observe(line)
		if failure, failed := parseStreamFailure(line); failed {
			result.status = failure.status
			result.body = failure.body
			result.errorMessage = failure.message
			// 上游在流内报错。已经向客户端写出内容时无法切换候选重放，补发显式
			// 终止帧后就地收尾；否则交回上层继续尝试下一个候选。
			if result.wroteBytes {
				terminateTruncated(streamTerminal{status: failure.status, message: failure.message})
				return result, true, nil
			}
			return result, false, nil
		}
		if streamPayloadHasVisibleOutput(line) {
			recordFirstStreamOutput(&result, requestStartedAt)
		}
		parseSSEUsage(line, &result.tokens)
		lines := [][]byte{line}
		if converter != nil {
			lines = converter.Transform(line)
		}
		for _, output := range lines {
			if len(output) == 0 {
				continue
			}
			output = normalizeSSELine(plan.ClientEndpoint(), output, candidate.UpstreamName, advancedConfig)
			for _, restoredOutput := range streamRestorer.restoreSSELine(output) {
				if !committed && !streamPayloadHasVisibleOutput(line) {
					pending = append(pending, restoredOutput)
					pendingSize += len(restoredOutput)
					if pendingSize <= maxPendingStreamBytes {
						continue
					}
					committed = true
					if err = flushPending(); err != nil {
						result.errorMessage = err.Error()
						return result, true, nil
					}
					continue
				}
				if !committed {
					committed = true
				}
				if err = flushPending(); err != nil {
					result.errorMessage = err.Error()
					return result, true, nil
				}
				if err = writeOutput(restoredOutput); err != nil {
					result.errorMessage = err.Error()
					return result, true, nil
				}
			}
		}
	}
	if converter != nil {
		for _, output := range converter.Complete() {
			if len(output) == 0 {
				continue
			}
			output = normalizeSSELine(plan.ClientEndpoint(), output, candidate.UpstreamName, advancedConfig)
			for _, restoredOutput := range streamRestorer.restoreSSELine(output) {
				if !committed {
					committed = true
					if err = flushPending(); err != nil {
						result.errorMessage = err.Error()
						return result, true, nil
					}
				}
				if err = writeOutput(restoredOutput); err != nil {
					result.errorMessage = err.Error()
					return result, true, nil
				}
			}
		}
	}
	if err = scanner.Err(); err != nil {
		result.errorMessage = err.Error()
		result.timedOut = isUpstreamTimeout(err)
		if !result.wroteBytes {
			return result, false, gerror.Wrap(err, "read upstream stream")
		}
	}
	if !result.wroteBytes {
		if err = flushPending(); err != nil {
			result.errorMessage = err.Error()
			return result, true, nil
		}
	}
	for _, output := range streamRestorer.flushPending() {
		if !committed {
			committed = true
			if err = flushPending(); err != nil {
				result.errorMessage = err.Error()
				return result, true, nil
			}
		}
		if err = writeOutput(output); err != nil {
			result.errorMessage = err.Error()
			return result, true, nil
		}
	}
	result.responseText = capture.Text()
	result.responseModel = capture.Model()
	result.streamCompleted = capture.Completed()
	result.reasoningContent = reasoning.Reasoning()
	result.reasoningField = reasoning.Field()
	result.reasoningToolCallIDs = reasoning.ToolCallIDs()
	// 上游先结束但没有给出结束标记（空闲超时、连接被半路关闭、干净 EOF）：
	// 客户端收到的是半截回答，补发终止帧让它能明确提示，而不是静默停下。
	if streamTruncated(true, result) {
		terminateTruncated(streamTerminal{status: result.status, message: result.errorMessage})
	}
	return result, true, nil
}

func responseHeaders(headers http.Header, plan protocol.Plan) http.Header {
	result := headers.Clone()
	if plan.Converts() {
		result.Del("Content-Length")
		result.Del("Content-Encoding")
		result.Set("Content-Type", "application/json")
	}
	return result
}
