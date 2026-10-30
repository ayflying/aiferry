package system

import "testing"

func TestRerankSensitivePromptTexts(t *testing.T) {
	for _, body := range []string{
		`{"query":"敏感查询","documents":["普通文档"]}`,
		`{"query":"普通查询","documents":["敏感文档"]}`,
		`{"query":"普通查询","documents":[{"title":"标题","text":"敏感正文"}]}`,
		`{"query":"普通查询","documents":[{"nested":{"content":"敏感正文"}}]}`,
	} {
		if !matchesSensitivePrompt("/rerank", []byte(body), []string{"敏感"}) {
			t.Errorf("未检查重排输入：%s", body)
		}
	}
	if matchesSensitivePrompt("/rerank", []byte(`{"query":"普通","documents":["文档"]}`), []string{"敏感"}) {
		t.Fatal("普通输入不应拦截")
	}
}
