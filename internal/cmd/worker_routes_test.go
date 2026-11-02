package cmd

import "testing"

func TestWorkerRouteRequiresMaster(t *testing.T) {
	for _, path := range []string{"/v1/video/generations", "/v1/videos", "/v1/videos/id/content", "/v1/dashboard/billing/usage", "/v1/dashboard/billing/channels"} {
		if !workerRouteRequiresMaster(path) {
			t.Fatalf("unsafe worker endpoint allowed: %s", path)
		}
	}
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/v1/dashboard/billing/subscription", "/v1/images/generations", "/v1/videos-other"} {
		if workerRouteRequiresMaster(path) {
			t.Fatalf("local endpoint blocked: %s", path)
		}
	}
}
