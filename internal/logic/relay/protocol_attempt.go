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

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
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
	primary := s.preferredProtocolPlan(ctx, endpoint, candidate, conversionEnabled, settings)
	primaryStartedAt := time.Now()
	result, handled, attemptErr := s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, userID, apiKeyID, settings, advancedConfig, primary, sensitiveDataRestorer)
	// 历史说明：0.6.20 曾在这里做「首跳 11128 后扰动重发一次」的自愈重试；0.6.25 起
	// 扰动已提前到首跳（见 attemptWithProtocol 的 workbuddy 块），重发同一扰动体在
	// 上游窗口内必然重演失败，只会放大对风控的刺激，因此移除该重试。跨窗口的重试
	// 由最后候选的风控退避（retry_policy.go）与组合冷却负责。
	// 首跳耗时先记在结果上：协议回退时它会成为调用流程步骤的耗时快照；
	// 不回退时调用方会用整次尝试的耗时覆盖它，不影响既有语义。
	result.latency = time.Since(primaryStartedAt)
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
	// 协议回退是网关对同一候选发起的第二次真实上游调用：首跳的失败必须登记进
	// 调用流程，否则用户只看到「上游尝试 N 次」，不知道网关换过端点重试并自愈。
	primaryStep := newAttemptFlowStep(candidate.ChannelName, result)
	fallbackResult, fallbackHandled, fallbackErr := s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, userID, apiKeyID, settings, advancedConfig, fallback, sensitiveDataRestorer)
	fallbackResult.precedingFlow = append(fallbackResult.precedingFlow, primaryStep)
	return fallbackResult, fallbackHandled, fallbackErr
}

// preferredProtocolPlan 决定这次转发用哪个上游端点。allowConversion 为 false 时
// 锁定直连，不再把 gpt-* 请求转投 /responses，但也不阻断转发。
func (s *sRelay) preferredProtocolPlan(ctx context.Context, endpoint string, candidate Candidate, allowConversion bool, settings adminapi.SystemResilienceSettingsInput) protocol.Plan {
	if !allowConversion {
		return protocol.DirectPlan(endpoint)
	}
	// 渠道类型声明的协议偏好先于模型名推断判定：转投必然失败的端点
	// 等于每次请求多一次上游往返。
	typeConfig, typeOK := s.protocolTypeConfig(ctx, candidate)
	if typeOK && typeConfig.Protocol.ChatCompletionsOnly {
		return protocol.PreferredChatCompletionsPlan(endpoint)
	}
	// Messages 名单判定：渠道类型声明的名单优先（类型专属知识），类型级
	// 未配置或读取失败时退回全局名单（系统设置，union-*、claude-* 等）。
	// 命中即把该模型切到 Anthropic Messages 端点。
	if !typeOK {
		typeConfig = channeltype.Config{}
	}
	if messagesURL := channeltype.ResolveMessagesEndpoint(typeConfig, settings, candidate.UpstreamName); messagesURL != "" {
		return protocol.PreferredAnthropicPlan(endpoint, messagesURL)
	}
	if candidate.ChannelType == "zhipu" && isZhipuResponsesBaseURL(candidate.BaseURL) {
		return protocol.PreferredResponsesPlan(endpoint)
	}
	// 协议能力由实际接收请求的上游模型决定，公开映射名仅供客户端路由使用。
	return protocol.PreferredPlan(endpoint, candidate.UpstreamName)
}

// protocolTypeConfig 读取候选渠道的类型配置，供协议端点判定使用。
// 读取失败按未声明处理，退回按模型名推断，不阻断转发。
func (s *sRelay) protocolTypeConfig(ctx context.Context, candidate Candidate) (channeltype.Config, bool) {
	if s.types == nil {
		return channeltype.Config{}, false
	}
	_, typeConfig, err := s.types.GetByCode(ctx, candidate.ChannelType)
	if err != nil {
		return channeltype.Config{}, false
	}
	return typeConfig, true
}

// declaresUpstreamStreamOnly 判定渠道类型是否声明「上游只接受流式请求」。
// 读取失败按未声明处理，退回按客户端原样转发，不阻断请求。
func (s *sRelay) declaresUpstreamStreamOnly(ctx context.Context, candidate Candidate) bool {
	typeConfig, ok := s.protocolTypeConfig(ctx, candidate)
	return ok && typeConfig.Protocol.ForceUpstreamStream
}

func isZhipuResponsesBaseURL(baseURL string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "https://open.bigmodel.cn/api/v1")
}

// resolveUpstreamURL 拼接上游地址：端点是完整 URL（如渠道类型声明的
// Messages 地址）时原样使用，否则拼在渠道根地址之后。
func resolveUpstreamURL(baseURL, endpoint string) string {
	if channeltype.IsAbsoluteHTTPURL(endpoint) {
		return endpoint
	}
	return baseURL + endpoint
}

func (s *sRelay) attemptWithProtocol(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, originalBody []byte, candidate Candidate, stream bool, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, advancedConfig channel.AdvancedConfig, plan protocol.Plan, sensitiveDataRestorer *sensitiveDataRestorer) (attemptResult, bool, error) {
	convertedBody, err := plan.ConvertRequest(originalBody)
	if err != nil {
		return attemptResult{}, false, err
	}
	// 思考历史由客户端提供；不从 Redis 回填、不补空串、不改写思考字段名。
	body, err := prepareRequestBody(plan.UpstreamEndpoint(), convertedBody, candidate.UpstreamName, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
	}
	if candidate.ChannelType == "workbuddy" && plan.UpstreamEndpoint() == protocol.ChatCompletionsEndpoint {
		body, err = channel.ApplyWorkBuddyBody(body)
		if err != nil {
			return attemptResult{}, false, err
		}
		// 2026-10-26 生产实证（afreq_50304d2f…，10 次上游尝试全 11128）：带指纹的
		// 原始体首发必被拒，且这次拒绝本身会把上游推进「账号级拦截窗口」——窗口内
		// 连扰动体也一并被拒，客户端重发继续喂窗口，形成「用户永远失败」的死循环；
		// 同一份字节在窗口外单发重放则一次性通过。因此把零宽空格扰动从「失败后重试」
		// 提前到首跳：第一次上游调用就是扰动体，不再用注定失败的原始体去触发窗口。
		// 扰动幂等（已含 U+200B 的文本跳过），语义不变、对被检内容不可见。
		if perturbed, ok := channel.PerturbWorkBuddyBody(body); ok {
			body = perturbed
		}
	}
	body, err = applyPromptCachePolicy(body, candidate, userID, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
	}
	// OpenCode Zen 免费层指纹校验只认 chat completions 形态：注入官方工具
	// 定义与流式改写（详见 channel.ApplyOpenCodeFreeBody）。协议转换到
	// responses/messages 的请求不注入，避免破坏非 chat 结构。
	upstreamURL := resolveUpstreamURL(candidate.BaseURL, plan.UpstreamEndpoint())
	forcedStream := false
	if channel.IsOpenCodeFreeLane(candidate.ChannelType, candidate.BaseURL) && plan.UpstreamEndpoint() == protocol.ChatCompletionsEndpoint {
		var forced bool
		body, forced, err = channel.ApplyOpenCodeFreeBody(body)
		if err != nil {
			return attemptResult{}, false, err
		}
		forcedStream = forced
		// 2026-10-14 起匿名推理端点从 …/zen 迁到 …/inference，chat 请求
		// 必须改写基址（模型目录与用量查询仍走原基址，无需改写）。
		upstreamURL = channel.OpenCodeFreeUpstreamURL(candidate.BaseURL, plan.UpstreamEndpoint())
	} else if plan.UpstreamEndpoint() == protocol.ChatCompletionsEndpoint && s.declaresUpstreamStreamOnly(ctx, candidate) {
		// 该上游只接受流式请求（非流式直接被网关 400 且无响应体）：客户端要非流式
		// JSON 时把请求体改成 stream=true，回包处再把 SSE 聚合回 chat.completion。
		if !gjson.GetBytes(body, "stream").Bool() {
			body, _ = sjson.SetBytes(body, "stream", true)
			body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
			forcedStream = true
		}
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
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "create upstream request")
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	// 鉴权头与组织/项目头由渠道类型声明统一决定，与模型测试共用同一实现。
	if err = s.applyUpstreamAuthHeaders(ctx, req, candidate); err != nil {
		return attemptResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Anthropic Messages 上游要求显式协议版本头，缺失时部分网关直接 400。
	if plan.IsAnthropicUpstream() {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	// OpenCode Go 等上游要求稳定的会话标识头，缺失时直接 400。
	applyOpencodeGoHeaders(req.Header, incomingHeaders, candidate, userID)
	// 代理按密钥固定序号模运算配对；代理层失败仅本密钥顺延下一个（不随机、
	// 不影响其它密钥的映射），详见 relay.doViaProxy / channel.DoViaProxies。
	requestStartedAt := time.Now()
	var resp *http.Response
	resp, err = s.doViaProxy(ctx, req, candidate, func(client *http.Client) (*http.Response, error) {
		if stream {
			return doStreamRequest(ctx, client, req, time.Duration(settings.StreamFirstByteTimeoutSeconds)*time.Second)
		}
		return client.Do(req)
	})
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "call upstream")
	}
	if !stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		// 强制流式车道（OpenCode 免费层指纹校验，或渠道类型声明只接受流式）：
		// 客户端要非流式 JSON，但请求体已被改成 stream=true，上游回的是 SSE。
		// 把分片聚合成 chat.completion 后再走非流式回包/计费。
		forcedStreamAggregate := forcedStream && !stream && resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr == nil
		if forcedStreamAggregate {
			responseBody = aggregateChatCompletionSSE(responseBody)
		}
		// 思考内容在响应改写前捕获，仅用于报文记录；改写本身不再搬移任何字段。
		// 唯一的例外是「正文污染守卫」：思考块泄漏搬回 reasoning_content，
		// 客户端协议标记泄漏则判定为上游坏响应，不交给客户端。
		responseBody, markupLeak := inspectBufferedContent(plan.UpstreamEndpoint(), responseBody)
		if markupLeak {
			message := "上游把客户端内部标记（工具协议或摘要信封）写进了正文，未作为回答交付"
			return attemptResult{
				status:       http.StatusBadGateway,
				body:         openAIError("upstream_error", message),
				errorMessage: message,
				headers:      responseHeaders(resp.Header, plan),
			}, false, nil
		}
		reasoningContent, reasoningField, reasoningToolCallIDs := captureBufferedReasoning(plan.UpstreamEndpoint(), responseBody)
		responseBody = normalizeResponseBody(plan.UpstreamEndpoint(), responseBody, candidate.UpstreamName, advancedConfig)
		result := attemptResult{status: resp.StatusCode, body: plan.ConvertResponse(responseBody), tokens: parseJSONUsage(responseBody), headers: responseHeaders(resp.Header, plan)}
		if forcedStreamAggregate {
			// 上游按流式回了 text/event-stream，聚合后给客户端的是 JSON，必须改写
			// Content-Type，否则客户端按 SSE 解析 JSON 会直接报错。
			result.headers.Set("Content-Type", "application/json")
		}
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
	// 正文污染守卫：只在正文第一次出现时判定一次，认出思考块就把正文改写成
	// reasoning_content，认出客户端协议标记则拒绝该响应。形态不符时原样放行。
	contentGuard := newStreamContentGuard(plan.UpstreamEndpoint())
	payloadCapture := newPayloadStreamCapture(plan.ClientEndpoint())
	streamRestorer := newSensitiveDataStreamRestorer(sensitiveDataRestorer)
	pending := make([][]byte, 0)
	pendingSize := 0
	committed := false
	result.payloadCapture = payloadCapture
	writeOutput := func(output []byte) error {
		// 报文聚合必须在写入前观察：output 已经过协议转换与敏感数据还原，
		// 与客户端实际收到的字节一致，正文污染问题以这份内容定性。
		payloadCapture.observe(output)
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
		// 正文污染守卫先于捕获与转换：后面的报文记录、协议转换与客户端收到的
		// 字节都以守卫后的形态为准，客户端不会再拿到被标签或协议标记包裹的正文。
		var markupLeak bool
		if line, markupLeak = contentGuard.Observe(line); markupLeak {
			// 上游把自家客户端的工具协议标记当正文写出：既不能当回答交付（客户端会
			// 解析失败并把垃圾存进历史），也还原不成正确的工具调用。尚未写出内容时
			// 交回候选循环切换；已经写出内容只能就地收尾，补发终止帧说明原因。
			message := "上游把客户端内部标记（工具协议或摘要信封）写进了正文，未作为回答交付"
			result.status = http.StatusBadGateway
			result.body = openAIError("upstream_error", message)
			result.errorMessage = message
			if result.wroteBytes {
				terminateTruncated(streamTerminal{status: http.StatusBadGateway, message: message})
				return result, true, nil
			}
			return result, false, nil
		}
		if line == nil {
			continue
		}
		// 思考内容在响应改写前捕获，仅用于报文记录；改写本身不再搬移任何字段。
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
	result.responseHasToolCalls = capture.HasToolCalls()
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
