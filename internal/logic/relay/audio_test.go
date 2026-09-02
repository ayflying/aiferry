package relay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

func TestAudioSpeechHandlerReplacesModel(t *testing.T) {
	body := []byte(`{"model":"tts-public","input":"你好","voice":"alloy"}`)
	model, err := (audioSpeechHandler{}).RequestedModel(body)
	if err != nil || model != "tts-public" {
		t.Fatalf("unexpected speech model: %q err=%v", model, err)
	}
	upstream, contentType, _, err := (audioSpeechHandler{}).UpstreamBody(body, "tts-upstream", http.Header{}, channeltype.AudioAdapterOpenAI)
	if err != nil || contentType != "application/json" {
		t.Fatalf("unexpected speech upstream body: err=%v contentType=%q", err, contentType)
	}
	if !bytes.Contains(upstream, []byte(`"model":"tts-upstream"`)) || !bytes.Contains(upstream, []byte(`"input":"你好"`)) {
		t.Fatalf("speech upstream body lost fields: %s", upstream)
	}
}

func TestAudioSpeechHandlerChatAdapter(t *testing.T) {
	body := []byte(`{"model":"tts-public","input":"你好世界","voice":"Chloe"}`)
	upstream, contentType, path, err := (audioSpeechHandler{}).UpstreamBody(body, "mimo-v2.5-tts", http.Header{}, channeltype.AudioAdapterChat)
	if err != nil || contentType != "application/json" || path != "/chat/completions" {
		t.Fatalf("unexpected chat-adapted body: err=%v contentType=%q path=%q", err, contentType, path)
	}
	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Audio struct {
			Format string `json:"format"`
			Voice  string `json:"voice"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(upstream, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "mimo-v2.5-tts" {
		t.Fatalf("upstream model not replaced: %q", payload.Model)
	}
	if len(payload.Messages) != 2 || payload.Messages[0].Role != "user" || payload.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected chat messages: %#v", payload.Messages)
	}
	if payload.Messages[1].Content != "你好世界" {
		t.Fatalf("assistant text lost: %q", payload.Messages[1].Content)
	}
	if payload.Audio.Voice != "Chloe" || payload.Audio.Format != "wav" {
		t.Fatalf("unexpected audio params: %#v", payload.Audio)
	}
	// 默认值：input/voice 缺省时使用兜底。
	upstream, _, _, err = (audioSpeechHandler{}).UpstreamBody([]byte(`{"model":"x"}`), "m", http.Header{}, channeltype.AudioAdapterChat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upstream), "mimo_default") {
		t.Fatalf("default voice not applied: %s", upstream)
	}
}

func TestChatAudioToBinary(t *testing.T) {
	raw := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x01}
	encoded := base64.StdEncoding.EncodeToString(raw)
	body := []byte(`{"choices":[{"message":{"audio":{"data":"data:audio/wav;base64,` + encoded + `"}}}]}`)
	decoded, err := chatAudioToBinary(body)
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("chat audio decode failed: err=%v bytes=%d", err, len(decoded))
	}
	if _, err = chatAudioToBinary([]byte(`{"choices":[{"message":{}}]}`)); err == nil {
		t.Fatal("expected error when audio data missing")
	}
}

func TestAudioTranscriptionHandlerRebuildsMultipart(t *testing.T) {
	wav := minimalTestWAV()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	_ = writer.WriteField("model", "asr-public")
	part, _ := writer.CreateFormFile("file", "audio.wav")
	_, _ = part.Write(wav)
	_ = writer.Close()

	handler := audioTranscriptionHandler{contentType: writer.FormDataContentType()}
	model, err := handler.RequestedModel(buffer.Bytes())
	if err != nil || model != "asr-public" {
		t.Fatalf("unexpected transcription model: %q err=%v", model, err)
	}
	upstream, contentType, _, err := handler.UpstreamBody(buffer.Bytes(), "asr-upstream", http.Header{}, channeltype.AudioAdapterOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if contentType == "" || !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Fatalf("unexpected upstream content type: %q", contentType)
	}
	// 上游体重建后模型名必须替换为上游名，且文件内容保留。
	upstreamHandler := audioTranscriptionHandler{contentType: contentType}
	rebuiltModel, err := upstreamHandler.RequestedModel(upstream)
	if err != nil || rebuiltModel != "asr-upstream" {
		t.Fatalf("upstream model not replaced: %q err=%v", rebuiltModel, err)
	}
	_, fileContent, err := audioMultipartPart(upstream, contentType, "file", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fileContent, wav) {
		t.Fatalf("audio file content changed: %d bytes vs %d", len(fileContent), len(wav))
	}
}

func TestAudioTranscriptionHandlerChatAdapter(t *testing.T) {
	wav := minimalTestWAV()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	_ = writer.WriteField("model", "asr-public")
	part, _ := writer.CreateFormFile("file", "audio.wav")
	_, _ = part.Write(wav)
	_ = writer.Close()

	handler := audioTranscriptionHandler{contentType: writer.FormDataContentType()}
	upstream, contentType, path, err := handler.UpstreamBody(buffer.Bytes(), "mimo-v2.5-asr", http.Header{}, channeltype.AudioAdapterChat)
	if err != nil || contentType != "application/json" || path != "/chat/completions" {
		t.Fatalf("unexpected chat-adapted body: err=%v contentType=%q path=%q", err, contentType, path)
	}
	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type       string `json:"type"`
				InputAudio struct {
					Data string `json:"data"`
				} `json:"input_audio"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(upstream, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "mimo-v2.5-asr" {
		t.Fatalf("upstream model not replaced: %q", payload.Model)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || len(payload.Messages[0].Content) != 1 {
		t.Fatalf("unexpected chat messages: %#v", payload.Messages)
	}
	entry := payload.Messages[0].Content[0]
	if entry.Type != "input_audio" {
		t.Fatalf("unexpected content type: %q", entry.Type)
	}
	data := entry.InputAudio.Data
	if !strings.HasPrefix(data, "data:audio/wav;base64,") {
		t.Fatalf("unexpected data url: %q", data[:min(len(data), 40)])
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "data:audio/wav;base64,"))
	if err != nil || !bytes.Equal(decoded, wav) {
		t.Fatalf("audio base64 mismatch: err=%v bytes=%d", err, len(decoded))
	}
}

func TestAudioTranscriptionHandlerRejectsNonMultipart(t *testing.T) {
	handler := audioTranscriptionHandler{contentType: "application/json"}
	if _, err := handler.RequestedModel([]byte("{}")); err == nil {
		t.Fatal("expected error for non-multipart transcriptions request")
	}
}

func minimalTestWAV() []byte {
	// 44 字节标准 WAV 头 + 2 字节静音数据，仅需保证字节被完整透传。
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	copy(header[8:12], "WAVE")
	return append(header, 0x00, 0x00)
}
