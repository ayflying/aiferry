package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteBufferedStreamResponseWritesAfterValidation(t *testing.T) {
	writer := httptest.NewRecorder()
	result := attemptResult{
		status:       http.StatusOK,
		headers:      http.Header{"Content-Type": []string{"text/event-stream"}},
		streamOutput: [][]byte{[]byte("data: first\n\n"), []byte("data: [DONE]\n\n")},
	}
	if err := (&Service{}).writeBufferedStreamResponse(writer, result); err != nil {
		t.Fatal(err)
	}
	if writer.Code != http.StatusOK {
		t.Fatalf("status = %d", writer.Code)
	}
	if writer.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", writer.Header().Get("Content-Type"))
	}
	if body := writer.Body.String(); body != "data: first\n\ndata: [DONE]\n\n" {
		t.Fatalf("body = %q", body)
	}
}
