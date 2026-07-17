package pricesource

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channel"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

var sourceCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type Config struct {
	BaseURL string                    `json:"baseUrl"`
	Pricing channeltype.PricingConfig `json:"pricing"`
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
	channels *channel.Service
}

func New(channelSvc *channel.Service) *Service {
	return &Service{channels: channelSvc}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	rows := make([]entity.PriceSources, 0)
	if err := dao.PriceSources.Ctx(ctx).
		OrderDesc(dao.PriceSources.Columns().BuiltIn).
		OrderAsc(dao.PriceSources.Columns().Name).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list price sources")
	}
	views := make([]View, 0, len(rows))
	for _, row := range rows {
		view, err := toView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Create(ctx context.Context, input adminapi.PriceSourceInput) (uint64, error) {
	code := strings.TrimSpace(input.Code)
	if !sourceCodePattern.MatchString(code) {
		return 0, gerror.New("price source code must start with a lowercase letter and contain only lowercase letters, numbers, underscores, or hyphens")
	}
	config, err := ParseConfig(input.Config)
	if err != nil {
		return 0, err
	}
	encoded, _ := json.Marshal(config)
	id, err := dao.PriceSources.Ctx(ctx).Data(do.PriceSources{
		Name:       strings.TrimSpace(input.Name),
		Code:       code,
		ConfigJson: string(encoded),
		Status:     normalizeStatus(input.Status),
		BuiltIn:    0,
	}).InsertAndGetId()
	return uint64(id), gerror.Wrap(err, "create price source")
}

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.PriceSourceInput) error {
	current, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Code) != current.Code {
		return gerror.New("price source code cannot be changed")
	}
	config, err := ParseConfig(input.Config)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(config)
	result, err := dao.PriceSources.Ctx(ctx).Where(dao.PriceSources.Columns().Id, id).Data(do.PriceSources{
		Name:       strings.TrimSpace(input.Name),
		ConfigJson: string(encoded),
		Status:     normalizeStatus(input.Status),
	}).Update()
	if err != nil {
		return gerror.Wrap(err, "update price source")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("price source not found")
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	current, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.BuiltIn == 1 {
		return gerror.New("built-in price sources cannot be deleted")
	}
	result, err := dao.PriceSources.Ctx(ctx).Where(dao.PriceSources.Columns().Id, id).Delete()
	if err != nil {
		return gerror.Wrap(err, "delete price source")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("price source not found")
	}
	return nil
}

func (s *Service) Sync(ctx context.Context, id uint64) (channel.PriceSyncResult, error) {
	row, config, err := s.Get(ctx, id)
	if err != nil {
		return channel.PriceSyncResult{}, err
	}
	if row.Status != 1 {
		return channel.PriceSyncResult{}, gerror.New("price source is disabled")
	}
	return s.channels.SyncExternalPriceSource(ctx, row.Id, row.Name, config.BaseURL, config.Pricing)
}

func (s *Service) Get(ctx context.Context, id uint64) (entity.PriceSources, Config, error) {
	var row entity.PriceSources
	if err := dao.PriceSources.Ctx(ctx).Where(dao.PriceSources.Columns().Id, id).Scan(&row); err != nil {
		return row, Config{}, gerror.Wrap(err, "find price source")
	}
	if row.Id == 0 {
		return row, Config{}, gerror.New("price source not found")
	}
	config, err := ParseConfig([]byte(row.ConfigJson))
	return row, config, err
}

func ParseConfig(raw []byte) (Config, error) {
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, gerror.Wrap(err, "invalid price source JSON")
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Config{}, gerror.New("price source baseUrl must be an absolute HTTP(S) URL")
	}
	config.Pricing, err = channeltype.ParsePricingConfig(config.Pricing)
	if err != nil {
		return Config{}, err
	}
	if config.Pricing.Adapter == channeltype.AdapterNone {
		return Config{}, gerror.New("price source pricing.adapter is required")
	}
	if config.Pricing.AuthType != channeltype.AuthNone {
		return Config{}, gerror.New("public price sources currently support only pricing.authType none")
	}
	return config, nil
}

func toView(row entity.PriceSources) (View, error) {
	config, err := ParseConfig([]byte(row.ConfigJson))
	if err != nil {
		return View{}, gerror.Wrapf(err, "invalid config for price source %s", row.Code)
	}
	return View{
		Id: row.Id, Name: row.Name, Code: row.Code, Config: config, Status: row.Status,
		BuiltIn: row.BuiltIn, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func normalizeStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}
