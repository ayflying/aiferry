package relay

import "testing"

func TestStreamResponseCaptureChat(t *testing.T) {
	capture := newStreamResponseCapture(chatCompletionsEndpoint)
	capture.Observe([]byte("data: {\"model\":\"gpt-5\",\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n"))
	capture.Observe([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n"))
	capture.Observe([]byte("data: [DONE]\n"))
	if !capture.Completed() || capture.Model() != "gpt-5" || capture.Text() != "hello world" {
		t.Fatalf("unexpected capture: completed=%t model=%q text=%q", capture.Completed(), capture.Model(), capture.Text())
	}
}

func TestStreamResponseCaptureResponses(t *testing.T) {
	capture := newStreamResponseCapture(responsesEndpoint)
	capture.Observe([]byte("data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-5\"}}\n"))
	capture.Observe([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n"))
	capture.Observe([]byte("data: {\"type\":\"response.completed\"}\n"))
	if !capture.Completed() || capture.Model() != "gpt-5" || capture.Text() != "answer" {
		t.Fatalf("unexpected capture: completed=%t model=%q text=%q", capture.Completed(), capture.Model(), capture.Text())
	}
}

func TestModelQualitySignals(t *testing.T) {
	question := "请分析这个服务连续出现超时的根因，并给出至少三个可执行的排查步骤、修复方案和回滚方案。"
	signals := inspectModelQuality(modelQualityInput{
		expectedModel: "gpt-5",
		observedModel: "gpt-4o-mini",
		question:      question,
		answer:        "不知道。",
	})
	if len(signals) != 1 || signals[0].reason != "upstream_model_tier_lower" {
		t.Fatalf("expected upstream_model_tier_lower signal, got %#v", signals)
	}
}

func TestRequestQuestionTextResponsesInputText(t *testing.T) {
	question := requestQuestionText(responsesEndpoint, []byte(`{"input":[{"type":"input_text","text":"请分析这个问题"}]}`))
	if question != "请分析这个问题" {
		t.Fatalf("unexpected question: %q", question)
	}
}

func TestModelQualitySkipsIncompleteStream(t *testing.T) {
	if isNormalModelQualityResult(true, attemptResult{status: 200}) {
		t.Fatal("incomplete stream must not be analyzed")
	}
	if !isNormalModelQualityResult(false, attemptResult{status: 200}) {
		t.Fatal("normal buffered response should be analyzed")
	}
}
