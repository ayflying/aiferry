package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// finishFailedRequest 保持最终失败响应的既有透传语义，并统一保存一次报文。
func (s *sRelay) finishFailedRequest(writer http.ResponseWriter, requestID string, candidate Candidate, requestedModel, endpoint string, isStream bool, attempts int, body []byte, result attemptResult, restorer *sensitiveDataRestorer) {
	// 所有候选都失败后，只要最后一次是「尚未向客户端写出任何内容的 4xx/5xx」，就把上游
	// 响应透传给客户端。4xx/5xx 都不再是对整次请求的终态判决——上游会把渠道侧故障包装成
	// 普通 400/502（如聚合层的 "Upstream request failed"），因此 4xx 随候选链一路重试到
	// 最后；改写成网关级 5xx（503 server_error，客户端会据此重试）反而丢失上游的原始错误
	// 信息，属于误导。5xx 透传同样是必要的：此前最终 5xx 只写状态不写体，客户端拿到的是
	// 空 200，看不到任何错误内容。
	if !result.wroteBytes && result.status >= http.StatusBadRequest {
		s.writeBufferedResponse(writer, result.status, restorer.restoreBufferedResponse(clientVisibleErrorBody(result)), result.headers)
	}
	// 失败请求同样落盘：保留既有请求脱敏与上游原始错误报文，不重复存储透传响应。
	s.savePayloadLog(requestID, candidate, requestedModel, endpoint, isStream, attempts, body, result, json.RawMessage(result.body))
}

// clientVisibleErrorBody 把上游错误体转成客户端必然可读的标准 OpenAI 错误信封。上游错误
// 体五花八门：workbuddy 内容风控返回 {"code":11128,...}，没有 error 字段，标准 SDK
// 与客户端错误分类层都渲染不出来，用户只能看到「有状态码但没有任何错误内容」。已是
// 错误信封形状（顶层 error 字段）的原样透传；其余包装为
// {"error":{"type":"upstream_error","message":<upstreamerror.Message 完整文案>}}——文案
// 保留 code/msg/displayTips 等原始上下文，便于用户按提示重发或反馈。
func clientVisibleErrorBody(result attemptResult) []byte {
	fallback := fmt.Sprintf("upstream returned HTTP %d", result.status)
	if len(bytes.TrimSpace(result.body)) == 0 {
		message := result.errorMessage
		if strings.TrimSpace(message) == "" {
			message = fallback
		}
		return openAIError("upstream_error", message)
	}
	if hasStandardErrorEnvelope(result.body) {
		return result.body
	}
	return openAIError("upstream_error", upstreamError(result.body, fallback))
}

// hasStandardErrorEnvelope 判断错误体是否已是错误信封形状（顶层 error 字段存在且非
// null，对象或字符串均可——{"error":"..."} 客户端同样可渲染）。已是错误形状的原样
// 透传，避免二次包装破坏上游自带的 message/type 结构。
func hasStandardErrorEnvelope(body []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	raw, ok := probe["error"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	return true
}
