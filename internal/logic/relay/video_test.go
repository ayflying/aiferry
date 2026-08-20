package relay

import (
	"encoding/json"
	"testing"
)

func TestPrepareVideoRequestBodyMapsLegacyPromptWithoutChangingModel(t *testing.T) {
	body, err := prepareVideoRequestBody([]byte(`{"model":"minimax-h3","prompt":"A ferry crossing a quiet lake","duration":5,"resolution":"2K","ratio":"16:9"}`), "minimax")
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

func TestPrepareVideoRequestBodyPreservesMiniMaxMultimodalContent(t *testing.T) {
	body, err := prepareVideoRequestBody([]byte(`{"model":"minimax-h3","content":[{"type":"text","text":"Make the character dance"},{"type":"image_url","image_url":{"url":"https://example.com/input.png"},"role":"first_frame"}],"duration":5,"resolution":"2K"}`), "minimax")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	content := payload["content"].([]any)
	if len(content) != 2 || payload["model"] != "minimax-h3" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestPrepareVideoRequestBodyPreservesNonMiniMaxPayload(t *testing.T) {
	original := []byte(`{"model":"other","prompt":"test","custom":true}`)
	body, err := prepareVideoRequestBody(original, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(original) {
		t.Fatalf("payload = %s, want %s", body, original)
	}
}

func TestVideoCreateURLUsesChannelProtocol(t *testing.T) {
	if got := videoCreateURL(Candidate{ChannelType: "minimax", BaseURL: "https://api.minimax.io/v1"}); got != "https://api.minimax.io/v2/video_generation" {
		t.Fatalf("MiniMax URL = %q", got)
	}
	if got := videoCreateURL(Candidate{ChannelType: "openai", BaseURL: "https://gateway.example/v1"}); got != "https://gateway.example/v1/video/generations" {
		t.Fatalf("compatible URL = %q", got)
	}
}

func TestMiniMaxVideoURLRemovesOpenAICompatibleVersionSuffix(t *testing.T) {
	for input, want := range map[string]string{
		"https://api.minimax.io/v1": "https://api.minimax.io/v2/video_generation",
		"https://api.minimaxi.com":   "https://api.minimaxi.com/v2/video_generation",
	} {
		if got := miniMaxVideoURL(input, "/v2/video_generation"); got != want {
			t.Fatalf("miniMaxVideoURL(%q) = %q, want %q", input, got, want)
		}
	}
}
