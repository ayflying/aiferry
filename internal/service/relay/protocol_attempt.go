package relay

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/channel"
)

func (s *Service) attempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, originalBody []byte, candidate Candidate, stream bool, startedAt time.Time, settings adminapi.SystemResilienceSettingsInput) (attemptResult, bool, error) {
	advancedConfig, err := channel.ParseAdvancedConfig([]byte(candidate.AdvancedConfig))
	if err != nil {
		return attemptResult{}, false, err
	}
	primary := directProtocolPlan(endpoint)
	result, handled, attemptErr := s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, startedAt, settings, advancedConfig, primary)
	if handled || attemptErr != nil || !advancedConfig.EnableProtocolConversion || !shouldFallbackWithProtocolConversion(result.status, result.body) {
		return result, handled, attemptErr
	}
	fallback, ok := fallbackProtocolPlan(endpoint)
	if !ok {
		return result, handled, attemptErr
	}
	return s.attemptWithProtocol(ctx, writer, incomingHeaders, originalBody, candidate, stream, startedAt, settings, advancedConfig, fallback)
}

func (s *Service) attemptWithProtocol(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, originalBody []byte, candidate Candidate, stream bool, startedAt time.Time, settings adminapi.SystemResilienceSettingsInput, advancedConfig channel.AdvancedConfig, plan protocolPlan) (attemptResult, bool, error) {
	convertedBody, err := plan.convertRequest(originalBody)
	if err != nil {
		return attemptResult{}, false, err
	}
	body, err := prepareRequestBody(plan.upstreamEndpoint, convertedBody, candidate.UpstreamName, advancedConfig)
	if err != nil {
		return attemptResult{}, false, err
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
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, candidate.BaseURL+plan.upstreamEndpoint, bytes.NewReader(body))
	if err != nil {
		return attemptResult{}, false, gerror.Wrap(err, "create upstream request")
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	req.Header.Set("Authorization", "Bearer "+apiKey)
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
		responseBody = normalizeResponseBody(plan.upstreamEndpoint, responseBody, candidate.UpstreamName, advancedConfig)
		result := attemptResult{status: resp.StatusCode, body: plan.convertResponse(responseBody), tokens: parseJSONUsage(responseBody), headers: responseHeaders(resp.Header, plan)}
		result.upstreamEndpoint = plan.upstreamEndpoint
		result.protocolConversion = plan.conversion
		if readErr != nil {
			return result, false, gerror.Wrap(readErr, "read upstream response")
		}
		if retryableStatusForRules(resp.StatusCode, settings.RetryStatusCodes) {
			result.errorMessage = upstreamError(responseBody, resp.Status)
			return result, false, nil
		}
		if writer != nil {
			s.writeBufferedResponse(writer, resp.StatusCode, result.body, result.headers)
		}
		return result, true, nil
	}
	defer resp.Body.Close()
	flusher, _ := writer.(http.Flusher)
	result := attemptResult{status: resp.StatusCode, headers: responseHeaders(resp.Header, plan), upstreamEndpoint: plan.upstreamEndpoint, protocolConversion: plan.conversion}
	if plan.converts() {
		result.headers.Set("Content-Type", "text/event-stream")
	}
	converter := newProtocolStreamConverter(plan)
	if !plan.converts() {
		converter = nil
	}
	firstByteTimeout := time.Duration(settings.StreamFirstByteTimeoutSeconds)*time.Second - time.Since(requestStartedAt)
	if firstByteTimeout <= 0 {
		return result, false, upstreamTimeoutError{phase: "stream first-byte"}
	}
	scanner := bufio.NewScanner(newStreamTimeoutReader(resp.Body, firstByteTimeout, time.Duration(settings.StreamIdleTimeoutSeconds)*time.Second))
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	firstChunk := true
	for scanner.Scan() {
		line := append(append([]byte(nil), scanner.Bytes()...), '\n')
		line = normalizeSSELine(plan.upstreamEndpoint, line, candidate.UpstreamName, advancedConfig)
		parseSSEUsage(line, &result.tokens)
		lines := [][]byte{line}
		if converter != nil {
			lines = converter.Transform(line)
		}
		for _, output := range lines {
			if len(output) == 0 {
				continue
			}
			output = normalizeSSELine(plan.clientEndpoint, output, candidate.UpstreamName, advancedConfig)
			if firstChunk {
				first := time.Since(startedAt).Milliseconds()
				result.firstTokenMs = &first
				firstChunk = false
			}
			if !result.wroteBytes {
				copyResponseHeaders(writer.Header(), result.headers)
				writer.WriteHeader(resp.StatusCode)
			}
			if _, err = writer.Write(output); err != nil {
				result.errorMessage = err.Error()
				return result, true, nil
			}
			result.wroteBytes = true
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	if converter != nil {
		for _, output := range converter.Complete() {
			if len(output) == 0 {
				continue
			}
			output = normalizeSSELine(plan.clientEndpoint, output, candidate.UpstreamName, advancedConfig)
			if !result.wroteBytes {
				copyResponseHeaders(writer.Header(), result.headers)
				writer.WriteHeader(resp.StatusCode)
			}
			if _, err = writer.Write(output); err != nil {
				result.errorMessage = err.Error()
				return result, true, nil
			}
			result.wroteBytes = true
			if flusher != nil {
				flusher.Flush()
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
		copyResponseHeaders(writer.Header(), resp.Header)
		writer.WriteHeader(resp.StatusCode)
	}
	return result, true, nil
}

func responseHeaders(headers http.Header, plan protocolPlan) http.Header {
	result := headers.Clone()
	if plan.converts() {
		result.Del("Content-Length")
		result.Del("Content-Encoding")
		result.Set("Content-Type", "application/json")
	}
	return result
}
