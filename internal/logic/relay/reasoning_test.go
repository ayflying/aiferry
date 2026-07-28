package relay

import "testing"

func TestRequestReasoningEffort(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "chat completions", body: `{"reasoning_effort":"HIGH"}`, want: "high"},
		{name: "responses", body: `{"reasoning":{"effort":"xhigh"}}`, want: "xhigh"},
		{name: "missing", body: `{"model":"gpt-5.6-sol"}`, want: ""},
		{name: "non string", body: `{"reasoning_effort":5}`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := requestReasoningEffort([]byte(test.body)); got != test.want {
				t.Fatalf("requestReasoningEffort() = %q, want %q", got, test.want)
			}
		})
	}
}
