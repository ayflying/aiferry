package relay

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestAudioSpeechHandlerReplacesModel(t *testing.T) {
	body := []byte(`{"model":"tts-public","input":"你好","voice":"alloy"}`)
	model, err := (audioSpeechHandler{}).RequestedModel(body)
	if err != nil || model != "tts-public" {
		t.Fatalf("unexpected speech model: %q err=%v", model, err)
	}
	upstream, contentType, err := (audioSpeechHandler{}).UpstreamBody(body, "tts-upstream", http.Header{})
	if err != nil || contentType != "application/json" {
		t.Fatalf("unexpected speech upstream body: err=%v contentType=%q", err, contentType)
	}
	if !bytes.Contains(upstream, []byte(`"model":"tts-upstream"`)) || !bytes.Contains(upstream, []byte(`"input":"你好"`)) {
		t.Fatalf("speech upstream body lost fields: %s", upstream)
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
	upstream, contentType, err := handler.UpstreamBody(buffer.Bytes(), "asr-upstream", http.Header{})
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
