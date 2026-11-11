package relay

import (
	"net/http"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func codexUnsupportedResult(candidate Candidate) (attemptResult, bool) {
	if candidate.ChannelType != "codex" {
		return attemptResult{}, false
	}
	message := "Codex 官方渠道仅支持 Responses；不支持音频、图片、视频或向量端点"
	return attemptResult{status: http.StatusBadRequest, body: openAIError("invalid_request_error", message), errorMessage: message}, true
}

func codexEndpointAllowed(endpoint string, conversionEnabled bool) bool {
	return endpoint == protocol.ResponsesEndpoint || (endpoint == protocol.ChatCompletionsEndpoint && conversionEnabled)
}

// aggregateForcedStream reuses the existing Chat SSE aggregator and protocol
// converters. Native Responses keeps its terminal response intact (including
// encrypted reasoning and tool output); stream failures are never JSON success.
func aggregateForcedStream(raw []byte, plan protocol.Plan) ([]byte, *streamFailure) {
	if plan.UpstreamEndpoint() != protocol.ResponsesEndpoint {
		return aggregateChatCompletionSSE(raw), nil
	}
	for _, line := range splitSSELines(raw) {
		if failure, failed := parseStreamFailure(line); failed {
			return nil, &failure
		}
		payload, _, valid := relaySSEDataPayload(line)
		if valid && gjson.GetBytes(payload, "type").String() == "response.completed" {
			response := gjson.GetBytes(payload, "response")
			if response.IsObject() {
				return []byte(response.Raw), nil
			}
		}
	}
	failure := &streamFailure{status: http.StatusBadGateway, message: streamTruncatedReason}
	failure.body = openAIError("upstream_error", failure.message)
	return nil, failure
}
