package relay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/schedule"
)

// 收发报文落盘（payload log）：每条请求写一个 <requestID>.json，内容为客户端
// 请求体 + 聚合后的响应内容，用于事后排查正文污染、参数改写等「不看原始报文
// 无法定性」的问题。文件只含 body，不含任何鉴权头；写入完全异步，任何失败都
// 只记日志，绝不影响转发主链路。

const (
	// maxPayloadFieldBytes 是单个字段（请求体或响应体）的最大字节数，超出截断并打标记，
	// 避免超大 agent 对话把磁盘写爆。
	maxPayloadFieldBytes = 8 << 20
	// payloadCleanupInterval 是报文目录的清理巡检间隔：按流量规模（单日可达数千条）
	// 每小时清理一次，保证目录文件数稳定收敛在 PayloadLogMaxFiles 以内。
	payloadCleanupInterval = time.Hour
)

// payloadRequestIDPattern 限定 requestID 只能是安全字符集（前缀 + 十六进制），
// 读取接口用它防路径穿越。
var payloadRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// payloadLogState 由 InitPayloadLog 在进程启动时初始化（config.Load 之后的唯一写点），
// 之后只被并发读。
var payloadLogState = struct {
	enabled  bool
	dir      string
	maxFiles int
	maxBytes int64
}{
	enabled:  false,
	dir:      "/app/data/relay-payloads",
	maxFiles: 9999,
	maxBytes: defaultPayloadMaxBytes,
}

// defaultPayloadMaxBytes 是报文目录的默认总容量上限（512MB）。文件数限额单独不够：
// 单条报文实测最大 1.2MB，9999 个能涨到 10GB 级，撑满磁盘会让整台宿主机上的服务
// 一起失败（2026-10-29 事故）。
const defaultPayloadMaxBytes = 512 << 20

// InitPayloadLog 注入收发报文落盘配置，必须在任何请求处理之前调用。
func InitPayloadLog(enabled bool, dir string, maxFiles int, maxBytes int64) {
	payloadLogState.enabled = enabled
	if d := strings.TrimSpace(dir); d != "" {
		payloadLogState.dir = d
	}
	if maxFiles > 0 {
		payloadLogState.maxFiles = maxFiles
	}
	if maxBytes > 0 {
		payloadLogState.maxBytes = maxBytes
	}
}

// payloadEntry 是单个请求的落盘内容。
type payloadEntry struct {
	RequestID          string          `json:"requestId"`
	CreatedAt          string          `json:"createdAt"`
	ChannelID          uint64          `json:"channelId"`
	ChannelName        string          `json:"channelName"`
	RequestedModel     string          `json:"requestedModel"`
	UpstreamModel      string          `json:"upstreamModel"`
	Endpoint           string          `json:"endpoint"`
	UpstreamEndpoint   string          `json:"upstreamEndpoint"`
	ProtocolConversion string          `json:"protocolConversion,omitempty"`
	IsStream           bool            `json:"isStream"`
	Attempts           int             `json:"attempts"`
	HTTPStatus         int             `json:"httpStatus"`
	Request            json.RawMessage `json:"request,omitempty"`
	Response           json.RawMessage `json:"response,omitempty"`
	RequestTruncated   bool            `json:"requestTruncated,omitempty"`
	ResponseTruncated  bool            `json:"responseTruncated,omitempty"`
}

// queuePayloadWrite 异步落盘一条报文：marshal + 临时文件 + rename 原子替换，
// 失败只记日志。requestBody / responseForLog 允许为 nil（例如无请求体的场景）。
func queuePayloadWrite(entry payloadEntry, requestBody, responseForLog []byte) {
	if !payloadLogState.enabled || entry.RequestID == "" {
		return
	}
	go func() {
		entry.CreatedAt = gtime.Now().Format("Y-m-d H:i:s")
		entry.Request, entry.RequestTruncated = truncatePayloadField(requestBody)
		entry.Response, entry.ResponseTruncated = truncatePayloadField(responseForLog)
		data, err := json.Marshal(entry)
		if err != nil {
			g.Log().Warningf(context.Background(), "payload log marshal failed for %s: %v", entry.RequestID, err)
			return
		}
		dir := payloadLogState.dir
		if err = os.MkdirAll(dir, 0o755); err != nil {
			g.Log().Warningf(context.Background(), "payload log mkdir %s failed: %v", dir, err)
			return
		}
		final := filepath.Join(dir, entry.RequestID+".json")
		tmp := final + ".tmp"
		if err = os.WriteFile(tmp, data, 0o600); err != nil {
			g.Log().Warningf(context.Background(), "payload log write %s failed: %v", tmp, err)
			return
		}
		if err = os.Rename(tmp, final); err != nil {
			g.Log().Warningf(context.Background(), "payload log rename %s failed: %v", final, err)
			return
		}
	}()
}

// truncatePayloadField 把超限字段截断到 maxPayloadFieldBytes 并返回是否发生截断。
// 截断可能落在 UTF-8 序列中间，最终 marshal 时无效字节会被替换为 U+FFFD，排查场景可接受。
func truncatePayloadField(data []byte) (json.RawMessage, bool) {
	if len(data) <= maxPayloadFieldBytes {
		if len(data) == 0 {
			return nil, false
		}
		return json.RawMessage(data), false
	}
	return json.RawMessage(data[:maxPayloadFieldBytes]), true
}

// payloadCleanupTaskKey 是「报文文件清理」在计划任务中心的标识。
const payloadCleanupTaskKey = "relay.payload_cleanup"

// StartPayloadCleanup 启动报文目录的每小时清理任务：按文件修改时间从旧到新删除，
// 保证目录文件数不超过 PayloadLogMaxFiles。
func StartPayloadCleanup(ctx context.Context) {
	spec := schedule.Spec{
		Key:         payloadCleanupTaskKey,
		Name:        "请求报文文件清理",
		Description: "每小时清理一次请求报文目录，按修改时间从旧到新删除，保证文件数与总体积都不超上限。用于保留失败请求原文供排障，默认关闭。",
		Kind:        schedule.KindInterval,
		Interval:    payloadCleanupInterval,
		Enabled:     payloadLogState.enabled,
	}
	if !payloadLogState.enabled {
		spec.DisabledReason = "未开启报文记录（PAYLOAD_LOG_ENABLED=false），无需清理"
		schedule.Register(spec)
		g.Log().Infof(ctx, "payload log disabled (PAYLOAD_LOG_ENABLED=false), cleanup not started")
		return
	}
	schedule.Register(spec)
	go func() {
		ticker := time.NewTicker(payloadCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = schedule.Run(ctx, payloadCleanupTaskKey, func(ctx context.Context) error {
					cleanupPayloads(ctx)
					return nil
				})
			}
		}
	}()
}

// cleanupPayloads 执行一轮目录收敛：按 .json 后缀过滤、按修改时间升序删除，
// 直到同时满足「文件数」与「总体积」两条限额。两条都要满足：只有文件数限额时，
// 大报文会把磁盘写满；只有体积限额时，小文件会堆积到 inode/目录项开销失控。
// 无论超限多少，至少保留最新一个文件，保证刚出问题的请求原文还在。
func cleanupPayloads(ctx context.Context) {
	dir := payloadLogState.dir
	maxFiles := payloadLogState.maxFiles
	maxBytes := payloadLogState.maxBytes
	if maxFiles <= 0 && maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			g.Log().Warningf(ctx, "payload log readdir %s failed: %v", dir, err)
		}
		return
	}
	type fileWithMod struct {
		name string
		mod  time.Time
		size int64
	}
	files := make([]fileWithMod, 0, len(entries))
	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, fileWithMod{name: entry.Name(), mod: info.ModTime(), size: info.Size()})
		totalBytes += info.Size()
	}
	countOver := maxFiles > 0 && len(files) > maxFiles
	bytesOver := maxBytes > 0 && totalBytes > maxBytes
	if !countOver && !bytesOver {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	removed, kept, remainingBytes := 0, len(files), totalBytes
	for i := 0; i < len(files); i++ {
		countOK := maxFiles <= 0 || kept <= maxFiles
		bytesOK := maxBytes <= 0 || remainingBytes <= maxBytes
		if countOK && bytesOK {
			break
		}
		if kept <= 1 {
			break
		}
		if err = os.Remove(filepath.Join(dir, files[i].name)); err == nil {
			removed++
			kept--
			remainingBytes -= files[i].size
		}
	}
	g.Log().Infof(ctx, "payload log cleanup removed %d files, kept %d files / %d bytes (limits: %d files, %d bytes)",
		removed, kept, remainingBytes, maxFiles, maxBytes)
}

// ReadPayload 按请求 ID 读取落盘报文。requestID 做严格白名单校验，杜绝路径穿越。
func ReadPayload(requestID string) ([]byte, error) {
	requestID = strings.TrimSpace(requestID)
	if !payloadRequestIDPattern.MatchString(requestID) {
		return nil, gerror.New("非法的 requestId")
	}
	return os.ReadFile(filepath.Join(payloadLogState.dir, requestID+".json"))
}

// payloadStreamCapture 聚合流式响应中「实际写给客户端」的正文与思考内容。
// 观察点在协议转换与敏感数据还原之后（客户端视角），因此正文里若出现上游
// 泄漏的思考标签（如 <analysis>），落盘内容与客户端所见完全一致，可直接定性。
type payloadStreamCapture struct {
	clientEndpoint string
	content        strings.Builder
	reasoning      strings.Builder
}

func newPayloadStreamCapture(clientEndpoint string) *payloadStreamCapture {
	return &payloadStreamCapture{clientEndpoint: clientEndpoint}
}

func (c *payloadStreamCapture) observe(line []byte) {
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return
	}
	if c.clientEndpoint == protocol.ResponsesEndpoint {
		// /responses 直通路径：output 是 Responses 协议事件流。
		switch gjson.GetBytes(payload, "type").String() {
		case "response.output_text.delta":
			c.appendField(&c.content, gjson.GetBytes(payload, "delta").String())
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			c.appendField(&c.reasoning, gjson.GetBytes(payload, "delta").String())
		}
		return
	}
	// /chat/completions（含协议转换后的输出）：output 是 chat SSE。
	c.appendField(&c.content, gjson.GetBytes(payload, "choices.0.delta.content").String())
	c.appendField(&c.reasoning, gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String())
}

func (c *payloadStreamCapture) appendField(builder *strings.Builder, value string) {
	if value == "" || builder.Len() >= maxPayloadFieldBytes {
		return
	}
	if builder.Len()+len(value) > maxPayloadFieldBytes {
		builder.WriteString(value[:maxPayloadFieldBytes-builder.Len()])
		return
	}
	builder.WriteString(value)
}

func (c *payloadStreamCapture) result() (string, string) {
	return c.content.String(), c.reasoning.String()
}

// payloadAggregatedResponse 把流式聚合结果包成响应 JSON（与 chat 响应 message 字段同构）。
func payloadAggregatedResponse(result attemptResult) json.RawMessage {
	if result.payloadCapture == nil {
		return nil
	}
	content, reasoning := result.payloadCapture.result()
	data, err := json.Marshal(map[string]string{"content": content, "reasoning_content": reasoning})
	if err != nil {
		return nil
	}
	return data
}

// savePayloadLog 组装单条报文并异步落盘：请求体 + 响应内容随元信息一起写入
// <requestID>.json。落盘完全旁路，任何失败只记日志，不影响转发与记账。
func (s *sRelay) savePayloadLog(requestID string, candidate Candidate, requestedModel, endpoint string, isStream bool, attempts int, requestBody []byte, result attemptResult, responseForLog json.RawMessage) {
	queuePayloadWrite(payloadEntry{
		RequestID:          requestID,
		ChannelID:          candidate.ChannelID,
		ChannelName:        candidate.ChannelName,
		RequestedModel:     requestedModel,
		UpstreamModel:      candidate.UpstreamName,
		Endpoint:           endpoint,
		UpstreamEndpoint:   result.upstreamEndpoint,
		ProtocolConversion: result.protocolConversion,
		IsStream:           isStream,
		Attempts:           attempts,
		HTTPStatus:         result.status,
	}, requestBody, responseForLog)
}
