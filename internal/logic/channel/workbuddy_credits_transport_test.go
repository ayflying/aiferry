package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 验证完整请求而非只检查帮助方法：URL、鉴权尾空格与身份隔离必须一起成立。
func TestWorkBuddyCreditsTransportContract(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{"costs":{"adapter":"none"}}`)
	cipher, err := secrets.Encrypt("fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/billing/meter/get-user-resource-summary" || r.Method != http.MethodPost {
			t.Errorf("错误请求 %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-token" {
			t.Errorf("鉴权格式错误 %q", got)
		}
		for _, name := range append(workBuddyChatIdentityHeaders, "Origin", "Referer") {
			if r.Header.Get(name) != "" {
				t.Errorf("早期形态不应混入 %s", name)
			}
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("历史请求体应为空，得到 %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"Packages":[{"PackageCode":"fixture","CycleTotalCapacity":"100","CycleRemainCapacity":"75","CycleUsedCapacity":"25"}]}}`))
	}))
	defer server.Close()
	svc.app.HTTP = server.Client()
	result := &CostResult{}
	err = svc.queryWorkBuddyCosts(context.Background(), entity.Channels{Type: "workbuddy", BaseUrl: server.URL + "/v2"}, cipher, channeltype.CostConfig{HeaderName: "Authorization", HeaderPrefix: "Bearer "}, result)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemainingAmount == nil || *result.RemainingAmount != 75 {
		t.Fatalf("余额错误 %+v", result)
	}
}
