package relay

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"testing"
)

func TestPrepareVideoRequestBodyMapsLegacyPromptWithoutChangingModel(t *testing.T) {
	body, err := prepareVideoRequestBody([]byte(`{"model":"minimax-h3","prompt":"A ferry crossing a quiet lake","duration":5,"resolution":"2K","ratio":"16:9"}`), "application/json", "minimax")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "minimax-h3" {
		t.Fatalf("model = %#v, want minimax-h3", payload["model"])
	}
	if _, exists := payload["prompt"]; exists {
		t.Fatalf("legacy prompt must be converted: %#v", payload)
	}
	content, ok := payload["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", payload["content"])
	}
	item := content[0].(map[string]any)
	if item["type"] != "text" || item["text"] != "A ferry crossing a quiet lake" {
		t.Fatalf("content item = %#v", item)
	}
}

func TestPrepareVideoRequestBodyPreservesNonMiniMaxPayload(t *testing.T) {
	original := []byte(`{"model":"other","prompt":"test","custom":true}`)
	body, err := prepareVideoRequestBody(original, "application/json", "openai")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(original) {
		t.Fatalf("payload = %s, want %s", body, original)
	}
}

func TestVideoRequestedModelSupportsJSONAndMultipart(t *testing.T) {
	model, err := videoRequestedModel([]byte(`{"model":"sora-2-pro","prompt":"test"}`), "application/json")
	if err != nil || model != "sora-2-pro" {
		t.Fatalf("JSON model = %q, err = %v", model, err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err = writer.WriteField("prompt", "test"); err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteField("model", "sora-2"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	model, err = videoRequestedModel(body.Bytes(), writer.FormDataContentType())
	if err != nil || model != "sora-2" {
		t.Fatalf("multipart model = %q, err = %v", model, err)
	}
}

func TestVideoCreateURLUsesChannelProtocol(t *testing.T) {
	miniMax := Candidate{ChannelType: "minimax", BaseURL: "https://api.minimax.io/v1"}
	if got := videoCreateURL(miniMax, legacyVideoAPI); got != "https://api.minimax.io/v2/video_generation" {
		t.Fatalf("MiniMax URL = %q", got)
	}
	openAI := Candidate{ChannelType: "openai", BaseURL: "https://gateway.example/v1"}
	if got := videoCreateURL(openAI, legacyVideoAPI); got != "https://gateway.example/v1/video/generations" {
		t.Fatalf("legacy URL = %q", got)
	}
	if got := videoCreateURL(openAI, openAIVideoAPI); got != "https://gateway.example/v1/videos" {
		t.Fatalf("OpenAI URL = %q", got)
	}
}

func TestVideoResponseIDAcceptsBothUpstreamShapes(t *testing.T) {
	if got := videoResponseID([]byte(`{"task_id":"task_1"}`), legacyVideoAPI); got != "task_1" {
		t.Fatalf("legacy id = %q", got)
	}
	if got := videoResponseID([]byte(`{"id":"video_1"}`), openAIVideoAPI); got != "video_1" {
		t.Fatalf("OpenAI id = %q", got)
	}
	if got := videoResponseID([]byte(`{"task_id":"task_2"}`), openAIVideoAPI); got != "task_2" {
		t.Fatalf("fallback id = %q", got)
	}
}

func TestVideoResourceURL(t *testing.T) {
	candidate := Candidate{BaseURL: "https://gateway.example/v1/"}
	if got := videoResourceURL(candidate, "video_123", false); got != "https://gateway.example/v1/videos/video_123" {
		t.Fatalf("retrieve URL = %q", got)
	}
	if got := videoResourceURL(candidate, "video_123", true); got != "https://gateway.example/v1/videos/video_123/content" {
		t.Fatalf("content URL = %q", got)
	}
}
