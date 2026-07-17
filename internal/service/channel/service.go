package channel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/channelgroup"
	"github.com/yunloli/aiferry/internal/service/channeltype"
	mailservice "github.com/yunloli/aiferry/internal/service/mail"
	"github.com/yunloli/aiferry/internal/service/pricingcache"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
	"github.com/yunloli/aiferry/internal/service/user"
)

const (
	ModeNone        = "none"
	ModeOpenAICosts = "openai_costs"
	ModeSub2API     = "sub2api_usage"
	ModeCustomJSON  = "custom_json"
)

type Service struct {
	app        *app.Service
	types      *channeltype.Service
	groups     *channelgroup.Service
	resilience *system.Service
	usage      *usage.Service
	prices     *pricingcache.Service
	users      *user.Service
	mail       *mailservice.Service
}

type View struct {
	Id                     uint64     `json:"id"`
	Name                   string     `json:"name"`
	Type                   string     `json:"type"`
	TypeName               string     `json:"typeName"`
	BaseURL                string     `json:"baseUrl"`
	HasAPIKey              bool       `json:"hasApiKey"`
	HasManagementKey       bool       `json:"hasManagementKey"`
	OrganizationID         string     `json:"organizationId"`
	ProjectID              string     `json:"projectId"`
	Status                 int        `json:"status"`
	AutoDisabled           bool       `json:"autoDisabled"`
	AutoDisabledAt         *time.Time `json:"autoDisabledAt"`
	AutoDisabledReason     string     `json:"autoDisabledReason"`
	AutoDisabledStatusCode *uint      `json:"autoDisabledStatusCode"`
	Priority               int        `json:"priority"`
	Weight                 uint       `json:"weight"`
	CostQueryMode          string     `json:"costQueryMode"`
	EnabledModelCount      int        `json:"enabledModelCount"`
	DiscoveredModels       int        `json:"discoveredModels"`
	LastTestStatus         string     `json:"lastTestStatus"`
	LastTestLatencyMs      uint       `json:"lastTestLatencyMs"`
	LastTestError          string     `json:"lastTestError"`
	LastTestAt             *time.Time `json:"lastTestAt"`
	LastCostUsed           *float64   `json:"lastCostUsed"`
	LastCostRemaining      *float64   `json:"lastCostRemaining"`
	LastCostCurrency       string     `json:"lastCostCurrency"`
	LastCostAt             *time.Time `json:"lastCostAt"`
	GroupIDs               []uint64   `json:"groupIds"`
	CreatedAt              time.Time  `json:"createdAt"`
}

type ModelView struct {
	Id                uint64     `json:"id" orm:"id"`
	ChannelId         uint64     `json:"channelId" orm:"channel_id"`
	ChannelName       string     `json:"channelName" orm:"channel_name"`
	PublicName        string     `json:"publicName" orm:"public_name"`
	UpstreamName      string     `json:"upstreamName" orm:"upstream_name"`
	Discovered        int        `json:"discovered" orm:"discovered"`
	Enabled           int        `json:"enabled" orm:"enabled"`
	InputPrice        *float64   `json:"inputPrice" orm:"input_price"`
	CachedInputPrice  *float64   `json:"cachedInputPrice" orm:"cached_input_price"`
	CacheWritePrice   *float64   `json:"cacheWritePrice" orm:"cache_write_price"`
	OutputPrice       *float64   `json:"outputPrice" orm:"output_price"`
	ImageInputPrice   *float64   `json:"imageInputPrice" orm:"image_input_price"`
	AudioInputPrice   *float64   `json:"audioInputPrice" orm:"audio_input_price"`
	AudioOutputPrice  *float64   `json:"audioOutputPrice" orm:"audio_output_price"`
	RequestPrice      *float64   `json:"requestPrice" orm:"request_price"`
	BillingMode       string     `json:"billingMode" orm:"billing_mode"`
	LastTestEndpoint  string     `json:"lastTestEndpoint" orm:"last_test_endpoint"`
	LastTestStatus    string     `json:"lastTestStatus" orm:"last_test_status"`
	LastTestLatencyMs uint       `json:"lastTestLatencyMs" orm:"last_test_latency_ms"`
	LastTestError     string     `json:"lastTestError" orm:"last_test_error"`
	LastTestAt        *time.Time `json:"lastTestAt" orm:"last_test_at"`
	UpdatedAt         time.Time  `json:"updatedAt" orm:"updated_at"`
}

type PublicModelView struct {
	Id               uint64   `json:"id" orm:"id"`
	PublicName       string   `json:"publicName" orm:"public_name"`
	InputPrice       *float64 `json:"inputPrice" orm:"input_price"`
	CachedInputPrice *float64 `json:"cachedInputPrice" orm:"cached_input_price"`
	CacheWritePrice  *float64 `json:"cacheWritePrice" orm:"cache_write_price"`
	OutputPrice      *float64 `json:"outputPrice" orm:"output_price"`
	ImageInputPrice  *float64 `json:"imageInputPrice" orm:"image_input_price"`
	AudioInputPrice  *float64 `json:"audioInputPrice" orm:"audio_input_price"`
	AudioOutputPrice *float64 `json:"audioOutputPrice" orm:"audio_output_price"`
	RequestPrice     *float64 `json:"requestPrice" orm:"request_price"`
	BillingMode      string   `json:"billingMode" orm:"billing_mode"`
}

type DiscoveredModel struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
}

func New(appSvc *app.Service, typeSvc *channeltype.Service, groupSvc *channelgroup.Service, resilienceSvc *system.Service, usageSvc *usage.Service, priceCache *pricingcache.Service, userSvc *user.Service, mailSvc *mailservice.Service) *Service {
	return &Service{app: appSvc, types: typeSvc, groups: groupSvc, resilience: resilienceSvc, usage: usageSvc, prices: priceCache, users: userSvc, mail: mailSvc}
}

func normalizeBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("baseUrl must be an absolute HTTP(S) URL")
	}
	return value, nil
}

func (s *Service) writableType(ctx context.Context, code string) (entity.ChannelTypes, channeltype.Config, error) {
	row, config, err := s.types.GetByCode(ctx, code)
	if err != nil {
		return row, config, err
	}
	if row.Status != 1 {
		return row, config, gerror.New("channel type is disabled")
	}
	return row, config, nil
}

func (s *Service) setConfiguredHeaders(ctx context.Context, req *http.Request, channel entity.Channels, authType, headerName, headerPrefix string) error {
	req.Header.Set("Accept", "application/json")
	switch authType {
	case channeltype.AuthNone:
	case channeltype.AuthChannelKey:
		key, err := s.app.Secrets.Decrypt(channel.ApiKeyCipher)
		if err != nil {
			return err
		}
		req.Header.Set(headerName, headerPrefix+key)
	case channeltype.AuthManagementKey:
		if channel.ManagementKeyCipher == "" {
			return gerror.New("channel type requires a management key")
		}
		key, err := s.app.Secrets.Decrypt(channel.ManagementKeyCipher)
		if err != nil {
			return err
		}
		req.Header.Set(headerName, headerPrefix+key)
	default:
		return gerror.New("unsupported channel type auth")
	}
	if channel.OrganizationId != "" {
		req.Header.Set("OpenAI-Organization", channel.OrganizationId)
	}
	if channel.ProjectId != "" {
		req.Header.Set("OpenAI-Project", channel.ProjectId)
	}
	return nil
}

func boolStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeWeight(value uint) uint {
	if value == 0 {
		return 1
	}
	return value
}

func nullableNumber(value *float64) any {
	if value == nil {
		return gdb.Raw("NULL")
	}
	return *value
}

func (s *Service) invalidateRoutes(ctx context.Context) error {
	return s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func routeCacheKey(model string) string {
	return fmt.Sprintf("aiferry:routes:%s", model)
}
