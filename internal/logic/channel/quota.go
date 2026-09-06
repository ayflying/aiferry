package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/upstreamerror"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// quotaCacheTTL 控制额度查询的 Redis 缓存时长。额度查询是管理端只读操作，
// 短缓存既能避免连点把上游账号打进风控，又不会明显滞后于真实用量。
const quotaCacheTTL = time.Minute

const (
	QuotaWindowFiveHour = "five_hour"
	QuotaWindowWeekly   = "weekly"
	QuotaWindowMCP      = "mcp"
)

type QuotaWindow struct {
	Kind        string     `json:"kind"`
	Label       string     `json:"label"`
	UsedPercent float64    `json:"usedPercent"`
	Used        *float64   `json:"used,omitempty"`
	Total       *float64   `json:"total,omitempty"`
	Remaining   *float64   `json:"remaining,omitempty"`
	NextResetAt *time.Time `json:"nextResetAt,omitempty"`
}

type QuotaView struct {
	Mode      string        `json:"mode"`
	Level     string        `json:"level"`
	Windows   []QuotaWindow `json:"windows"`
	QueriedAt time.Time     `json:"queriedAt"`
	Cached    bool          `json:"cached"`
	// PartialErrors 记录多密钥合并查询中失败的密钥明细；为空表示全部成功。
	PartialErrors []string `json:"partialErrors,omitempty"`
}

// QueryQuota 查询渠道上游的套餐额度（如智谱 GLM Coding Plan 的积分窗口）。
// 结果按渠道缓存一分钟，重复点击不会重复请求上游；refresh 为 true 时绕过缓存强制查询。
func (s *sChannel) QueryQuota(ctx context.Context, channelID uint64, refresh bool) (QuotaView, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return QuotaView{}, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return QuotaView{}, err
	}
	if config.Quota.Adapter == "" || config.Quota.Adapter == channeltype.AdapterNone {
		return QuotaView{}, gerror.New("该渠道类型不支持套餐额度查询")
	}
	if !refresh {
		if view, ok := s.readQuotaCache(ctx, channel.Id); ok {
			view.Cached = true
			return view, nil
		}
	}
	view, err := s.fetchQuota(ctx, channel, config.Quota)
	if err != nil {
		return QuotaView{}, err
	}
	s.writeQuotaCache(ctx, channel.Id, view)
	return view, nil
}

func (s *sChannel) readQuotaCache(ctx context.Context, channelID uint64) (QuotaView, bool) {
	if s.app.Redis == nil {
		return QuotaView{}, false
	}
	encoded, err := s.app.Redis.Get(ctx, fmt.Sprintf("aiferry:channel-quota:%d", channelID)).Bytes()
	if err != nil || len(encoded) == 0 {
		return QuotaView{}, false
	}
	var view QuotaView
	if err = json.Unmarshal(encoded, &view); err != nil || len(view.Windows) == 0 {
		return QuotaView{}, false
	}
	return view, true
}

func (s *sChannel) writeQuotaCache(ctx context.Context, channelID uint64, view QuotaView) {
	if s.app.Redis == nil {
		return
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		return
	}
	_ = s.app.Redis.Set(ctx, fmt.Sprintf("aiferry:channel-quota:%d", channelID), encoded, quotaCacheTTL).Err()
}

// fetchQuota 并发查询渠道全部上游密钥的套餐额度并合并视图：同一套餐的
// 百分比窗口取平均，MCP 调用次数累加，重置时间取最早，档位去重后拼接。
// 部分密钥查询失败时仍返回成功部分，失败明细附在 PartialErrors 中。
func (s *sChannel) fetchQuota(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig) (QuotaView, error) {
	endpoint, err := resolveHostURL(channel.BaseUrl, config.Path)
	if err != nil {
		return QuotaView{}, err
	}
	ciphers, err := s.credentialCiphers(ctx, channel.Id)
	if err != nil {
		return QuotaView{}, err
	}
	views := make([]QuotaView, len(ciphers))
	failed := make([]string, len(ciphers))
	var wg sync.WaitGroup
	for index, credential := range ciphers {
		wg.Add(1)
		go func(index int, credential quotaCredential) {
			defer wg.Done()
			view, err := s.fetchQuotaWithCredential(ctx, channel, config, endpoint, credential.Cipher)
			if err != nil {
				failed[index] = err.Error()
				return
			}
			views[index] = view
		}(index, credential)
	}
	wg.Wait()

	success := make([]QuotaView, 0, len(ciphers))
	failures := make([]string, 0, len(ciphers))
	for index := range ciphers {
		if failed[index] != "" {
			failures = append(failures, fmt.Sprintf("密钥 %s：%s", ciphers[index].Prefix, failed[index]))
			continue
		}
		success = append(success, views[index])
	}
	if len(success) == 0 {
		return QuotaView{}, gerror.New(strings.Join(failures, "；"))
	}
	view := mergeQuotaViews(success)
	view.PartialErrors = failures
	return view, nil
}

// fetchQuotaWithCredential 先按类型配置的认证头（裸 key）请求上游；若返回
// 401 则回退 Bearer 前缀重试一次，兼容个别上游账号对 Authorization 头的差异要求。
func (s *sChannel) fetchQuotaWithCredential(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, endpoint, cipher string) (QuotaView, error) {
	view, err := s.fetchQuotaWithPrefix(ctx, channel, config, endpoint, config.HeaderPrefix, cipher)
	if err == nil || !isQuotaUnauthorized(err) || config.HeaderPrefix == "Bearer " {
		return view, err
	}
	return s.fetchQuotaWithPrefix(ctx, channel, config, endpoint, "Bearer ", cipher)
}

// mergeQuotaViews 合并多个密钥的套餐额度：百分比窗口按 kind 分组取平均
// （多账号共享套餐额度时，平均值最能代表整体水位），MCP 调用次数累加，
// 重置时间取最早（任何一个账号重置都意味着部分额度恢复），档位去重拼接。
func mergeQuotaViews(views []QuotaView) QuotaView {
	merged := QuotaView{QueriedAt: time.Now()}
	levels := make([]string, 0, len(views))
	seenLevels := make(map[string]struct{}, len(views))
	orders := make([]string, 0, 3)
	percentSums := make(map[string]float64, 3)
	percentCounts := make(map[string]int, 3)
	usedCounts := make(map[string]*float64, 3)
	totalCounts := make(map[string]*float64, 3)
	remainCounts := make(map[string]*float64, 3)
	resetTimes := make(map[string]*time.Time, 3)
	labels := make(map[string]string, 3)
	for _, view := range views {
		if view.Level != "" {
			if _, seen := seenLevels[view.Level]; !seen {
				seenLevels[view.Level] = struct{}{}
				levels = append(levels, view.Level)
			}
		}
		for _, window := range view.Windows {
			if _, ok := percentSums[window.Kind]; !ok {
				orders = append(orders, window.Kind)
			}
			percentSums[window.Kind] += window.UsedPercent
			percentCounts[window.Kind]++
			usedCounts[window.Kind] = addQuotaValue(usedCounts[window.Kind], window.Used)
			totalCounts[window.Kind] = addQuotaValue(totalCounts[window.Kind], window.Total)
			remainCounts[window.Kind] = addQuotaValue(remainCounts[window.Kind], window.Remaining)
			resetTimes[window.Kind] = earliestQuotaTime(resetTimes[window.Kind], window.NextResetAt)
			labels[window.Kind] = window.Label
		}
	}
	merged.Level = strings.Join(levels, " / ")
	for _, kind := range orders {
		merged.Windows = append(merged.Windows, QuotaWindow{
			Kind: kind, Label: labels[kind],
			UsedPercent: percentSums[kind] / float64(percentCounts[kind]),
			Used:        usedCounts[kind], Total: totalCounts[kind], Remaining: remainCounts[kind],
			NextResetAt: resetTimes[kind],
		})
	}
	return merged
}

func addQuotaValue(base *float64, value *float64) *float64 {
	if value == nil {
		return base
	}
	if base == nil {
		merged := *value
		return &merged
	}
	sum := *base + *value
	return &sum
}

func earliestQuotaTime(base, value *time.Time) *time.Time {
	if value == nil {
		return base
	}
	if base == nil || value.Before(*base) {
		merged := *value
		return &merged
	}
	return base
}

func (s *sChannel) fetchQuotaWithPrefix(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, endpoint, headerPrefix, cipher string) (QuotaView, error) {
	body, err := s.fetchUpstreamJSON(ctx, channel, cipher, upstreamJSONRequest{
		Method:       config.Method,
		Endpoint:     endpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: headerPrefix,
		BodyLimit:    1 << 20,
		RequestError: "创建上游套餐额度查询请求失败",
		FetchError:   "请求上游套餐额度接口失败",
		ReadError:    "读取上游套餐额度接口响应失败",
		InvalidError: "上游套餐额度接口返回了无效 JSON",
		StatusError: func(status int, payload []byte) error {
			return gerror.Newf("上游套餐额度接口返回 HTTP %d：%s", status, upstreamerror.Message(payload, http.StatusText(status)))
		},
	})
	if err != nil {
		return QuotaView{}, err
	}
	return parseQuotaResponse(config.Adapter, body)
}

type quotaCredential struct {
	Prefix string
	Cipher string
}

// credentialCiphers 返回渠道全部上游密钥（与费用查询一致，包含已停用密钥，
// 便于检查被自动禁用渠道的剩余额度），按 ID 升序保证结果顺序稳定。
func (s *sChannel) credentialCiphers(ctx context.Context, channelID uint64) ([]quotaCredential, error) {
	rows := make([]credentialRow, 0, 1)
	if err := dao.ChannelCredentials.Ctx(ctx).Fields(dao.ChannelCredentials.Columns().Id, dao.ChannelCredentials.Columns().KeyPrefix, dao.ChannelCredentials.Columns().ApiKeyCipher).Where(do.ChannelCredentials{ChannelId: channelID}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channel credentials for quota query")
	}
	if len(rows) == 0 {
		return nil, gerror.New("channel has no upstream credential")
	}
	credentials := make([]quotaCredential, 0, len(rows))
	for _, row := range rows {
		prefix := row.KeyPrefix
		if prefix == "" {
			prefix = fmt.Sprintf("#%d", row.Id)
		}
		credentials = append(credentials, quotaCredential{Prefix: prefix, Cipher: row.ApiKeyCipher})
	}
	return credentials, nil
}

func parseQuotaResponse(adapter string, body []byte) (QuotaView, error) {
	var payload struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Success bool   `json:"success"`
		Data    struct {
			Level  interface{} `json:"level"`
			Limits []struct {
				Type          string   `json:"type"`
				Percentage    *float64 `json:"percentage"`
				Usage         *float64 `json:"usage"`
				CurrentValue  *float64 `json:"currentValue"`
				Remaining     *float64 `json:"remaining"`
				NextResetTime int64    `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return QuotaView{}, gerror.Wrap(err, "decode upstream quota response")
	}
	if payload.Code != 0 && payload.Code != http.StatusOK || !payload.Success && payload.Msg != "" {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = "上游未返回套餐额度数据"
		}
		return QuotaView{}, gerror.New("上游套餐额度查询失败：" + message)
	}
	view := QuotaView{Mode: adapter, Level: normalizeQuotaLevel(payload.Data.Level), QueriedAt: time.Now()}
	tokenLimits := make([]int, 0, len(payload.Data.Limits))
	for index, item := range payload.Data.Limits {
		switch item.Type {
		case "TOKENS_LIMIT":
			tokenLimits = append(tokenLimits, index)
		case "TIME_LIMIT":
			view.Windows = append(view.Windows, QuotaWindow{
				Kind: QuotaWindowMCP, Label: "MCP 月度调用",
				UsedPercent: quotaPercent(item.Percentage),
				Used:        item.CurrentValue, Total: item.Usage, Remaining: item.Remaining,
				NextResetAt: quotaResetTime(item.NextResetTime),
			})
		}
	}
	// 新套餐返回两个 TOKENS_LIMIT（按重置时间升序：5 小时窗口、每周窗口）；
	// 老套餐只有一个 TOKENS_LIMIT，仅展示 5 小时窗口。
	sort.Slice(tokenLimits, func(i, j int) bool {
		return payload.Data.Limits[tokenLimits[i]].NextResetTime < payload.Data.Limits[tokenLimits[j]].NextResetTime
	})
	labels := []string{"5 小时额度", "每周额度"}
	kinds := []string{QuotaWindowFiveHour, QuotaWindowWeekly}
	for position, index := range tokenLimits {
		if position >= len(labels) {
			position = len(labels) - 1
		}
		item := payload.Data.Limits[index]
		view.Windows = append(view.Windows, QuotaWindow{
			Kind: kinds[position], Label: labels[position],
			UsedPercent: quotaPercent(item.Percentage),
			NextResetAt: quotaResetTime(item.NextResetTime),
		})
	}
	if len(view.Windows) == 0 {
		return QuotaView{}, gerror.New("上游未返回可用的套餐额度窗口，请确认账号已订阅套餐")
	}
	return view, nil
}

func quotaPercent(value *float64) float64 {
	if value == nil {
		return 0
	}
	if *value < 0 {
		return 0
	}
	return *value
}

func quotaResetTime(milliseconds int64) *time.Time {
	if milliseconds <= 0 {
		return nil
	}
	reset := time.UnixMilli(milliseconds)
	return &reset
}

// normalizeQuotaLevel 兼容上游把套餐档位返回为字符串或数字两种形式。
func normalizeQuotaLevel(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return fmt.Sprintf("%v", value)
	default:
		return ""
	}
}

func isQuotaUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "HTTP 401") || strings.Contains(message, "HTTP 403")
}

// resolveHostURL 把 host 根路径（如 /api/monitor/usage/quota/limit）拼接到
// 渠道 API 根地址的 scheme://host 上。套餐额度接口不在 API 版本路径之下，
// 因此不能像 resolveEndpointURL 那样直接拼接在根地址后面。
func resolveHostURL(baseURL, path string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", gerror.New("channel base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("channel base URL must be an absolute HTTP(S) URL")
	}
	if !strings.HasPrefix(path, "/") {
		return "", gerror.New("quota path must be an absolute path")
	}
	return parsed.Scheme + "://" + parsed.Host + path, nil
}
