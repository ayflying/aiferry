package relay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizePayload(t *testing.T) {
	input := []byte(`{"channelName":"内部渠道","request":{"messages":[{"content":"正常正文 sk-testsecret Bearer upstream-secret"}],"api_key":"private-key","headers":{"Authorization":"private-auth","Cookie":"session-secret"}},"response":{"access_token":"private-token","usage":{"total_tokens":123}}}`)
	for _, admin := range []bool{false, true} {
		output, err := SanitizePayload(input, admin)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"sk-testsecret", "upstream-secret", "private-key", "private-auth", "session-secret", "private-token"} {
			if strings.Contains(string(output), secret) {
				t.Fatalf("泄露敏感信息 %s", secret)
			}
		}
		var value map[string]any
		if err := json.Unmarshal(output, &value); err != nil {
			t.Fatal(err)
		}
		_, hasChannel := value["channelName"]
		if hasChannel != admin {
			t.Fatalf("渠道名称可见性错误: admin=%v", admin)
		}
		if !strings.Contains(string(output), "正常正文") || !strings.Contains(string(output), "123") {
			t.Fatal("不应删除正文和用量")
		}
	}
}

func TestSanitizePayloadRejectsInvalidJSON(t *testing.T) {
	if _, err := SanitizePayload([]byte(`not json`), false); err == nil {
		t.Fatal("无效报文不得原样返回")
	}
}
