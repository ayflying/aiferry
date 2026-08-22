package relay

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/system"
)

const maxQualityTextRunes = 12000

type streamResponseCapture struct {
	endpoint  string
	text      strings.Builder
	model     string
	completed bool
}

type modelQualitySignal struct {
	reason string
}

func newStreamResponseCapture(endpoint string) *streamResponseCapture {
	return &streamResponseCapture{endpoint: endpoint}
}

func (c *streamResponseCapture) Observe(line []byte) {
	payload, done, valid := sseDataPayload(line)
	if !valid {
		return
	}
	if done {
		c.completed = true
		return
	}
	if c.endpoint == chatCompletionsEndpoint {
		if c.model == "" {
			c.model = gjson.GetBytes(payload, "model").String()
		}
		c.append(gjson.GetBytes(payload, "choices.0.delta.content").String())
		return
	}
	if c.endpoint != responsesEndpoint {
		return
	}
	response := gjson.GetBytes(payload, "response")
	if c.model == "" {
		c.model = response.Get("model").String()
	}
	switch gjson.GetBytes(payload, "type").String() {
	case "response.output_text.delta":
		c.append(gjson.GetBytes(payload, "delta").String())
	case "response.completed":
		c.completed = true
	}
}

func (c *streamResponseCapture) Text() string {
	return strings.TrimSpace(c.text.String())
}

func (c *streamResponseCapture) Model() string {
	return c.model
}

func (c *streamResponseCapture) Completed() bool {
	return c.completed
}

func (c *streamResponseCapture) append(value string) {
	currentSize := utf8.RuneCountInString(c.text.String())
	if value == "" || currentSize >= maxQualityTextRunes {
		return
	}
	c.text.WriteString(truncateQualityText(value, maxQualityTextRunes-currentSize))
}

func captureBufferedResponse(endpoint string, body []byte) (string, string) {
	model := gjson.GetBytes(body, "model").String()
	switch endpoint {
	case chatCompletionsEndpoint:
		return truncateQualityText(responseContentText(gjson.GetBytes(body, "choices.0.message.content")), maxQualityTextRunes), model
	case responsesEndpoint:
		var text strings.Builder
		gjson.GetBytes(body, "output").ForEach(func(_, output gjson.Result) bool {
			if output.Get("type").String() == "message" {
				text.WriteString(responseContentText(output.Get("content")))
			}
			return utf8.RuneCountInString(text.String()) < maxQualityTextRunes
		})
		return truncateQualityText(text.String(), maxQualityTextRunes), model
	default:
		return "", model
	}
}

func responseContentText(content gjson.Result) string {
	if content.Type == gjson.String {
		return content.String()
	}
	var text strings.Builder
	content.ForEach(func(_, item gjson.Result) bool {
		switch item.Get("type").String() {
		case "text", "input_text", "output_text":
			text.WriteString(item.Get("text").String())
		}
		return utf8.RuneCountInString(text.String()) < maxQualityTextRunes
	})
	return text.String()
}

func (s *sRelay) scheduleModelQualityAnalysis(ctx context.Context, requestID string, candidate Candidate, requestedModel, endpoint string, body []byte, stream, enabled bool, result attemptResult) {
	if !enabled || !isNormalModelQualityResult(stream, result) {
		return
	}
	question := requestQuestionText(endpoint, body)
	answer := strings.TrimSpace(result.responseText)
	if question == "" || answer == "" {
		return
	}
	input := modelQualityInput{
		requestID:      requestID,
		channelID:      candidate.ChannelID,
		credentialID:   candidate.ChannelCredentialID,
		requestedModel: requestedModel,
		expectedModel:  candidate.UpstreamName,
		observedModel:  result.responseModel,
		question:       question,
		answer:         answer,
	}
	go func() {
		signals := inspectModelQuality(input)
		if len(signals) == 0 {
			return
		}
		reasons := make([]string, 0, len(signals))
		for _, signal := range signals {
			reasons = append(reasons, signal.reason)
		}
		if err := s.resilience.RecordModelQualityEvent(context.Background(), system.ModelQualityEventInput{
			RequestID: input.requestID, ChannelID: input.channelID, CredentialID: input.credentialID,
			RequestedModel: input.requestedModel, ExpectedModel: input.expectedModel, ObservedModel: input.observedModel,
			Reasons: reasons, QuestionChars: uint(utf8.RuneCountInString(input.question)), AnswerChars: uint(utf8.RuneCountInString(input.answer)),
		}); err != nil {
			g.Log().Errorf(context.WithoutCancel(ctx), "record model quality event request_id=%s: %v", input.requestID, err)
			return
		}
		g.Log().Warningf(context.WithoutCancel(ctx), "model quality suspicion request_id=%s channel_id=%d credential_id=%d requested_model=%q expected_model=%q observed_model=%q reasons=%s question_chars=%d answer_chars=%d", input.requestID, input.channelID, input.credentialID, input.requestedModel, input.expectedModel, input.observedModel, strings.Join(reasons, ","), utf8.RuneCountInString(input.question), utf8.RuneCountInString(input.answer))
	}()
}

type modelQualityInput struct {
	requestID      string
	channelID      uint64
	credentialID   uint64
	requestedModel string
	expectedModel  string
	observedModel  string
	question       string
	answer         string
}

func isNormalModelQualityResult(stream bool, result attemptResult) bool {
	if result.status < 200 || result.status >= 300 || result.errorMessage != "" || result.timedOut {
		return false
	}
	return !stream || result.streamCompleted
}

func inspectModelQuality(input modelQualityInput) []modelQualitySignal {
	var signals []modelQualitySignal
	if modelTierLower(input.expectedModel, input.observedModel) {
		signals = append(signals, modelQualitySignal{reason: "upstream_model_tier_lower"})
	}
	return signals
}

func requestQuestionText(endpoint string, body []byte) string {
	var text strings.Builder
	switch endpoint {
	case chatCompletionsEndpoint:
		gjson.GetBytes(body, "messages").ForEach(func(_, message gjson.Result) bool {
			if message.Get("role").String() == "user" {
				text.WriteString(responseContentText(message.Get("content")))
			}
			return utf8.RuneCountInString(text.String()) < maxQualityTextRunes
		})
	case responsesEndpoint:
		input := gjson.GetBytes(body, "input")
		if input.Type == gjson.String {
			text.WriteString(input.String())
			break
		}
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("role").String() == "user" {
				text.WriteString(responseContentText(item.Get("content")))
			} else if item.Get("type").String() == "input_text" {
				text.WriteString(item.Get("text").String())
			}
			return utf8.RuneCountInString(text.String()) < maxQualityTextRunes
		})
	}
	return strings.TrimSpace(truncateQualityText(text.String(), maxQualityTextRunes))
}

func modelTierLower(expected, observed string) bool {
	expectedFamily, expectedTier := modelTier(expected)
	observedFamily, observedTier := modelTier(observed)
	return expectedFamily != "" && expectedFamily == observedFamily && expectedTier > 0 && observedTier > 0 && expectedTier > observedTier
}

func modelTier(model string) (string, int) {
	name := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(name, "gpt-"):
		tier := 0
		switch {
		case strings.Contains(name, "gpt-5"):
			tier = 50
		case strings.Contains(name, "gpt-4"):
			tier = 40
		case strings.Contains(name, "gpt-3.5"):
			tier = 35
		case strings.Contains(name, "gpt-3"):
			tier = 30
		}
		if strings.Contains(name, "mini") {
			tier -= 5
		}
		if strings.Contains(name, "nano") {
			tier -= 10
		}
		return "gpt", tier
	case strings.Contains(name, "claude"):
		switch {
		case strings.Contains(name, "opus"):
			return "claude", 30
		case strings.Contains(name, "sonnet"):
			return "claude", 20
		case strings.Contains(name, "haiku"):
			return "claude", 10
		}
	case strings.Contains(name, "gemini"):
		switch {
		case strings.Contains(name, "ultra"):
			return "gemini", 30
		case strings.Contains(name, "pro"):
			return "gemini", 20
		case strings.Contains(name, "flash"):
			return "gemini", 10
		}
	}
	return "", 0
}

func truncateQualityText(value string, maxRunes int) string {
	if maxRunes <= 0 || value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes])
}
