package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	codexIssuer   = "https://auth.openai.com"
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexLoginTTL = 15 * time.Minute
)

// codexAuthRequest 只访问官方授权域；不把渠道可编辑根地址当作令牌接收方。
// 错误仅返回状态码，避免上游把授权码、刷新令牌回显到管理端或日志。
func codexAuthRequest(ctx context.Context, client *http.Client, path, contentType, payload string, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexIssuer+path, strings.NewReader(payload))
	if err != nil {
		return 0, gerror.Wrap(err, "创建 Codex 授权请求失败")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	safeClient := *client
	safeClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safeClient.Do(req)
	if err != nil {
		return 0, gerror.New("请求 Codex 官方授权服务失败，请检查网络或渠道代理")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, gerror.New("读取 Codex 授权响应失败")
	}
	if err = json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, gerror.New("Codex 授权服务返回无效 JSON")
	}
	return resp.StatusCode, nil
}

func isCodexUpstreamURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Host == "chatgpt.com" && (u.Path == "/backend-api/wham/usage" || strings.HasPrefix(u.Path, "/backend-api/codex/"))
}

// interval 可为数字或字符串；官方当前返回字符串。
type codexSeconds int

func (n *codexSeconds) UnmarshalJSON(b []byte) error {
	var value json.Number
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	parsed, err := value.Int64()
	if err != nil {
		return err
	}
	*n = codexSeconds(parsed)
	return nil
}
