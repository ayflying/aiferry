package cluster

import "time"

// SnapshotModelPrice 区分未知价格 NULL 与明确免费价格 0。
type SnapshotModelPrice struct {
	PublicName       string    `json:"publicName" orm:"public_name"`
	BillingMode      string    `json:"billingMode" orm:"billing_mode"`
	InputPrice       *float64  `json:"inputPrice" orm:"input_price"`
	CachedInputPrice *float64  `json:"cachedInputPrice" orm:"cached_input_price"`
	CacheWritePrice  *float64  `json:"cacheWritePrice" orm:"cache_write_price"`
	OutputPrice      *float64  `json:"outputPrice" orm:"output_price"`
	ImageInputPrice  *float64  `json:"imageInputPrice" orm:"image_input_price"`
	AudioInputPrice  *float64  `json:"audioInputPrice" orm:"audio_input_price"`
	AudioOutputPrice *float64  `json:"audioOutputPrice" orm:"audio_output_price"`
	RequestPrice     *float64  `json:"requestPrice" orm:"request_price"`
	CreatedAt        time.Time `json:"createdAt" orm:"created_at"`
	UpdatedAt        time.Time `json:"updatedAt" orm:"updated_at"`
	DeletedAt        time.Time `json:"deletedAt" orm:"deleted_at"`
}

// SnapshotChannelModel 保留渠道价格覆盖项的 NULL（不覆盖）语义。
// HealthScore 等本地运行态仍由 tableSpec.local 排除，不从主节点覆盖。
type SnapshotChannelModel struct {
	Id                 uint64     `json:"id" orm:"id"`
	ChannelId          uint64     `json:"channelId" orm:"channel_id"`
	PublicName         string     `json:"publicName" orm:"public_name"`
	UpstreamName       string     `json:"upstreamName" orm:"upstream_name"`
	Discovered         int        `json:"discovered" orm:"discovered"`
	Enabled            int        `json:"enabled" orm:"enabled"`
	InputPrice         *float64   `json:"inputPrice" orm:"input_price"`
	CachedInputPrice   *float64   `json:"cachedInputPrice" orm:"cached_input_price"`
	OutputPrice        *float64   `json:"outputPrice" orm:"output_price"`
	HealthScore        int        `json:"healthScore" orm:"health_score"`
	AutoDisabledAt     *time.Time `json:"autoDisabledAt" orm:"auto_disabled_at"`
	AutoDisabledReason string     `json:"autoDisabledReason" orm:"auto_disabled_reason"`
	AutoDisabledSource string     `json:"autoDisabledSource" orm:"auto_disabled_source"`
	ClosedWindowsJson  string     `json:"closedWindowsJson" orm:"closed_windows_json"`
	LastTestEndpoint   string     `json:"lastTestEndpoint" orm:"last_test_endpoint"`
	LastTestStatus     string     `json:"lastTestStatus" orm:"last_test_status"`
	LastTestLatencyMs  uint       `json:"lastTestLatencyMs" orm:"last_test_latency_ms"`
	LastTestError      string     `json:"lastTestError" orm:"last_test_error"`
	LastTestAt         time.Time  `json:"lastTestAt" orm:"last_test_at"`
	CreatedAt          time.Time  `json:"createdAt" orm:"created_at"`
	UpdatedAt          time.Time  `json:"updatedAt" orm:"updated_at"`
	DeletedAt          time.Time  `json:"deletedAt" orm:"deleted_at"`
}
