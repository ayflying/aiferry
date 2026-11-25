package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	GeminiSubscriptionBaseURL    = "https://cloudcode-pa.googleapis.com"
	GeminiSubscriptionStreamPath = "/v1internal:streamGenerateContent?alt=sse"
	geminiSubscriptionModelsURL  = GeminiSubscriptionBaseURL + "/v1internal:fetchAvailableModels"
)

func (s *sChannel) discoverGeminiSubscriptionModels(ctx context.Context, ch entity.Channels, credential RouteCredential) ([]string, map[string]modelmetadata.Metadata, error) {
	project := strings.TrimSpace(ch.ProjectId)
	if project == "" {
		return nil, nil, gerror.New("Gemini subscription model discovery requires a project ID")
	}
	client, token, err := s.geminiSubscriptionClientToken(ctx, ch, credential)
	if err != nil {
		return nil, nil, err
	}
	return s.fetchGeminiSubscriptionModels(ctx, client, project, token)
}

func (s *sChannel) geminiSubscriptionClientToken(ctx context.Context, ch entity.Channels, credential RouteCredential) (*http.Client, string, error) {
	client, err := s.GeminiSubscriptionHTTPClient(ctx, ch, credential.ID)
	if err != nil {
		return nil, "", err
	}
	token, err := s.GeminiSubscriptionAccessToken(ctx, ch.Id, credential.ID, credential.APIKeyCipher)
	if err != nil {
		return nil, "", err
	}
	return client, token, nil
}

func (s *sChannel) fetchGeminiSubscriptionModels(ctx context.Context, client *http.Client, project, token string) ([]string, map[string]modelmetadata.Metadata, error) {
	projectJSON, err := json.Marshal(struct {
		Project string `json:"project"`
	}{Project: project})
	if err != nil {
		return nil, nil, gerror.Wrap(err, "encode Gemini subscription model discovery request")
	}
	req, err := newGeminiSubscriptionRequest(ctx, geminiSubscriptionModelsURL, "application/json", token, projectJSON)
	if err != nil {
		return nil, nil, gerror.Wrap(err, "create Gemini subscription model discovery request")
	}
	body, err := s.fetchJSON(client, req, upstreamJSONRequest{
		BodyLimit:    4 << 20,
		FetchError:   "fetch Gemini subscription models",
		ReadError:    "read Gemini subscription models",
		InvalidError: "Gemini subscription model query returned invalid JSON",
		StatusError:  upstreamModelQueryError,
	})
	if err != nil {
		return nil, nil, err
	}
	return parseGeminiSubscriptionModels(body)
}

func newGeminiSubscriptionRequest(ctx context.Context, endpoint, accept, token string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	return req, nil
}

// The catalog keys are the exact runtime IDs accepted by streamGenerateContent.
// Display labels, quota, and recommended/default hints do not change routing.
func parseGeminiSubscriptionModels(body []byte) ([]string, map[string]modelmetadata.Metadata, error) {
	models := gjson.GetBytes(body, "models")
	if !models.IsObject() {
		return nil, nil, gerror.New("Gemini subscription model query did not return a models object")
	}
	metadata := make(map[string]modelmetadata.Metadata)
	var names []string
	models.ForEach(func(key, entry gjson.Result) bool {
		id := key.String()
		if !entry.IsObject() || entry.Get("isInternal").Type == gjson.True || entry.Get("disabled").Type == gjson.True || !geminiSubscriptionTextModel(id) {
			return true
		}
		names = append(names, id)
		metadata[id] = geminiSubscriptionModelMetadata(entry)
		return true
	})
	return normalizeModelNames(names), metadata, nil
}

var geminiSubscriptionModelID = regexp.MustCompile(`(?i)^(gemini|claude|gpt-oss)-[a-z0-9][a-z0-9._-]{0,150}$`)

func geminiSubscriptionTextModel(id string) bool {
	if !geminiSubscriptionModelID.MatchString(id) {
		return false
	}
	id = strings.ToLower(id)
	switch id {
	case "gemini-2.5-pro", "gemini-2.5-flash-thinking": // Retired runtime IDs still occasionally listed upstream.
		return false
	}
	for _, segment := range strings.Split(id, "-") {
		switch segment {
		case "image", "embedding", "embeddings", "tts", "audio", "video", "live", "nativeaudio":
			return false
		}
	}
	return true
}

func geminiSubscriptionModelMetadata(entry gjson.Result) modelmetadata.Metadata {
	// Reuse normal metadata validation/type checking. Do not report upstream
	// image input or tool support: this channel only adapts chat text.
	fields := map[string]json.RawMessage{
		"input_modalities":  json.RawMessage(`["text"]`),
		"output_modalities": json.RawMessage(`["text"]`),
		"tools":             json.RawMessage(`false`),
		"structured_output": json.RawMessage(`false`),
	}
	for name, upstream := range map[string]string{
		"display_name": "displayName", "description": "description", "context_length": "maxTokens",
		"max_output_tokens": "maxOutputTokens", "reasoning": "supportsThinking",
	} {
		if value := entry.Get(upstream); value.Exists() {
			fields[name] = json.RawMessage(value.Raw)
		}
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return modelmetadata.Metadata{}
	}
	return modelmetadata.Parse(gjson.ParseBytes(body))
}
