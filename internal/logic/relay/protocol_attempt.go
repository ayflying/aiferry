package relay

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/sjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func (s *sRelay) attempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, originalBody []byte, candidate Candidate, stream bool, startedAt time.Time, userID uint64, settings adminapi.SystemResilienceSettingsInput, sensitiveDataRestorer *sensitiveDataRestorer) (attemptResult, bool, error) {
	advancedConfig, err := channel.ParseAdvancedConfig([]byte(candidate.AdvancedConfig))
	if err != nil {
		return attemptResult{}, false, err
	}
	primary := protocol.PreferredPlan(endpoint, candidate.PublicName)
	result, handled, attemptErr := s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, startedAt, userID, settings, advancedConfig, primary, sensitiveDataRestorer)
	needsFallback := protocol.ShouldFallback(result.status, result.body) || s.missingBillableUsage(candidate, endpoint, result)
	if handled || attemptErr != nil || !needsFallback {
		return result, handled, attemptErr
	}
	fallback, ok := protocol.AlternatePlan(endpoint, primary)
	if !ok {
		return result, handled, attemptErr
	}
	return s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, startedAt, userID, settings, advancedConfig, fallback, sensitiveDataRestorer)
}

func (s *sRelay) attemptWithProtocol(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, originalBody []byte, candidate Candidate, stream bool, startedAt time.Time, userID uint64, settings adminapi.SystemResilienceSettingsInput, advancedConfig channel.AdvancedConfig, plan protocol.Plan, sensitiveDataRestorer *sensitiveDataRestorer) (attemptResult, bool, error) {
	convertedBody, err := plan.ConvertRequest(originalBody)
	if err != nil {
		return attemptResult{}, false, err
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
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return attemptResult{}, false, err
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
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	if candidate.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", candidate.OrganizationID)
	}
	if candidate.ProjectID != "" {
		req.Header.Set("OpenAI-Project", candidate.ProjectID)
	}
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
		responseBody = normalizeResponseBody(plan.UpstreamEndpoint(), responseBody, candidate.UpstreamName, advancedConfig)
		result := attemptResult{status: resp.StatusCode, body: plan.ConvertResponse(responseBody), tokens: parseJSONUsage(responseBody), headers: responseHeaders(resp.Header, plan)}
		result.upstreamEndpoint = plan.UpstreamEndpoint()
		result.protocolConversion = plan.Conversion()
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
	streamRestorer := newSensitiveDataStreamRestorer(sensitiveDataRestorer)
	pending := make([][]byte, 0)
	pendingSize := 0
	committed := false
	writeOutput := func(output []byte) error {
		if !result.wroteBytes {
			recordFirstStreamOutput(&result, startedAt)
			copyResponseHeaders(writer.Header(), result.headers)
			writer.WriteHeader(resp.StatusCode)
		}
		if _, err = writer.Write(output); err != nil {
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
	firstByteTimeout := time.Duration(settings.StreamFirstByteTimeoutSeconds)*time.Second - time.Since(requestStartedAt)
	if firstByteTimeout <= 0 {
		return result, false, upstreamTimeoutError{phase: "stream first-byte"}
	}
	scanner := bufio.NewScanner(newStreamTimeoutReader(resp.Body, firstByteTimeout, time.Duration(settings.StreamIdleTimeoutSeconds)*time.Second))
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		line := append(append([]byte(nil), scanner.Bytes()...), '\n')
		line = normalizeSSELine(plan.UpstreamEndpoint(), line, candidate.UpstreamName, advancedConfig)
		capture.Observe(line)
		if failure, failed := parseStreamFailure(line); failed {
			result.status = failure.status
			result.body = failure.body
			result.errorMessage = failure.message
			// The client already received output; terminate this stream so it can retry.
			// Sending an SSE error event here would stop Codex from retrying the request.
			if result.wroteBytes {
				return result, true, nil
			}
			return result, false, nil
		}
		if streamPayloadHasVisibleOutput(line) {
			recordFirstStreamOutput(&result, startedAt)
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
