package channel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 火山方舟控制面 GetAFPUsage 接口参数：个人版 Agent Plan 的 AFP 额度查询，
// 含 5 小时 / 近一天 / 近一周 / 近一月四个滚动窗口。该接口不走推理域名
// （volces.com）而是控制面域名（volcengineapi.com），鉴权使用火山 V4 签名。
const (
	afpRegion  = "cn-beijing"
	afpService = "ark"
	afpAction  = "GetAFPUsage"
	afpVersion = "2024-01-01"
)

type afpWindow struct {
	Quota         json.Number `json:"Quota"`
	Used          json.Number `json:"Used"`
	SubscribeTime int64       `json:"SubscribeTime"`
	ResetTime     int64       `json:"ResetTime"`
}

type afpResult struct {
	PlanType    string    `json:"PlanType"`
	AFPFiveHour afpWindow `json:"AFPFiveHour"`
	AFPDaily    afpWindow `json:"AFPDaily"`
	AFPWeekly   afpWindow `json:"AFPWeekly"`
	AFPMonthly  afpWindow `json:"AFPMonthly"`
}

// volcAFPKeyPair 从渠道管理密钥密文解出 AK/SK。凭据统一存渠道管理密钥
// 字段（加密落库），格式固定为 "AccessKeyID:SecretAccessKey"，冒号分隔
// （火山 Secret Access Key 本身不含冒号）。
func (s *sChannel) volcAFPKeyPair(managementKeyCipher string) (string, string, error) {
	if managementKeyCipher == "" {
		return "", "", gerror.New("渠道未配置管理密钥：请在渠道设置中填入火山引擎 Access Key（格式：AccessKeyID:SecretAccessKey）")
	}
	plain, err := s.app.Secrets.Decrypt(managementKeyCipher)
	if err != nil {
		return "", "", gerror.Wrap(err, "解密渠道管理密钥失败")
	}
	accessKeyID, secretAccessKey, found := strings.Cut(strings.TrimSpace(plain), ":")
	if !found || accessKeyID == "" || secretAccessKey == "" {
		return "", "", gerror.New("管理密钥格式无效：火山渠道需填入 AccessKeyID:SecretAccessKey（冒号分隔）")
	}
	return accessKeyID, secretAccessKey, nil
}

// volcSignatureV4 按火山引擎 V4 签名规范计算 Authorization 头。
// 签名头固定为 content-type;host;x-content-sha256;x-date（官方 SDK 同款）。
// 返回签入的 X-Date 值，调用方必须把它设到同一请求头，保证时间一致。
func volcSignatureV4(req *http.Request, body []byte, accessKeyID, secretAccessKey, region, service string) (authorization, xDate string) {
	host := req.URL.Host
	now := time.Now().UTC()
	xDate = now.Format("20060102T150405Z")
	shortDate := xDate[:8]
	payloadHash := sha256.Sum256(body)
	xContentSha256 := hex.EncodeToString(payloadHash[:])

	contentType := req.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		strings.Join([]string{
			"content-type:" + contentType,
			"host:" + host,
			"x-content-sha256:" + xContentSha256,
			"x-date:" + xDate,
		}, "\n") + "\n",
		"content-type;host;x-content-sha256;x-date",
		xContentSha256,
	}, "\n")

	requestHash := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := strings.Join([]string{shortDate, region, service, "request"}, "/")
	stringToSign := strings.Join([]string{
		"HMAC-SHA256",
		xDate,
		credentialScope,
		hex.EncodeToString(requestHash[:]),
	}, "\n")

	hmacSHA256 := func(key, data []byte) []byte {
		mac := hmac.New(sha256.New, key)
		mac.Write(data)
		return mac.Sum(nil)
	}
	kDate := hmacSHA256([]byte(secretAccessKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	return fmt.Sprintf("HMAC-SHA256 Credential=%s, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=%s",
		accessKeyID+"/"+credentialScope, signature), xDate
}

// queryVolcAFP 调用 GetAFPUsage 并解析为额度窗口视图。Agent Plan 各档位
// 仅 5 小时 / 每周 / 每月三个窗口有效（官方确认近一天 AFPDaily 为占位字段，
// Quota 恒为 0），故解析时跳过 Quota 为 0 的窗口。
func (s *sChannel) queryVolcAFP(ctx context.Context, channel entity.Channels, managementKeyCipher string) (QuotaView, error) {
	accessKeyID, secretAccessKey, err := s.volcAFPKeyPair(managementKeyCipher)
	if err != nil {
		return QuotaView{}, err
	}

	endpoint := fmt.Sprintf("https://ark.%s.volcengineapi.com/?Action=%s&Version=%s", afpRegion, afpAction, afpVersion)
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return QuotaView{}, gerror.Wrap(err, "创建火山套餐额度查询请求失败")
	}
	req.Header.Set("Content-Type", "application/json")
	authorization, xDate := volcSignatureV4(req, body, accessKeyID, secretAccessKey, afpRegion, afpService)
	req.Header.Set("Authorization", authorization)
	req.Header.Set("X-Date", xDate)
	payloadHash := sha256.Sum256(body)
	req.Header.Set("X-Content-Sha256", hex.EncodeToString(payloadHash[:]))

	client, err := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if err != nil {
		return QuotaView{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return QuotaView{}, gerror.Wrap(err, "请求火山套餐额度接口失败")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return QuotaView{}, gerror.Wrap(err, "读取火山套餐额度接口响应失败")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := gjson.GetBytes(raw, "ResponseMetadata.Error.Message").String()
		if message == "" {
			message = strings.TrimSpace(string(raw))
		}
		if len(message) > 300 {
			message = message[:300] + "…"
		}
		return QuotaView{}, gerror.Newf("火山套餐额度接口返回 HTTP %d：%s", resp.StatusCode, message)
	}
	return parseAFPResponse(raw)
}

// parseAFPResponse 把 GetAFPUsage 的 Result 解析为统一额度视图。
// AFP 数值为字符串形式的数字，按 0 窗口跳过规则只保留有效窗口。
func parseAFPResponse(raw []byte) (QuotaView, error) {
	var payload struct {
		ResponseMetadata struct {
			Error struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result afpResult `json:"Result"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return QuotaView{}, gerror.Wrap(err, "解析火山套餐额度响应失败")
	}
	if payload.ResponseMetadata.Error.Code != "" {
		return QuotaView{}, gerror.Newf("火山套餐额度查询失败：%s %s", payload.ResponseMetadata.Error.Code, payload.ResponseMetadata.Error.Message)
	}

	view := QuotaView{Mode: "volcengine_afp", Level: payload.Result.PlanType, QueriedAt: time.Now()}
	afpWindowEntry := func(kind, label string, window afpWindow) *QuotaWindow {
		total, err := window.Quota.Float64()
		if err != nil || total <= 0 {
			return nil
		}
		used, err := window.Used.Float64()
		if err != nil {
			used = 0
		}
		entry := &QuotaWindow{
			Kind:        kind,
			Label:       label,
			UsedPercent: used / total * 100,
			Used:        &used,
			Total:       &total,
		}
		remaining := total - used
		if remaining < 0 {
			remaining = 0
		}
		entry.Remaining = &remaining
		if window.ResetTime > 0 {
			reset := time.UnixMilli(window.ResetTime)
			entry.NextResetAt = &reset
		}
		return entry
	}
	for _, item := range []struct {
		kind, label string
		window      afpWindow
	}{
		{QuotaWindowFiveHour, "5 小时额度", payload.Result.AFPFiveHour},
		{QuotaWindowWeekly, "每周额度", payload.Result.AFPWeekly},
		{QuotaWindowMonthly, "月度额度", payload.Result.AFPMonthly},
	} {
		if entry := afpWindowEntry(item.kind, item.label, item.window); entry != nil {
			view.Windows = append(view.Windows, *entry)
		}
	}
	if len(view.Windows) == 0 {
		return QuotaView{}, gerror.Newf("火山套餐未返回有效的 AFP 额度窗口，原始响应：%s", quotaRawSnippet(raw))
	}
	return view, nil
}
