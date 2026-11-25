package geminisub

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// Usage uses OpenAI counting semantics: PromptTokens includes CachedTokens,
// and CompletionTokens includes any reasoning tokens reported by Gemini.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"-"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatContent `json:"message,omitempty"`
	Delta        *chatContent `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatContent struct {
	Role      string  `json:"role,omitempty"`
	Content   *string `json:"content,omitempty"`
	Reasoning string  `json:"reasoning_content,omitempty"`
}

type chatUsage struct {
	Usage
	PromptDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
}

type responseState struct {
	finish          string
	hasText         bool
	hasUsage        bool
	promptTokens    int
	candidateTokens int
	thoughtTokens   int
	totalTokens     int
	cacheTokens     int
}

// ConvertStream writes OpenAI chat SSE incrementally. It emits a separate usage
// frame when metadata is supplied, followed by [DONE] on success. The caller
// owns the input/output streams, including cancellation and transport flushing.
func ConvertStream(dst io.Writer, src io.Reader, model, completionID string) (Usage, error) {
	frame := newChatResponse("chat.completion.chunk", model, completionID)
	started := false
	writeDelta := func(delta chatContent, reason *string) error {
		frame.Choices = []chatChoice{{Delta: &delta, FinishReason: reason}}
		return writeEvent(dst, frame)
	}
	start := func() error {
		if started {
			return nil
		}
		started = true
		return writeDelta(chatContent{Role: "assistant"}, nil)
	}
	state, err := convert(src, func(delta chatContent) error {
		if err := start(); err != nil {
			return err
		}
		return writeDelta(delta, nil)
	})
	usage := state.usage()
	if err != nil {
		return usage, err
	}
	if err := start(); err != nil {
		return usage, err
	}
	if err := writeDelta(chatContent{}, &state.finish); err != nil {
		return usage, err
	}
	if state.hasUsage {
		frame.Choices = []chatChoice{}
		frame.Usage = state.chatUsage()
		if err := writeEvent(dst, frame); err != nil {
			return usage, err
		}
	}
	_, err = io.WriteString(dst, "data: [DONE]\n\n")
	return usage, err
}

// ConvertResponse aggregates upstream SSE, a JSON object, or a JSON array of
// chunks into a single OpenAI Chat Completion response.
func ConvertResponse(src io.Reader, model, completionID string) ([]byte, Usage, error) {
	var text, reasoning strings.Builder
	state, err := convert(src, func(delta chatContent) error {
		if delta.Content != nil {
			text.WriteString(*delta.Content)
		}
		reasoning.WriteString(delta.Reasoning)
		return nil
	})
	usage := state.usage()
	if err != nil {
		return nil, usage, err
	}
	message := text.String()
	result := newChatResponse("chat.completion", model, completionID)
	result.Choices = []chatChoice{{
		Message:      &chatContent{Role: "assistant", Content: &message, Reasoning: reasoning.String()},
		FinishReason: &state.finish,
	}}
	if state.hasUsage {
		result.Usage = state.chatUsage()
	}
	body, err := json.Marshal(result)
	return body, usage, err
}

func newChatResponse(object, model, completionID string) chatResponse {
	return chatResponse{ID: completionID, Object: object, Created: time.Now().Unix(), Model: model}
}

func writeEvent(dst io.Writer, frame chatResponse) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	_, err = io.WriteString(dst, "data: "+string(payload)+"\n\n")
	return err
}

func convert(src io.Reader, emit func(chatContent) error) (responseState, error) {
	var state responseState
	terminal, err := readUpstream(src, func(data upstreamResponse) error { return state.visit(data, emit) })
	if err != nil {
		return state, err
	}
	if state.finish == "" {
		if !terminal && state.hasText {
			return state, gerror.New("gemini subscription stream ended without a finish reason")
		}
		state.finish = "stop"
	}
	if !state.hasText && state.finish != "content_filter" {
		return state, gerror.New("gemini subscription returned no text")
	}
	return state, nil
}

func (s *responseState) visit(data upstreamResponse, emit func(chatContent) error) error {
	if err := checkUpstreamError(data.Error); err != nil {
		return err
	}
	if data.Response != nil {
		data = *data.Response
		if err := checkUpstreamError(data.Error); err != nil {
			return err
		}
	}
	if data.Usage != nil {
		s.hasUsage = true
		setCount(&s.promptTokens, data.Usage.Prompt)
		setCount(&s.candidateTokens, data.Usage.Candidates)
		setCount(&s.thoughtTokens, data.Usage.Thoughts)
		setCount(&s.totalTokens, data.Usage.Total)
		setCount(&s.cacheTokens, data.Usage.Cached)
	}
	if data.PromptFeedback.BlockReason != "" && data.PromptFeedback.BlockReason != "BLOCK_REASON_UNSPECIFIED" {
		s.finish = "content_filter"
	}
	if len(data.Candidates) == 0 {
		return nil
	}
	candidate := data.Candidates[0]
	if reason := candidate.FinishReason; reason != "" && reason != "FINISH_REASON_UNSPECIFIED" {
		var err error
		s.finish, err = finishReason(reason)
		if err != nil {
			return err
		}
	}
	for _, part := range candidate.Content.Parts {
		if hasJSONValue(part.FunctionCall) || hasJSONValue(part.InlineData) || hasJSONValue(part.FileData) {
			return gerror.New("gemini subscription returned unsupported non-text content")
		}
		if part.Text == nil || *part.Text == "" {
			continue
		}
		s.hasText = true
		delta := chatContent{Content: part.Text}
		if part.Thought {
			delta = chatContent{Reasoning: *part.Text}
		}
		if err := emit(delta); err != nil {
			return err
		}
	}
	return nil
}

func checkUpstreamError(raw json.RawMessage) error {
	if !hasJSONValue(raw) {
		return nil
	}
	// The response may include sensitive request metadata. Use only numeric code.
	var upstream struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(raw, &upstream) == nil && upstream.Code > 0 {
		return gerror.Newf("gemini subscription upstream error (%d)", upstream.Code)
	}
	return gerror.New("gemini subscription upstream error")
}

func finishReason(reason string) (string, error) {
	switch reason {
	case "STOP":
		return "stop", nil
	case "MAX_TOKENS":
		return "length", nil
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "MODEL_ARMOR":
		return "content_filter", nil
	default:
		return "", gerror.New("gemini subscription returned an unsupported finish reason")
	}
}

func setCount(dst, src *int) {
	if src != nil && *src >= 0 {
		*dst = *src
	}
}

func (s responseState) usage() Usage {
	completion := s.candidateTokens + s.thoughtTokens
	return Usage{
		PromptTokens: s.promptTokens, CompletionTokens: completion, CachedTokens: s.cacheTokens,
		TotalTokens: max(s.totalTokens, s.promptTokens+completion),
	}
}

func (s responseState) chatUsage() *chatUsage {
	u := &chatUsage{Usage: s.usage()}
	if s.cacheTokens > 0 {
		u.PromptDetails = &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: s.cacheTokens}
	}
	if s.thoughtTokens > 0 {
		u.CompletionDetails = &struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}{ReasoningTokens: s.thoughtTokens}
	}
	return u
}
