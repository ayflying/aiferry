package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/apikey"
)

const (
	maxVideoRequestBody = 64 << 20
	videoTaskRouteTTL  = 24 * time.Hour
)

type videoTaskRoute struct {
	UserID   uint64    `json:"userId"`
	APIKeyID uint64    `json:"apiKeyId"`
	Candidate Candidate `json:"candidate"`
}

// CreateVideo preserves the legacy OpenAI-compatible /v1/video/generations surface
// while adapting MiniMax channels to their native asynchronous H3 API.
func (s *sRelay) CreateVideo(ctx context.Context, incomingHeaders http.Header, body []byte, key apikey.AuthKey) (int, []byte, http.Header, error) {
	if len(body) > maxVideoRequestBody {
		return 0, nil, nil, gerror.New("video request body exceeds 64 MiB")
	}
	if !gjson.ValidBytes(body) {
		return 0, nil, nil, gerror.New("video request body must be valid JSON")
	}
	requestedModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestedModel == "" {
		return 0, nil, nil, gerror.New("model is required")
	}
	if !keyAllowsModel(key, requestedModel) {
		return 0, nil, nil, gerror.New("API key is not allowed to use model " + requestedModel)
	}
	candidates, err := s.route(ctx, requestedModel, key)
	if err != nil {
		return 0, nil, nil, err
	}
	if len(candidates) == 0 {
		return 0, nil, nil, gerror.Wrapf(ErrNoAvailableChannel, "no available channel for model %s", requestedModel)
	}
	var last videoUpstreamResult
	for _, candidate := range candidates {
		credential, credentialErr := s.channels.SelectCredential(ctx, key.Id, candidate.ChannelID, nil)
		if credentialErr != nil {
			last = videoUpstreamResult{err: credentialErr}
			continue
		}
		candidate.ChannelCredentialID = credential.ID
		candidate.APIKeyCipher = credential.APIKeyCipher
		result := s.createVideoUpstream(ctx, incomingHeaders, body, candidate)
		last = result
		if result.err != nil || result.status < http.StatusOK || result.status >= http.StatusMultipleChoices {
			continue
		}
		taskID := strings.TrimSpace(gjson.GetBytes(result.body, "task_id").String())
		if taskID == "" {
			return 0, nil, nil, gerror.New("video upstream response did not include task_id")
		}
		if err = s.storeVideoTaskRoute(ctx, taskID, key, candidate); err != nil {
			return 0, nil, nil, err
		}
		return result.status, result.body, result.headers, nil
	}
	if last.err != nil {
		return 0, nil, nil, last.err
	}
	if last.status > 0 {
		return last.status, last.body, last.headers, nil
	}
	return 0, nil, nil, gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible video channels failed")
}

// GetVideoTask queries the task using the same upstream credential and only permits
// the API key that created the task. Task-to-channel state is intentionally stored
// in Redis instead of a database migration because MiniMax task identifiers expire.
func (s *sRelay) GetVideoTask(ctx context.Context, incomingHeaders http.Header, taskID string, key apikey.AuthKey) (int, []byte, http.Header, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return 0, nil, nil, gerror.New("task_id is required")
	}
	route, err := s.loadVideoTaskRoute(ctx, taskID, key)
	if err != nil {
		return 0, nil, nil, err
	}
	return s.queryVideoUpstream(ctx, incomingHeaders, taskID, route.Candidate)
}

type videoUpstreamResult struct {
	status  int
	body    []byte
	headers http.Header
	err     error
}

func (s *sRelay) createVideoUpstream(ctx context.Context, incomingHeaders http.Header, body []byte, candidate Candidate) videoUpstreamResult {
	payload, err := prepareVideoRequestBody(body, candidate.ChannelType)
	if err != nil {
		return videoUpstreamResult{err: err}
	}
	return s.callVideoUpstream(ctx, http.MethodPost, videoCreateURL(candidate), incomingHeaders, payload, candidate)
}

func (s *sRelay) queryVideoUpstream(ctx context.Context, incomingHeaders http.Header, taskID string, candidate Candidate) (int, []byte, http.Header, error) {
	if candidate.ChannelType != "minimax" {
		return 0, nil, nil, gerror.New("video task polling is currently available only for MiniMax channels")
	}
	result := s.callVideoUpstream(ctx, http.MethodGet, miniMaxVideoURL(candidate.BaseURL, "/v2/query/video_generation/"+taskID), incomingHeaders, nil, candidate)
	return result.status, result.body, result.headers, result.err
}

func (s *sRelay) callVideoUpstream(ctx context.Context, method, target string, incomingHeaders http.Header, body []byte, candidate Candidate) videoUpstreamResult {
	apiKey, err := s.app.Secrets.Decrypt(candidate.APIKeyCipher)
	if err != nil {
		return videoUpstreamResult{err: err}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, target, bytes.NewReader(body))
	if err != nil {
		return videoUpstreamResult{err: gerror.Wrap(err, "create video upstream request")}
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if candidate.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", candidate.OrganizationID)
	}
	if candidate.ProjectID != "" {
		req.Header.Set("OpenAI-Project", candidate.ProjectID)
	}
	client, err := s.channels.HTTPClientForProxy(candidate.ProxyURLCipher)
	if err != nil {
		return videoUpstreamResult{err: err}
	}
	resp, err := client.Do(req)
	if err != nil {
		return videoUpstreamResult{err: gerror.Wrap(err, "call video upstream")}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxVideoRequestBody+1))
	if readErr != nil {
		return videoUpstreamResult{status: resp.StatusCode, err: gerror.Wrap(readErr, "read video upstream response")}
	}
	if len(responseBody) > maxVideoRequestBody {
		return videoUpstreamResult{status: resp.StatusCode, err: gerror.New("video upstream response exceeds 64 MiB")}
	}
	return videoUpstreamResult{status: resp.StatusCode, body: responseBody, headers: resp.Header.Clone()}
}

func prepareVideoRequestBody(original []byte, channelType string) ([]byte, error) {
	if channelType != "minimax" {
		return original, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(original, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode video request body")
	}
	if _, exists := payload["content"]; !exists {
		prompt, _ := payload["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			return nil, gerror.New("MiniMax video request requires content or prompt")
		}
		payload["content"] = []any{map[string]any{"type": "text", "text": prompt}}
	}
	delete(payload, "prompt")
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode MiniMax video request")
}

func videoCreateURL(candidate Candidate) string {
	if candidate.ChannelType == "minimax" {
		return miniMaxVideoURL(candidate.BaseURL, "/v2/video_generation")
	}
	return strings.TrimRight(candidate.BaseURL, "/") + "/video/generations"
}

func miniMaxVideoURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return baseURL + path
}

func (s *sRelay) storeVideoTaskRoute(ctx context.Context, taskID string, key apikey.AuthKey, candidate Candidate) error {
	encoded, err := json.Marshal(videoTaskRoute{UserID: key.UserId, APIKeyID: key.Id, Candidate: candidate})
	if err != nil {
		return gerror.Wrap(err, "encode video task route")
	}
	if err = s.app.Redis.Set(ctx, videoTaskRouteKey(taskID), encoded, videoTaskRouteTTL).Err(); err != nil {
		return gerror.Wrap(err, "store video task route")
	}
	return nil
}

func (s *sRelay) loadVideoTaskRoute(ctx context.Context, taskID string, key apikey.AuthKey) (videoTaskRoute, error) {
	encoded, err := s.app.Redis.Get(ctx, videoTaskRouteKey(taskID)).Bytes()
	if err != nil {
		return videoTaskRoute{}, gerror.New("video task not found or has expired")
	}
	var route videoTaskRoute
	if err = json.Unmarshal(encoded, &route); err != nil {
		return videoTaskRoute{}, gerror.Wrap(err, "decode video task route")
	}
	if route.UserID != key.UserId || route.APIKeyID != key.Id {
		return videoTaskRoute{}, gerror.New("video task is not available to this API key")
	}
	return route, nil
}

func videoTaskRouteKey(taskID string) string {
	return "aiferry:video-task:" + taskID
}
