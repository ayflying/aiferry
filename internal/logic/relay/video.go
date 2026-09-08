package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

const (
	maxVideoRequestBody = 64 << 20
	videoTaskRouteTTL   = 24 * time.Hour
)

type videoAPIMode string

const (
	legacyVideoAPI videoAPIMode = "legacy"
	openAIVideoAPI videoAPIMode = "openai"
)

type videoTaskRoute struct {
	UserID      uint64       `json:"userId"`
	APIKeyID    uint64       `json:"apiKeyId"`
	Mode        videoAPIMode `json:"mode"`
	Candidate   Candidate    `json:"candidate"`
	ResourceURL string       `json:"resourceUrl,omitempty"`
}

// videoAdapter 是视频渠道协议适配器：把「创建 URL、请求体改写、任务查询、
// 内容下载、响应 ID 提取」从渠道类型硬编码分支收敛为可声明的协议差异。
// openai（默认）走标准 /videos；minimax 与 volcengine_ark 在网关内翻译私有协议。
type videoAdapter struct {
	code string
}

// videoAdapterFor 按渠道类型解析视频适配器；解析失败时回退 openai 并记录告警，
// 与音频适配器（attemptAudioUpstream）行为保持一致。
func (s *sRelay) videoAdapterFor(ctx context.Context, candidate Candidate) videoAdapter {
	adapter, err := s.channels.VideoAdapterFor(ctx, candidate.ChannelType)
	if err != nil {
		g.Log().Warningf(ctx, "resolve video adapter for channel %s: %v", candidate.ChannelType, err)
		adapter = channeltype.VideoAdapterOpenAI
	}
	return videoAdapter{code: adapter}
}

// createURL 返回创建任务的上游端点。
func (a videoAdapter) createURL(candidate Candidate, mode videoAPIMode) string {
	switch a.code {
	case channeltype.VideoAdapterMiniMax:
		return miniMaxVideoURL(candidate.BaseURL, "/v2/video_generation")
	case channeltype.VideoAdapterVolcengineArk:
		return strings.TrimRight(candidate.BaseURL, "/") + "/contents/generations/tasks"
	}
	if mode == openAIVideoAPI {
		return strings.TrimRight(candidate.BaseURL, "/") + "/videos"
	}
	return strings.TrimRight(candidate.BaseURL, "/") + "/video/generations"
}

// retrieveURL 返回查询任务状态的上游端点（OpenAI 形态的任务查询）。
func (a videoAdapter) retrieveURL(candidate Candidate, taskID string, mode videoAPIMode) string {
	switch a.code {
	case channeltype.VideoAdapterMiniMax:
		return miniMaxVideoURL(candidate.BaseURL, "/v2/query/video_generation/"+taskID)
	case channeltype.VideoAdapterVolcengineArk:
		return strings.TrimRight(candidate.BaseURL, "/") + "/contents/generations/tasks/" + taskID
	}
	if mode == openAIVideoAPI {
		return strings.TrimRight(candidate.BaseURL, "/") + "/videos/" + taskID
	}
	return strings.TrimRight(candidate.BaseURL, "/") + "/video/generations/" + taskID
}

// legacyRetrieveURL 返回 legacy /video/generations 形态的查询端点。
func (a videoAdapter) legacyRetrieveURL(candidate Candidate, taskID string) string {
	return a.retrieveURL(candidate, taskID, legacyVideoAPI)
}

// legacyContentURL 返回 legacy 形态的内容下载端点。
func (a videoAdapter) legacyContentURL(candidate Candidate, taskID string) string {
	switch a.code {
	case channeltype.VideoAdapterMiniMax, channeltype.VideoAdapterVolcengineArk:
		// MiniMax / 方舟没有独立内容端点，内容通过查询响应中的 file_url 下载。
		return ""
	}
	return strings.TrimRight(candidate.BaseURL, "/") + "/video/generations/" + taskID + "/content"
}

// prepareBody 在转发前按上游协议改写请求体；openai 原样透传。
func (a videoAdapter) prepareBody(original []byte, contentType string) ([]byte, error) {
	switch a.code {
	case channeltype.VideoAdapterMiniMax:
		return prepareMiniMaxVideoRequestBody(original, contentType)
	default:
		return original, nil
	}
}

func (s *sRelay) CreateVideo(ctx context.Context, incomingHeaders http.Header, body []byte, key apikey.AuthKey) (int, []byte, http.Header, error) {
	return s.createVideo(ctx, incomingHeaders, body, key, legacyVideoAPI)
}

func (s *sRelay) CreateVideos(ctx context.Context, incomingHeaders http.Header, body []byte, key apikey.AuthKey) (int, []byte, http.Header, error) {
	return s.createVideo(ctx, incomingHeaders, body, key, openAIVideoAPI)
}

func (s *sRelay) createVideo(ctx context.Context, incomingHeaders http.Header, body []byte, key apikey.AuthKey, mode videoAPIMode) (int, []byte, http.Header, error) {
	if len(body) > maxVideoRequestBody {
		return 0, nil, nil, gerror.New("video request body exceeds 64 MiB")
	}
	requestedModel, err := videoRequestedModel(body, incomingHeaders.Get("Content-Type"))
	if err != nil {
		return 0, nil, nil, err
	}
	if requestedModel == "" {
		return 0, nil, nil, gerror.New("model is required")
	}
	if !keyAllowsModel(key, requestedModel) {
		return 0, nil, nil, gerror.New("API key is not allowed to use model " + requestedModel)
	}
	candidates, err := s.routeCached(ctx, requestedModel, key)
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
		result := s.createVideoUpstream(ctx, incomingHeaders, body, mode, candidate)
		last = result
		if result.err != nil || result.status < http.StatusOK || result.status >= http.StatusMultipleChoices {
			continue
		}
		videoID := videoResponseID(result.body, mode)
		if videoID == "" {
			return 0, nil, nil, gerror.New("video upstream response did not include a task or video identifier")
		}
		if err = s.storeVideoTaskRoute(ctx, videoID, key, mode, candidate, ""); err != nil {
			return 0, nil, nil, err
		}
		return result.status, result.body, result.headers, nil
	}
	if last.err != nil {
		return 0, nil, nil, last.err
	}
	if last.status > 0 {
		return normalizedVideoUpstreamResponse(last.status, last.body, last.headers, nil)
	}
	return 0, nil, nil, gerror.Wrap(ErrEligibleChannelsExhausted, "all eligible video channels failed")
}

func (s *sRelay) GetVideoTask(ctx context.Context, incomingHeaders http.Header, taskID string, key apikey.AuthKey) (int, []byte, http.Header, error) {
	route, err := s.loadVideoTaskRoute(ctx, taskID, key)
	if err != nil {
		return 0, nil, nil, err
	}
	adapter := s.videoAdapterFor(ctx, route.Candidate)
	if route.Mode == openAIVideoAPI {
		return s.getOpenAIVideo(ctx, incomingHeaders, taskID, route, adapter)
	}
	if adapter.code != channeltype.VideoAdapterOpenAI {
		status, body, headers, err, resourceURL := s.queryTaskOnUpstream(ctx, incomingHeaders, taskID, route.Candidate, adapter, videoContentPath(taskID, route.Mode))
		if err == nil && status >= http.StatusOK && status < http.StatusMultipleChoices && resourceURL != "" {
			route.ResourceURL = resourceURL
			if storeErr := s.storeVideoTaskRoute(ctx, taskID, key, route.Mode, route.Candidate, resourceURL); storeErr != nil {
				return 0, nil, nil, storeErr
			}
		}
		return status, body, headers, err
	}
	result := s.callVideoUpstream(ctx, http.MethodGet, adapter.retrieveURL(route.Candidate, taskID, route.Mode), incomingHeaders, nil, route.Candidate)
	return normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
}

func (s *sRelay) GetVideoTaskContent(ctx context.Context, incomingHeaders http.Header, taskID string, key apikey.AuthKey) (int, []byte, http.Header, error) {
	route, err := s.loadVideoTaskRoute(ctx, taskID, key)
	if err != nil {
		return 0, nil, nil, err
	}
	adapter := s.videoAdapterFor(ctx, route.Candidate)
	if route.Mode == openAIVideoAPI {
		return s.getOpenAIVideoContent(ctx, incomingHeaders, taskID, route, adapter)
	}
	if adapter.code != channeltype.VideoAdapterOpenAI {
		return s.downloadAdapterVideo(ctx, incomingHeaders, taskID, route.Candidate, adapter, route.ResourceURL)
	}
	result := s.callVideoUpstream(ctx, http.MethodGet, adapter.legacyContentURL(route.Candidate, taskID), incomingHeaders, nil, route.Candidate)
	return normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
}

func (s *sRelay) GetOpenAIVideo(ctx context.Context, incomingHeaders http.Header, videoID string, key apikey.AuthKey) (int, []byte, http.Header, error) {
	route, err := s.loadVideoTaskRoute(ctx, videoID, key)
	if err != nil {
		return 0, nil, nil, err
	}
	return s.getOpenAIVideo(ctx, incomingHeaders, videoID, route, s.videoAdapterFor(ctx, route.Candidate))
}

func (s *sRelay) GetOpenAIVideoContent(ctx context.Context, incomingHeaders http.Header, videoID string, key apikey.AuthKey) (int, []byte, http.Header, error) {
	route, err := s.loadVideoTaskRoute(ctx, videoID, key)
	if err != nil {
		return 0, nil, nil, err
	}
	return s.getOpenAIVideoContent(ctx, incomingHeaders, videoID, route, s.videoAdapterFor(ctx, route.Candidate))
}

func (s *sRelay) getOpenAIVideoContent(ctx context.Context, incomingHeaders http.Header, videoID string, route videoTaskRoute, adapter videoAdapter) (int, []byte, http.Header, error) {
	if adapter.code != channeltype.VideoAdapterOpenAI {
		return s.downloadAdapterVideo(ctx, incomingHeaders, videoID, route.Candidate, adapter, route.ResourceURL)
	}
	result := s.callVideoUpstream(ctx, http.MethodGet, videoResourceURL(route.Candidate, videoID, true), incomingHeaders, nil, route.Candidate)
	return normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
}

func (s *sRelay) getOpenAIVideo(ctx context.Context, incomingHeaders http.Header, videoID string, route videoTaskRoute, adapter videoAdapter) (int, []byte, http.Header, error) {
	if adapter.code != channeltype.VideoAdapterOpenAI {
		status, body, headers, err, _ := s.queryTaskOnUpstream(ctx, incomingHeaders, videoID, route.Candidate, adapter, videoContentPath(videoID, openAIVideoAPI))
		return status, body, headers, err
	}
	result := s.callVideoUpstream(ctx, http.MethodGet, videoResourceURL(route.Candidate, videoID, false), incomingHeaders, nil, route.Candidate)
	return normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
}

// queryTaskOnUpstream 用适配器协议查询任务状态，并把响应中的资源地址改写为
// 网关内容端点（openai 形态的适配器不会走到这里）。
func (s *sRelay) queryTaskOnUpstream(ctx context.Context, incomingHeaders http.Header, taskID string, candidate Candidate, adapter videoAdapter, contentPath string) (int, []byte, http.Header, error, string) {
	switch adapter.code {
	case channeltype.VideoAdapterMiniMax:
		return s.queryMiniMaxVideo(ctx, incomingHeaders, taskID, candidate, contentPath)
	case channeltype.VideoAdapterVolcengineArk:
		result := s.callVideoUpstream(ctx, http.MethodGet, adapter.retrieveURL(candidate, taskID, legacyVideoAPI), incomingHeaders, nil, candidate)
		status, body, headers, err := normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
		if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
			return status, body, headers, err, ""
		}
		resourceURL := arkVideoResponseURL(body)
		if resourceURL == "" {
			return status, body, headers, nil, ""
		}
		return status, rewriteArkVideoResponseURL(body, contentPath), headers, nil, resourceURL
	}
	return 0, nil, nil, gerror.Newf("adapter %s does not support task queries", adapter.code), ""
}

// downloadAdapterVideo 下载适配器协议的视频文件：优先使用路由里缓存的资源地址，
// 否则先查一次任务状态再解析。跨域资源不走渠道代理。
func (s *sRelay) downloadAdapterVideo(ctx context.Context, incomingHeaders http.Header, taskID string, candidate Candidate, adapter videoAdapter, resourceURL string) (int, []byte, http.Header, error) {
	if strings.TrimSpace(resourceURL) == "" {
		status, body, headers, err, resolved := s.queryTaskOnUpstream(ctx, incomingHeaders, taskID, candidate, adapter, videoContentPath(taskID, openAIVideoAPI))
		if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
			return status, body, headers, err
		}
		resourceURL = resolved
	}
	if resourceURL == "" {
		return 0, nil, nil, gerror.New("video is not completed or does not include a downloadable file")
	}
	fileCandidate := candidate
	if !sameUpstreamOrigin(candidate.BaseURL, resourceURL) {
		fileCandidate.APIKeyCipher = ""
		fileCandidate.OrganizationID = ""
		fileCandidate.ProjectID = ""
		fileCandidate.ProxyURLCipher = ""
		fileCandidate.DirectHTTP = true
	}
	file := s.callVideoUpstream(ctx, http.MethodGet, resourceURL, incomingHeaders, nil, fileCandidate)
	return normalizedVideoUpstreamResponse(file.status, file.body, file.headers, file.err)
}

func normalizedVideoUpstreamResponse(status int, body []byte, headers http.Header, err error) (int, []byte, http.Header, error) {
	if err != nil || status == 0 || status < http.StatusMultipleChoices || len(body) > 0 {
		return status, body, headers, err
	}
	if headers == nil {
		headers = make(http.Header)
	} else {
		headers = headers.Clone()
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-AiFerry-Upstream-Status", strconv.Itoa(status))
	message := "Upstream video provider returned HTTP " + strconv.Itoa(status) + " without an error response body"
	return status, openAIError("upstream_error", message), headers, nil
}

type videoUpstreamResult struct {
	status  int
	body    []byte
	headers http.Header
	err     error
}

func (s *sRelay) createVideoUpstream(ctx context.Context, incomingHeaders http.Header, body []byte, mode videoAPIMode, candidate Candidate) videoUpstreamResult {
	adapter := s.videoAdapterFor(ctx, candidate)
	payload, err := adapter.prepareBody(body, incomingHeaders.Get("Content-Type"))
	if err != nil {
		return videoUpstreamResult{err: err}
	}
	return s.callVideoUpstream(ctx, http.MethodPost, adapter.createURL(candidate, mode), incomingHeaders, payload, candidate)
}

func (s *sRelay) queryMiniMaxVideo(ctx context.Context, incomingHeaders http.Header, taskID string, candidate Candidate, contentPath string) (int, []byte, http.Header, error, string) {
	result := s.callVideoUpstream(ctx, http.MethodGet, miniMaxVideoURL(candidate.BaseURL, "/v2/query/video_generation/"+taskID), incomingHeaders, nil, candidate)
	status, body, headers, err := normalizedVideoUpstreamResponse(result.status, result.body, result.headers, result.err)
	if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
		return status, body, headers, err, ""
	}
	resourceURL, err := resolveMiniMaxVideoResourceURL(candidate.BaseURL, miniMaxVideoResponseURL(body))
	if err != nil {
		return 0, nil, nil, err, ""
	}
	return status, rewriteMiniMaxVideoResponseURL(body, contentPath), headers, nil, resourceURL
}

func miniMaxVideoResponseURL(body []byte) string {
	for _, path := range []string{"url", "task.content.url", "data.url", "data.task.content.url"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func rewriteMiniMaxVideoResponseURL(body []byte, contentPath string) []byte {
	if !gjson.ValidBytes(body) || miniMaxVideoResponseURL(body) == "" {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["url"] = contentPath
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func videoContentPath(videoID string, mode videoAPIMode) string {
	if mode == openAIVideoAPI {
		return "/v1/videos/" + videoID + "/content"
	}
	return "/v1/video/generations/" + videoID + "/content"
}

func resolveMiniMaxVideoResourceURL(baseURL, resourceURL string) (string, error) {
	resourceURL = strings.TrimSpace(resourceURL)
	if resourceURL == "" {
		return "", nil
	}
	resource, err := url.Parse(resourceURL)
	if err != nil {
		return "", gerror.Wrap(err, "parse MiniMax video resource URL")
	}
	if resource.IsAbs() {
		if resource.Scheme != "https" && resource.Scheme != "http" {
			return "", gerror.New("MiniMax video resource URL must use HTTP or HTTPS")
		}
		return resource.String(), nil
	}
	base, err := url.Parse(miniMaxVideoURL(baseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", gerror.New("MiniMax channel base URL is invalid")
	}
	return base.ResolveReference(resource).String(), nil
}

func sameUpstreamOrigin(baseURL, targetURL string) bool {
	base, baseErr := url.Parse(baseURL)
	target, targetErr := url.Parse(targetURL)
	return baseErr == nil && targetErr == nil && strings.EqualFold(base.Scheme, target.Scheme) && strings.EqualFold(base.Host, target.Host)
}

func (s *sRelay) callVideoUpstream(ctx context.Context, method, target string, incomingHeaders http.Header, body []byte, candidate Candidate) videoUpstreamResult {
	apiKey := ""
	var err error
	if candidate.APIKeyCipher != "" {
		apiKey, err = s.app.Secrets.Decrypt(candidate.APIKeyCipher)
		if err != nil {
			return videoUpstreamResult{err: err}
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, target, bytes.NewReader(body))
	if err != nil {
		return videoUpstreamResult{err: gerror.Wrap(err, "create video upstream request")}
	}
	copyRequestHeaders(req.Header, incomingHeaders)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if method == http.MethodPost && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
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
	responseHeaders := resp.Header.Clone()
	if resp.StatusCode >= http.StatusMultipleChoices {
		g.Log().Warningf(ctx, "video upstream failed channel_id=%d credential_id=%d status=%d response_bytes=%d upstream_trace_id=%q", candidate.ChannelID, candidate.ChannelCredentialID, resp.StatusCode, len(responseBody), responseHeaders.Get("Trace-Id"))
	}
	return videoUpstreamResult{status: resp.StatusCode, body: responseBody, headers: responseHeaders}
}

// prepareMiniMaxVideoRequestBody 把 OpenAI 形态请求体翻译为 MiniMax 私有协议：
// prompt 字段转成 content 数组，模型等其余字段原样保留。
func prepareMiniMaxVideoRequestBody(original []byte, contentType string) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType == "multipart/form-data" {
		return nil, gerror.New("MiniMax video channels require application/json requests")
	}
	if !gjson.ValidBytes(original) {
		return nil, gerror.New("video request body must be valid JSON")
	}
	var payload map[string]any
	if err = json.Unmarshal(original, &payload); err != nil {
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

// arkVideoResponseURL 从方舟任务查询响应中解析视频文件地址（content.video_url）。
func arkVideoResponseURL(body []byte) string {
	for _, path := range []string{"content.video_url", "video_url", "result.video_url"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

// rewriteArkVideoResponseURL 把响应中的 video_url 替换为网关内容端点，
// 避免 SPA 客户端直接暴露上游 CDN 地址。
func rewriteArkVideoResponseURL(body []byte, contentPath string) []byte {
	if !gjson.ValidBytes(body) || arkVideoResponseURL(body) == "" {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if content := gjson.GetBytes(body, "content"); content.IsObject() {
		if inner, ok := payload["content"].(map[string]any); ok {
			inner["video_url"] = contentPath
		}
	} else {
		payload["video_url"] = contentPath
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func videoResourceURL(candidate Candidate, videoID string, content bool) string {
	path := "/videos/" + videoID
	if content {
		path += "/content"
	}
	return strings.TrimRight(candidate.BaseURL, "/") + path
}

func miniMaxVideoURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return baseURL + path
}

func videoRequestedModel(body []byte, contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" || mediaType == "application/json" {
		if !gjson.ValidBytes(body) {
			return "", gerror.New("video request body must be valid JSON")
		}
		return strings.TrimSpace(gjson.GetBytes(body, "model").String()), nil
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		return "", gerror.New("video request content type must be application/json or multipart/form-data")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return "", gerror.Wrap(partErr, "read video multipart request")
		}
		if part.FormName() != "model" {
			continue
		}
		model, readErr := io.ReadAll(io.LimitReader(part, 1024))
		if readErr != nil {
			return "", gerror.Wrap(readErr, "read video model field")
		}
		return strings.TrimSpace(string(model)), nil
	}
	return "", nil
}

func videoResponseID(body []byte, mode videoAPIMode) string {
	primary := "task_id"
	fallback := "id"
	if mode == openAIVideoAPI {
		primary, fallback = fallback, primary
	}
	id := strings.TrimSpace(gjson.GetBytes(body, primary).String())
	if id == "" {
		id = strings.TrimSpace(gjson.GetBytes(body, fallback).String())
	}
	return id
}

func (s *sRelay) storeVideoTaskRoute(ctx context.Context, taskID string, key apikey.AuthKey, mode videoAPIMode, candidate Candidate, resourceURL string) error {
	encoded, err := json.Marshal(videoTaskRoute{UserID: key.UserId, APIKeyID: key.Id, Mode: mode, Candidate: candidate, ResourceURL: resourceURL})
	if err != nil {
		return gerror.Wrap(err, "encode video task route")
	}
	if err = s.app.Redis.Set(ctx, videoTaskRouteKey(taskID), encoded, videoTaskRouteTTL).Err(); err != nil {
		return gerror.Wrap(err, "store video task route")
	}
	return nil
}

func (s *sRelay) loadVideoTaskRoute(ctx context.Context, taskID string, key apikey.AuthKey) (videoTaskRoute, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return videoTaskRoute{}, gerror.New("video identifier is required")
	}
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
