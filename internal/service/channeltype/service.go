package channeltype

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	AdapterNone        = "none"
	AdapterOpenAICosts = "openai_costs"
	AdapterSub2API     = "sub2api_usage"
	AdapterCustomJSON  = "custom_json"
	AdapterNewAPIRatio = "newapi_ratio"

	AuthNone          = "none"
	AuthChannelKey    = "channel_key"
	AuthManagementKey = "management_key"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type ModelConfig struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	ListPath     string `json:"listPath"`
	IDPath       string `json:"idPath"`
	AuthType     string `json:"authType"`
	HeaderName   string `json:"headerName"`
	HeaderPrefix string `json:"headerPrefix"`
}

type CostConfig struct {
	Adapter       string `json:"adapter"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	AuthType      string `json:"authType"`
	HeaderName    string `json:"headerName"`
	HeaderPrefix  string `json:"headerPrefix"`
	UsedPath      string `json:"usedPath"`
	RemainingPath string `json:"remainingPath"`
	CurrencyPath  string `json:"currencyPath"`
	FixedCurrency string `json:"fixedCurrency"`
}

type PricingConfig struct {
	Adapter              string `json:"adapter"`
	Method               string `json:"method"`
	Path                 string `json:"path"`
	AuthType             string `json:"authType"`
	HeaderName           string `json:"headerName"`
	HeaderPrefix         string `json:"headerPrefix"`
	ListPath             string `json:"listPath"`
	ModelPath            string `json:"modelPath"`
	NamePath             string `json:"namePath"`
	CurrencyPath         string `json:"currencyPath"`
	ConditionsPath       string `json:"conditionsPath"`
	RatesPath            string `json:"ratesPath"`
	InputPricePath       string `json:"inputPricePath"`
	CachedInputPricePath string `json:"cachedInputPricePath"`
	CacheWritePricePath  string `json:"cacheWritePricePath"`
	OutputPricePath      string `json:"outputPricePath"`
	ImageInputPricePath  string `json:"imageInputPricePath"`
	AudioInputPricePath  string `json:"audioInputPricePath"`
	AudioOutputPricePath string `json:"audioOutputPricePath"`
	RequestPricePath     string `json:"requestPricePath"`
}

type EndpointConfig struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	RequestBody    string `json:"requestBody"`
	SupportsStream bool   `json:"supportsStream"`
	AuthType       string `json:"authType"`
	HeaderName     string `json:"headerName"`
	HeaderPrefix   string `json:"headerPrefix"`
}

type Config struct {
	Models    ModelConfig               `json:"models"`
	Costs     CostConfig                `json:"costs"`
	Pricing   PricingConfig             `json:"pricing"`
	Endpoints map[string]EndpointConfig `json:"endpoints"`
}

type View struct {
	Id        uint64    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Config    Config    `json:"config"`
	Status    int       `json:"status"`
	BuiltIn   int       `json:"builtIn"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Service struct {
	builtins *config.BuiltinRegistry
}

func New(builtins *config.BuiltinRegistry) *Service {
	return &Service{builtins: builtins}
}

func ValidateBuiltins(builtins *config.BuiltinRegistry) error {
	for _, item := range builtins.ChannelTypes {
		if _, err := ParseConfig(item.Config); err != nil {
			return gerror.Wrapf(err, "invalid built-in channel type %s", item.Code)
		}
	}
	return nil
}

func DefaultConfig() Config {
	return Config{
		Models: ModelConfig{
			Method: "GET", Path: "/models", ListPath: "data", IDPath: "id",
			AuthType: AuthChannelKey, HeaderName: "Authorization", HeaderPrefix: "Bearer ",
		},
		Costs: CostConfig{
			Adapter: AdapterOpenAICosts, Method: "GET", Path: "/organization/costs",
			AuthType: AuthManagementKey, HeaderName: "Authorization", HeaderPrefix: "Bearer ", FixedCurrency: "USD",
		},
		Pricing: PricingConfig{Adapter: AdapterNone, Method: "GET", AuthType: AuthChannelKey, HeaderName: "Authorization", HeaderPrefix: "Bearer "},
		Endpoints: map[string]EndpointConfig{
			"chatCompletions":       defaultEndpoint("POST", "/chat/completions", "json", true),
			"responses":             defaultEndpoint("POST", "/responses", "json", true),
			"embeddings":            defaultEndpoint("POST", "/embeddings", "json", false),
			"imagesGenerations":     defaultEndpoint("POST", "/images/generations", "json", false),
			"imagesEdits":           defaultEndpoint("POST", "/images/edits", "multipart", false),
			"audioSpeech":           defaultEndpoint("POST", "/audio/speech", "json", false),
			"audioTranscriptions":   defaultEndpoint("POST", "/audio/transcriptions", "multipart", false),
			"audioTranslations":     defaultEndpoint("POST", "/audio/translations", "multipart", false),
			"videoGenerations":      defaultEndpoint("POST", "/videos", "json", false),
			"videoRetrieve":         defaultEndpoint("GET", "/videos/{video_id}", "none", false),
			"videoContent":          defaultEndpoint("GET", "/videos/{video_id}/content", "none", false),
			"videoRemix":            defaultEndpoint("POST", "/videos/{video_id}/remix", "multipart", false),
			"moderations":           defaultEndpoint("POST", "/moderations", "json", false),
			"files":                 defaultEndpoint("POST", "/files", "multipart", false),
			"batches":               defaultEndpoint("POST", "/batches", "json", false),
			"fineTuningJobs":        defaultEndpoint("POST", "/fine_tuning/jobs", "json", false),
			"realtimeSessions":      defaultEndpoint("POST", "/realtime/sessions", "json", false),
			"realtimeClientSecrets": defaultEndpoint("POST", "/realtime/client_secrets", "json", false),
		},
	}
}

func defaultEndpoint(method, path, requestBody string, supportsStream bool) EndpointConfig {
	return EndpointConfig{
		Method: method, Path: path, RequestBody: requestBody, SupportsStream: supportsStream,
		AuthType: AuthChannelKey, HeaderName: "Authorization", HeaderPrefix: "Bearer ",
	}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	rows := make([]entity.ChannelTypes, 0)
	if err := dao.ChannelTypes.Ctx(ctx).
		Where(dao.ChannelTypes.Columns().BuiltIn, 0).
		OrderAsc(dao.ChannelTypes.Columns().Name).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channel types")
	}
	views := make([]View, 0, len(s.builtins.ChannelTypes)+len(rows))
	for _, item := range s.builtins.ChannelTypes {
		view, err := toView(builtinEntity(item))
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	for _, row := range rows {
		view, err := toView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (entity.ChannelTypes, Config, error) {
	if item, exists := s.builtins.ChannelTypeByID(id); exists {
		row := builtinEntity(item)
		parsed, err := ParseConfig([]byte(row.ConfigJson))
		return row, parsed, err
	}
	var row entity.ChannelTypes
	if err := dao.ChannelTypes.Ctx(ctx).Where(dao.ChannelTypes.Columns().Id, id).Where(dao.ChannelTypes.Columns().BuiltIn, 0).Scan(&row); err != nil {
		return row, Config{}, gerror.Wrap(err, "find channel type")
	}
	if row.Id == 0 {
		return row, Config{}, gerror.New("channel type not found")
	}
	config, err := ParseConfig([]byte(row.ConfigJson))
	return row, config, err
}

func (s *Service) GetByCode(ctx context.Context, code string) (entity.ChannelTypes, Config, error) {
	if item, exists := s.builtins.ChannelTypeByCode(code); exists {
		row := builtinEntity(item)
		parsed, err := ParseConfig([]byte(row.ConfigJson))
		return row, parsed, err
	}
	var row entity.ChannelTypes
	if err := dao.ChannelTypes.Ctx(ctx).
		Where(dao.ChannelTypes.Columns().Code, strings.TrimSpace(code)).
		Where(dao.ChannelTypes.Columns().BuiltIn, 0).
		Scan(&row); err != nil {
		return row, Config{}, gerror.Wrap(err, "find channel type")
	}
	if row.Id == 0 {
		return row, Config{}, gerror.New("channel type not found")
	}
	config, err := ParseConfig([]byte(row.ConfigJson))
	return row, config, err
}

func (s *Service) Create(ctx context.Context, input adminapi.ChannelTypeInput) (uint64, error) {
	name, code := strings.TrimSpace(input.Name), strings.TrimSpace(input.Code)
	if !codePattern.MatchString(code) {
		return 0, gerror.New("channel type code must start with a lowercase letter and contain only lowercase letters, numbers, underscores, or hyphens")
	}
	if _, exists := s.builtins.ChannelTypeByCode(code); exists {
		return 0, gerror.New("渠道类型代码已由本地内置配置占用")
	}
	config, err := ParseConfig(input.Config)
	if err != nil {
		return 0, err
	}
	encoded, _ := json.Marshal(config)
	id, err := dao.ChannelTypes.Ctx(ctx).Data(do.ChannelTypes{
		Name:       name,
		Code:       code,
		ConfigJson: string(encoded),
		Status:     1,
		BuiltIn:    0,
	}).InsertAndGetId()
	if err != nil {
		return 0, gerror.Wrap(err, "create channel type")
	}
	return uint64(id), nil
}

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.ChannelTypeInput) error {
	current, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.BuiltIn == 1 {
		return gerror.New("内置渠道类型由 manifest/builtins.json 管理")
	}
	if strings.TrimSpace(input.Code) != current.Code {
		return gerror.New("channel type code cannot be changed")
	}
	config, err := ParseConfig(input.Config)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(config)
	if _, err = dao.ChannelTypes.Ctx(ctx).Where(dao.ChannelTypes.Columns().Id, id).Data(do.ChannelTypes{
		Name:       strings.TrimSpace(input.Name),
		ConfigJson: string(encoded),
	}).Update(); err != nil {
		return gerror.Wrap(err, "update channel type")
	}
	return nil
}

func (s *Service) SetStatus(ctx context.Context, id uint64, status int) error {
	current, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.BuiltIn == 1 {
		return gerror.New("内置渠道类型由 manifest/builtins.json 管理")
	}
	if _, err := dao.ChannelTypes.Ctx(ctx).Where(dao.ChannelTypes.Columns().Id, id).Data(do.ChannelTypes{Status: normalizeStatus(status)}).Update(); err != nil {
		return gerror.Wrap(err, "update channel type status")
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	current, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.BuiltIn == 1 {
		return gerror.New("内置渠道类型由 manifest/builtins.json 管理")
	}
	count, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Type, current.Code).Count()
	if err != nil {
		return gerror.Wrap(err, "check channel type usage")
	}
	if count > 0 {
		return gerror.New("channel type is in use and cannot be deleted")
	}
	if _, err = dao.ChannelTypes.Ctx(ctx).Where(dao.ChannelTypes.Columns().Id, id).Delete(); err != nil {
		return gerror.Wrap(err, "delete channel type")
	}
	return nil
}

func normalizeStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}

func builtinEntity(item config.BuiltinChannelType) entity.ChannelTypes {
	return entity.ChannelTypes{Id: item.ID, Name: item.Name, Code: item.Code, ConfigJson: string(item.Config), Status: 1, BuiltIn: 1}
}

func toView(row entity.ChannelTypes) (View, error) {
	config, err := ParseConfig([]byte(row.ConfigJson))
	if err != nil {
		return View{}, gerror.Wrapf(err, "invalid config for channel type %s", row.Code)
	}
	return View{
		Id: row.Id, Name: row.Name, Code: row.Code, Config: config,
		Status: row.Status, BuiltIn: row.BuiltIn, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
