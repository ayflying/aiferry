// Package geminisub converts text-only OpenAI chat wire messages to and from
// the Gemini Antigravity wire format. It does not perform HTTP or authentication.
package geminisub

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

type chatRequest struct {
	Messages            []chatMessage     `json:"messages"`
	Temperature         *float64          `json:"temperature"`
	TopP                *float64          `json:"top_p"`
	MaxTokens           *int              `json:"max_tokens"`
	MaxCompletionTokens *int              `json:"max_completion_tokens"`
	Stop                json.RawMessage   `json:"stop"`
	N                   int               `json:"n"`
	Tools               []json.RawMessage `json:"tools"`
	Functions           []json.RawMessage `json:"functions"`
	Modalities          []string          `json:"modalities"`
}

type chatMessage struct {
	Role         string            `json:"role"`
	Content      json.RawMessage   `json:"content"`
	ToolCalls    []json.RawMessage `json:"tool_calls"`
	FunctionCall json.RawMessage   `json:"function_call"`
}

type requestEnvelope struct {
	Model       string          `json:"model"`
	Project     string          `json:"project"`
	Request     generateRequest `json:"request"`
	RequestType string          `json:"requestType"`
	RequestID   string          `json:"requestId"`
}

type generateRequest struct {
	Contents          []content         `json:"contents"`
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type content struct {
	Role  string     `json:"role"`
	Parts []textPart `json:"parts"`
}

type textPart struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

// BuildRequest converts Chat Completions JSON into a Gemini Antigravity request
// envelope, using the resolved upstream model and subscription project.
func BuildRequest(body []byte, upstreamModel, project string) ([]byte, error) {
	if strings.TrimSpace(upstreamModel) == "" || strings.TrimSpace(project) == "" {
		return nil, gerror.New("gemini subscription request requires an upstream model and project")
	}
	var chat chatRequest
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, gerror.New("invalid chat request JSON")
	}
	if chat.N < 0 || chat.N > 1 || len(chat.Tools) > 0 || len(chat.Functions) > 0 {
		return nil, gerror.New("gemini subscription supports a single text choice without tools")
	}
	for _, modality := range chat.Modalities {
		if modality != "text" {
			return nil, gerror.New("gemini subscription supports only the text modality")
		}
	}
	request, err := chat.generate()
	if err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, gerror.Wrap(err, "generate gemini subscription request ID")
	}
	return json.Marshal(requestEnvelope{
		Model: upstreamModel, Project: project, Request: request,
		RequestType: "agent", RequestID: "agent-" + hex.EncodeToString(nonce[:]),
	})
}

func (chat chatRequest) generate() (generateRequest, error) {
	var request generateRequest
	for i, message := range chat.Messages {
		if len(message.ToolCalls) > 0 || hasJSONValue(message.FunctionCall) {
			return request, gerror.Newf("chat message %d contains unsupported tool calls", i)
		}
		parts, err := parseTextParts(message.Content)
		if err != nil {
			return request, gerror.Wrapf(err, "chat message %d", i)
		}
		switch message.Role {
		case "system", "developer":
			if request.SystemInstruction == nil {
				request.SystemInstruction = &content{Role: "user"}
			}
			request.SystemInstruction.Parts = append(request.SystemInstruction.Parts, parts...)
		case "user":
			request.Contents = append(request.Contents, content{Role: "user", Parts: parts})
		case "assistant":
			request.Contents = append(request.Contents, content{Role: "model", Parts: parts})
		default:
			return request, gerror.Newf("chat message %d has an unsupported role", i)
		}
	}
	if len(request.Contents) == 0 {
		return request, gerror.New("gemini subscription requires a conversation message")
	}
	if request.Contents[len(request.Contents)-1].Role == "model" {
		request.Contents = append(request.Contents, content{Role: "user", Parts: []textPart{{Text: "Continue"}}})
	}
	maxTokens := chat.MaxCompletionTokens
	if maxTokens == nil {
		maxTokens = chat.MaxTokens
	}
	if maxTokens != nil && *maxTokens <= 0 {
		return request, gerror.New("maximum completion tokens must be positive")
	}
	stop, err := parseStop(chat.Stop)
	if err != nil {
		return request, err
	}
	if chat.Temperature != nil || chat.TopP != nil || maxTokens != nil || len(stop) > 0 {
		request.GenerationConfig = &generationConfig{
			Temperature: chat.Temperature, TopP: chat.TopP, MaxOutputTokens: maxTokens, StopSequences: stop,
		}
	}
	return request, nil
}

func parseTextParts(raw json.RawMessage) ([]textPart, error) {
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		return []textPart{{Text: *text}}, nil
	}
	var blocks []struct {
		Type string  `json:"type"`
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil || len(blocks) == 0 {
		return nil, gerror.New("content must be a string or non-empty text-part array")
	}
	parts := make([]textPart, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" || block.Text == nil {
			return nil, gerror.New("content contains a non-text or malformed part")
		}
		parts = append(parts, textPart{Text: *block.Text})
	}
	return parts, nil
}

func parseStop(raw json.RawMessage) ([]string, error) {
	if !hasJSONValue(raw) {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return []string{value}, nil
	}
	var values []*string
	if json.Unmarshal(raw, &values) != nil {
		return nil, gerror.New("stop must be a string or an array of strings")
	}
	stop := make([]string, 0, len(values))
	for _, entry := range values {
		if entry == nil {
			return nil, gerror.New("stop must not contain null")
		}
		stop = append(stop, *entry)
	}
	return stop, nil
}

func hasJSONValue(raw json.RawMessage) bool {
	return len(raw) != 0 && !bytes.Equal(raw, []byte("null"))
}
