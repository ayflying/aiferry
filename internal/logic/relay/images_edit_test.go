package relay

import (
	"bytes"
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/apikey"
)

// buildImagesEditMultipart 构造一个 /images/edits multipart 请求体，用于验证解析与重建。
func buildImagesEditMultipart(t *testing.T, fields map[string]string, fileName string, fileContent []byte) ([]byte, string) {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	for _, name := range []string{"model", "prompt", "size"} {
		if value, exists := fields[name]; exists {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatalf("write field %s: %v", name, err)
			}
		}
	}
	part, err := writer.CreateFormFile("image", fileName)
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	if _, err = part.Write(fileContent); err != nil {
		t.Fatalf("write image part: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buffer.Bytes(), writer.FormDataContentType()
}

func TestParseImagesEditMultipartKeepsTextAndImageFields(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nfake-png-bytes")
	body, contentType := buildImagesEditMultipart(t, map[string]string{
		"model":  "gpt-image-2",
		"prompt": "同一间教室里的第二个镜头",
		"size":   "1024x1024",
	}, "frame-01.png", image)

	parts, err := parseImagesEditMultipart(body, contentType)
	if err != nil {
		t.Fatalf("parse multipart: %v", err)
	}
	if got := imagesEditTextPart(parts, "model"); got != "gpt-image-2" {
		t.Fatalf("unexpected model: %q", got)
	}
	if got := imagesEditTextPart(parts, "prompt"); got != "同一间教室里的第二个镜头" {
		t.Fatalf("unexpected prompt: %q", got)
	}
	if !imagesEditHasImage(parts) {
		t.Fatal("image part should be detected")
	}
	for _, part := range parts {
		if part.fileName == "frame-01.png" {
			if !bytes.Equal(part.value, image) {
				t.Fatal("image bytes should be preserved verbatim")
			}
			return
		}
	}
	t.Fatal("image part not found after parsing")
}

func TestParseImagesEditMultipartRejectsNonMultipartBody(t *testing.T) {
	if _, err := parseImagesEditMultipart([]byte(`{"model":"gpt-image-2"}`), "application/json"); err == nil {
		t.Fatal("application/json body must be rejected")
	}
	if _, err := parseImagesEditMultipart([]byte(""), "multipart/form-data"); err == nil {
		t.Fatal("missing boundary must be rejected")
	}
}

func TestImagesEditHasImageAcceptsIndexedReferenceNames(t *testing.T) {
	parts := []imagesEditPart{
		{name: "model", value: []byte("gpt-image-2")},
		{name: "image_2", fileName: "second.png", value: []byte("png")},
	}
	if !imagesEditHasImage(parts) {
		t.Fatal("image_2 should count as a reference image")
	}
	if imagesEditHasImage(parts[:1]) {
		t.Fatal("text-only request must not be treated as carrying an image")
	}
}

func TestEncodeImagesEditPartsRewritesModelAndKeepsImageBytes(t *testing.T) {
	image := []byte("reference-image-bytes")
	body, contentType := buildImagesEditMultipart(t, map[string]string{
		"model":  "gpt-image-2",
		"prompt": "继续同一个场景",
		"size":   "1024x1024",
	}, "frame-02.png", image)

	parts, err := parseImagesEditMultipart(body, contentType)
	if err != nil {
		t.Fatalf("parse multipart: %v", err)
	}
	encoded, encodedType, err := encodeImagesEditParts(parts, "upstream-image-model")
	if err != nil {
		t.Fatalf("encode multipart: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(encodedType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("unexpected encoded content type: %q (%v)", encodedType, err)
	}
	reader := multipart.NewReader(bytes.NewReader(encoded), params["boundary"])
	seen := map[string]string{}
	var imagePart []byte
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		content := new(bytes.Buffer)
		if _, err = content.ReadFrom(part); err != nil {
			t.Fatalf("read encoded part: %v", err)
		}
		if part.FileName() != "" {
			imagePart = content.Bytes()
			continue
		}
		seen[part.FormName()] = content.String()
	}
	if seen["model"] != "upstream-image-model" {
		t.Fatalf("model should be rewritten to upstream name, got %q", seen["model"])
	}
	if seen["prompt"] != "继续同一个场景" || seen["size"] != "1024x1024" {
		t.Fatalf("text fields should be forwarded unchanged: %#v", seen)
	}
	if !bytes.Equal(imagePart, image) {
		t.Fatalf("image bytes should survive re-encoding, got %q", imagePart)
	}
}

func TestImagesEditPromptProbeFeedsSensitiveWordCheck(t *testing.T) {
	parts := []imagesEditPart{
		{name: "model", value: []byte("gpt-image-2")},
		{name: "prompt", value: []byte("  雨夜街道  ")},
	}
	probe := imagesEditPromptProbe("gpt-image-2", parts)
	if got := string(probe); got != `{"model":"gpt-image-2","prompt":"雨夜街道"}` {
		t.Fatalf("unexpected probe payload: %s", got)
	}
}

func TestHandleImagesEditValidatesRequestBeforeRouting(t *testing.T) {
	service := &sRelay{}

	// 缺少 model 字段：必须在触达渠道路由前失败。
	noModel, contentType := buildImagesEditMultipart(t, map[string]string{"prompt": "只有提示词"}, "frame.png", []byte("png"))
	if err := service.HandleImagesEdit(context.Background(), http.Header{}, "127.0.0.1", "/images/edits", noModel, contentType, apikey.AuthKey{}, nil); err == nil {
		t.Fatal("missing model must be rejected")
	}

	// 有 model 但没有参考图：multipart 不是合法编辑请求。
	noImageFields := &bytes.Buffer{}
	writer := multipart.NewWriter(noImageFields)
	if err := writer.WriteField("model", "gpt-image-2"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "无参考图"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := service.HandleImagesEdit(context.Background(), http.Header{}, "127.0.0.1", "/images/edits", noImageFields.Bytes(), writer.FormDataContentType(), apikey.AuthKey{}, nil); err == nil {
		t.Fatal("request without an image file must be rejected")
	}
}
