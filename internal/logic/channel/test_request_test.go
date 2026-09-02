package channel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestTestEndpointsUsesModelCapabilitiesForAutoMode(t *testing.T) {
	endpoints := testEndpoints("auto", "gpt-4.1-mini")
	if len(endpoints) != 3 || endpoints[0] != "chat" || endpoints[1] != "responses" || endpoints[2] != "embeddings" {
		t.Fatalf("unexpected auto endpoints: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "gpt-5.6-luna"); len(endpoints) != 2 || endpoints[0] != "responses" || endpoints[1] != "chat" {
		t.Fatalf("new GPT models should test Responses first: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "gpt-image-2"); len(endpoints) != 1 || endpoints[0] != "images" {
		t.Fatalf("image models should use the image endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "text-embedding-3-large"); len(endpoints) != 1 || endpoints[0] != "embeddings" {
		t.Fatalf("embedding models should use the embeddings endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "mimo-v2.5-tts"); len(endpoints) != 1 || endpoints[0] != "tts" {
		t.Fatalf("TTS models should use the tts endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "mimo-v2.5-tts-voicedesign"); len(endpoints) != 1 || endpoints[0] != "tts" {
		t.Fatalf("TTS voicedesign variants should use the tts endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "mimo-v2.5-asr"); len(endpoints) != 1 || endpoints[0] != "asr" {
		t.Fatalf("ASR models should use the asr endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("auto", "whisper-large-v3"); len(endpoints) != 1 || endpoints[0] != "asr" {
		t.Fatalf("whisper models should use the asr endpoint: %#v", endpoints)
	}
	if endpoints = testEndpoints("responses", "gpt-5.6-luna"); len(endpoints) != 1 || endpoints[0] != "responses" {
		t.Fatalf("explicit endpoint should not expand: %#v", endpoints)
	}
}

func TestTestPayloadAddsStreamUsageForChat(t *testing.T) {
	path, payload, streamed := testPayload("chat", "gpt-test", true)
	if path != "/chat/completions" || !streamed {
		t.Fatalf("unexpected chat payload metadata: path=%q streamed=%t", path, streamed)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["stream"] != true {
		t.Fatalf("chat stream flag was not enabled: %#v", value)
	}
	options, ok := value["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("chat stream usage was not requested: %#v", value)
	}
}

func TestTestPayloadKeepsEmbeddingsNonStreaming(t *testing.T) {
	path, _, streamed := testPayload("embeddings", "text-embedding-3-small", true)
	if path != "/embeddings" || streamed {
		t.Fatalf("embeddings should remain non-streaming: path=%q streamed=%t", path, streamed)
	}
}

func TestTestPayloadUsesImageGenerationEndpoint(t *testing.T) {
	path, payload, streamed := testPayload("images", "gpt-image-2", true)
	if path != "/images/generations" || streamed {
		t.Fatalf("image testing should use the non-streaming image endpoint: path=%q streamed=%t", path, streamed)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["model"] != "gpt-image-2" || value["prompt"] == "" || value["size"] != "1024x1024" {
		t.Fatalf("unexpected image test payload: %#v", value)
	}
}

func TestTestPayloadBuildsTTSAndASRRequests(t *testing.T) {
	path, payload, streamed := testPayload("tts", "mimo-v2.5-tts", true)
	if path != "/audio/speech" || streamed {
		t.Fatalf("tts testing should use the non-streaming speech endpoint: path=%q streamed=%t", path, streamed)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["model"] != "mimo-v2.5-tts" || value["input"] == "" || value["voice"] == "" {
		t.Fatalf("unexpected tts test payload: %#v", value)
	}

	path, asrPayload, streamed := testPayload("asr", "mimo-v2.5-asr", true)
	if path != "/audio/transcriptions" || streamed {
		t.Fatalf("asr testing should use the non-streaming transcriptions endpoint: path=%q streamed=%t", path, streamed)
	}
	request, ok := asrPayload.(asrMultipartRequest)
	if !ok {
		t.Fatalf("asr payload should be multipart: %#v", asrPayload)
	}
	if request.Model != "mimo-v2.5-asr" || len(request.Content) < 44 {
		t.Fatalf("unexpected asr multipart payload: model=%q contentBytes=%d", request.Model, len(request.Content))
	}
	// 最小 WAV 必须是合法 RIFF/WAVE 头，上游才能解析。
	if string(request.Content[0:4]) != "RIFF" || string(request.Content[8:12]) != "WAVE" {
		t.Fatalf("asr sample is not a valid WAV: %x", request.Content[:12])
	}
}

func TestChatAdapterPayloadConvertsTTSAndASR(t *testing.T) {
	// TTS：标准 speech payload 转为 chat completions 承载。
	path, chatPayload := chatAdapterPayload("tts", "mimo-v2.5-tts", map[string]any{"input": "你好", "voice": "Chloe"})
	if path != "/chat/completions" {
		t.Fatalf("unexpected chat adapter path: %q", path)
	}
	body, err := json.Marshal(chatPayload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["model"] != "mimo-v2.5-tts" {
		t.Fatalf("chat adapter lost model: %#v", value)
	}
	messages, ok := value["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("unexpected chat adapter messages: %#v", value["messages"])
	}
	assistant, _ := messages[1].(map[string]any)
	if assistant["content"] != "你好" {
		t.Fatalf("chat adapter lost speech text: %#v", assistant)
	}
	audio, _ := value["audio"].(map[string]any)
	if audio == nil || audio["voice"] != "Chloe" || audio["format"] != "wav" {
		t.Fatalf("unexpected chat adapter audio params: %#v", value["audio"])
	}

	// ASR：multipart payload 的 WAV 内容被 base64 后经 input_audio 传入。
	_, _, _ = testPayload("asr", "mimo-v2.5-asr", false)
	asrPath, asrChat := chatAdapterPayload("asr", "mimo-v2.5-asr", asrTestPayload("mimo-v2.5-asr"))
	if asrPath != "/chat/completions" {
		t.Fatalf("unexpected chat adapter asr path: %q", asrPath)
	}
	asrBody, err := json.Marshal(asrChat)
	if err != nil {
		t.Fatal(err)
	}
	var asrValue struct {
		Messages []struct {
			Content []struct {
				Type       string `json:"type"`
				InputAudio struct {
					Data string `json:"data"`
				} `json:"input_audio"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(asrBody, &asrValue); err != nil {
		t.Fatal(err)
	}
	if len(asrValue.Messages) != 1 || len(asrValue.Messages[0].Content) != 1 || asrValue.Messages[0].Content[0].Type != "input_audio" {
		t.Fatalf("unexpected chat adapter asr messages: %s", asrBody)
	}
	data := asrValue.Messages[0].Content[0].InputAudio.Data
	if !json.Valid([]byte(asrBody)) || !bytes.HasPrefix([]byte(data), []byte("data:audio/wav;base64,")) {
		t.Fatalf("unexpected chat adapter asr data url prefix")
	}
}

func TestParseTestUsageReadsSSEUsage(t *testing.T) {
	tokens := parseTestUsage([]byte("data: {\"usage\":{\"input_tokens\":8,\"output_tokens\":3,\"total_tokens\":11}}\n\ndata: [DONE]\n"), true)
	if tokens.Input == nil || *tokens.Input != 8 || tokens.Output == nil || *tokens.Output != 3 || tokens.Total == nil || *tokens.Total != 11 {
		t.Fatalf("unexpected stream usage: %+v", tokens)
	}
	if !canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusNotFound}) ||
		!canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusBadRequest, Message: "This model does not support chat completions"}) ||
		canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusBadRequest, Message: "The requested model does not exist"}) ||
		canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusTooManyRequests}) {
		t.Fatal("endpoint fallback statuses are incorrect")
	}
}
