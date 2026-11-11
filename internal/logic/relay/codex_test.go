package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

const codexCompletedSSE = `event: response.completed
data: {"type":"response.completed","response":{"id":"resp_test","object":"response","model":"future-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}

`

func TestCodexResponsesOnlyPlan(t *testing.T) {
	service := &sRelay{}
	for _, model := range []string{"gpt-5.3-codex", "o3", "future-model", "embedding-named-model"} {
		for _, endpoint := range []string{protocol.ChatCompletionsEndpoint, protocol.ResponsesEndpoint} {
			plan := service.preferredProtocolPlan(context.Background(), endpoint, Candidate{ChannelType: "codex", UpstreamName: model}, true, adminapi.SystemResilienceSettingsInput{})
			if plan.UpstreamEndpoint() != protocol.ResponsesEndpoint {
				t.Fatalf("%s %s routed to %s", model, endpoint, plan.UpstreamEndpoint())
			}
		}
	}
}

func TestCodexAttemptAggregatesAndNeverFallsBack(t *testing.T) {
	for _, endpoint := range []string{protocol.ChatCompletionsEndpoint, protocol.ResponsesEndpoint} {
		for _, status := range []int{http.StatusOK, http.StatusNotFound} {
			t.Run(endpoint+http.StatusText(status), func(t *testing.T) {
				calls := 0
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/responses" {
						t.Errorf("unsupported endpoint called: %s", r.URL.Path)
					}
					body, _ := io.ReadAll(r.Body)
					if !gjson.GetBytes(body, "stream").Bool() || gjson.GetBytes(body, "store").Bool() || !gjson.GetBytes(body, "instructions").Exists() || gjson.GetBytes(body, "temperature").Exists() || gjson.GetBytes(body, "max_output_tokens").Exists() {
						t.Errorf("invalid request: %s", body)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = w.Write([]byte(codexCompletedSSE))
					} else {
						_, _ = w.Write([]byte(`{"error":{"message":"unsupported responses endpoint"}}`))
					}
				}))
				defer upstream.Close()
				secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
				if err != nil {
					t.Fatal(err)
				}
				appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
				service := &sRelay{app: appSvc, channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil), prices: pricingcache.New()}
				candidate := Candidate{ChannelID: 7, ChannelType: "codex", UpstreamName: "future-model", BaseURL: upstream.URL}
				body := []byte(`{"model":"alias","messages":[{"role":"user","content":"hi"}],"temperature":0.8,"max_completion_tokens":16}`)
				if endpoint == protocol.ResponsesEndpoint {
					body = []byte(`{"model":"alias","input":"hi","temperature":0.8,"max_output_tokens":16}`)
				}
				result, _, err := service.attempt(context.Background(), nil, http.Header{}, endpoint, body, candidate, false, 1, 2, adminapi.SystemResilienceSettingsInput{ProtocolConversionEnabled: true, NonStreamTimeoutSeconds: 30}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if calls != 1 || result.status != status {
					t.Fatalf("calls=%d status=%d body=%s", calls, result.status, result.body)
				}
				if status == http.StatusOK {
					path := "output.0.content.0.text"
					if endpoint == protocol.ChatCompletionsEndpoint {
						path = "choices.0.message.content"
					}
					if gjson.GetBytes(result.body, path).String() != "OK" || result.headers.Get("Content-Type") != "application/json" || result.tokens.Input == nil || *result.tokens.Input != 3 {
						t.Fatalf("bad aggregate: %s tokens=%+v", result.body, result.tokens)
					}
				}
			})
		}
	}
}

func TestCodexStreamingUsesExistingProtocolConverter(t *testing.T) {
	for _, endpoint := range []string{protocol.ChatCompletionsEndpoint, protocol.ResponsesEndpoint} {
		plan := (&sRelay{}).preferredProtocolPlan(context.Background(), endpoint, Candidate{ChannelType: "codex", UpstreamName: "future-model"}, true, adminapi.SystemResilienceSettingsInput{})
		if !plan.Converts() {
			if endpoint != protocol.ResponsesEndpoint {
				t.Fatal("Chat must use converter")
			}
			continue
		}
		converter := protocol.NewStreamConverter(plan)
		var output []byte
		for _, line := range splitSSELines([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n" + codexCompletedSSE)) {
			for _, converted := range converter.Transform(line) {
				output = append(output, converted...)
			}
		}
		if len(output) == 0 {
			t.Fatal("missing converted stream")
		}
		found := false
		for _, line := range splitSSELines(output) {
			payload, _, valid := relaySSEDataPayload(line)
			if valid && gjson.GetBytes(payload, "choices.0.delta.content").String() == "OK" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Chat stream lost text: %s", output)
		}
	}
}

func TestCodexRejectsUnsupportedEndpointAndTruncatedSSE(t *testing.T) {
	service := &sRelay{}
	candidate := Candidate{ChannelType: "codex"}
	settings := adminapi.SystemResilienceSettingsInput{}
	audio, _ := service.attemptAudioUpstream(context.Background(), nil, nil, nil, candidate, nil, time.Now(), settings)
	image, _ := service.attemptImagesEditUpstream(context.Background(), nil, nil, nil, candidate, settings, time.Now())
	video := service.callVideoUpstream(context.Background(), http.MethodPost, "unused", nil, nil, candidate)
	if audio.status != http.StatusBadRequest || image.status != http.StatusBadRequest || video.status != http.StatusBadRequest {
		t.Fatalf("specialized endpoints not rejected locally: audio=%d image=%d video=%d", audio.status, image.status, video.status)
	}
	result, _, err := service.attempt(context.Background(), nil, nil, protocol.ChatCompletionsEndpoint, nil, candidate, false, 0, 0, settings, nil)
	if err != nil || result.status != http.StatusBadRequest {
		t.Fatalf("conversion-disabled Chat not rejected: %+v %v", result, err)
	}
	for _, endpoint := range []string{"/embeddings", "/images/generations", "/rerank"} {
		result, _, err := (&sRelay{}).attempt(context.Background(), nil, nil, endpoint, nil, Candidate{ChannelType: "codex"}, false, 0, 0, adminapi.SystemResilienceSettingsInput{}, nil)
		if err != nil || result.status != http.StatusBadRequest {
			t.Fatalf("%s: %+v %v", endpoint, result, err)
		}
	}
	for _, raw := range []string{`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n", `data: {"type":"response.failed","response":{"error":{"message":"quota exceeded"}}}` + "\n"} {
		_, failure := aggregateForcedStream([]byte(raw), protocol.DirectPlan(protocol.ResponsesEndpoint))
		if failure == nil || failure.status < 400 {
			t.Fatalf("failure swallowed: %s", raw)
		}
	}
}
