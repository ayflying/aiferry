package relay

import (
	"encoding/json"
	"net/http"
)

// finishFailedRequest 保持最终失败响应的既有透传语义，并统一保存一次报文。
func (s *sRelay) finishFailedRequest(writer http.ResponseWriter, requestID string, candidate Candidate, requestedModel, endpoint string, isStream bool, attempts int, body []byte, result attemptResult, restorer *sensitiveDataRestorer) {
	// 所有候选都失败后，只要最后一次是「尚未向客户端写出任何内容的 4xx」，就把上游响应
	// 原样透传。4xx 不再是对整次请求的终态判决——上游会把渠道侧故障包装成普通 400（如
	// 聚合层的 "Upstream request failed"），因此 4xx 随候选链一路重试到最后；改写成网关级
	// 5xx（503 server_error，客户端会据此重试）反而丢失上游的原始错误信息，属于误导。
	if !result.wroteBytes && result.status >= http.StatusBadRequest && result.status < http.StatusInternalServerError {
		s.writeBufferedResponse(writer, result.status, restorer.restoreBufferedResponse(result.body), result.headers)
	}
	// 失败请求同样落盘：保留既有请求脱敏与上游原始错误报文，不重复存储透传响应。
	s.savePayloadLog(requestID, candidate, requestedModel, endpoint, isStream, attempts, body, result, json.RawMessage(result.body))
}
