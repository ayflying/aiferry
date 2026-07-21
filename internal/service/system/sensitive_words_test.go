package system

import (
	"reflect"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestNormalizeSensitiveWordSettings(t *testing.T) {
	input := adminapi.SensitiveWordSettingsInput{
		Enabled:         true,
		CheckUserPrompt: true,
		Keywords:        []string{" blocked ", "BLOCKED", "deny", ""},
	}
	want := []string{"blocked", "deny"}
	got := normalizeSensitiveWordSettings(input)
	if !reflect.DeepEqual(got.Keywords, want) {
		t.Fatalf("keywords = %#v, want %#v", got.Keywords, want)
	}
}

func TestDecodeSensitiveWordSettingsEnablesImagesForExistingSettings(t *testing.T) {
	settings, err := decodeSensitiveWordSettings([]byte(`{"enabled":true,"checkUserPrompt":true,"keywords":["blocked"]}`))
	if err != nil {
		t.Fatalf("decodeSensitiveWordSettings() error = %v", err)
	}
	if !settings.ImageEnabled {
		t.Fatal("ImageEnabled = false, want true for settings saved before the image switch was added")
	}
}

func TestHasImageInput(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     string
		want     bool
	}{
		{
			name:     "chat image URL",
			endpoint: "/chat/completions",
			body:     `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`,
			want:     true,
		},
		{
			name:     "chat text only",
			endpoint: "/chat/completions",
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			want:     false,
		},
		{
			name:     "responses image input",
			endpoint: "/responses",
			body:     `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]}`,
			want:     true,
		},
		{
			name:     "responses string input",
			endpoint: "/responses",
			body:     `{"input":"hello"}`,
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasImageInput(test.endpoint, []byte(test.body)); got != test.want {
				t.Fatalf("hasImageInput() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMatchesSensitivePrompt(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     string
		want     bool
	}{
		{
			name:     "chat user string",
			endpoint: "/chat/completions",
			body:     `{"messages":[{"role":"user","content":"Please BLOCKED this request"}]}`,
			want:     true,
		},
		{
			name:     "chat user text item",
			endpoint: "/chat/completions",
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"please deny this"}]}]}`,
			want:     true,
		},
		{
			name:     "chat ignores system and image URL",
			endpoint: "/chat/completions",
			body:     `{"messages":[{"role":"system","content":"blocked"},{"role":"developer","content":"deny"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/blocked.png"}}]}]}`,
			want:     false,
		},
		{
			name:     "responses string input",
			endpoint: "/responses",
			body:     `{"input":"please deny this"}`,
			want:     true,
		},
		{
			name:     "responses user input text",
			endpoint: "/responses",
			body:     `{"input":[{"role":"user","content":[{"type":"input_text","text":"blocked content"}]}]}`,
			want:     true,
		},
		{
			name:     "responses ignores developer and input image",
			endpoint: "/responses",
			body:     `{"input":[{"role":"developer","content":[{"type":"input_text","text":"blocked"}]},{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/deny.png"}]}]}`,
			want:     false,
		},
		{
			name:     "embeddings string array",
			endpoint: "/embeddings",
			body:     `{"input":["allowed","BLOCKED text"]}`,
			want:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesSensitivePrompt(test.endpoint, []byte(test.body), []string{"blocked", "deny"}); got != test.want {
				t.Fatalf("matchesSensitivePrompt() = %v, want %v", got, test.want)
			}
		})
	}
}
