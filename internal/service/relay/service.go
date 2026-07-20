package relay

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/channel"
	"github.com/yunloli/aiferry/internal/service/iplocation"
	mailservice "github.com/yunloli/aiferry/internal/service/mail"
	"github.com/yunloli/aiferry/internal/service/pricingcache"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
	"github.com/yunloli/aiferry/internal/service/user"
)

const maxRequestBody = 16 << 20

type Service struct {
	app        *app.Service
	usage      *usage.Service
	resilience *system.Service
	users      *user.Service
	prices     *pricingcache.Service
	mail       *mailservice.Service
	channels   *channel.Service
	locations  *iplocation.Service
}

type Candidate struct {
	ChannelModelID      uint64 `orm:"channel_model_id"`
	ChannelID           uint64 `orm:"channel_id"`
	ChannelName         string `orm:"channel_name"`
	BaseURL             string `orm:"base_url"`
	ChannelCredentialID uint64
	APIKeyCipher        string
	OrganizationID      string `orm:"organization_id"`
	ProjectID           string `orm:"project_id"`
	ProxyURLCipher      string `orm:"proxy_url_cipher"`
	AdvancedConfig      string `orm:"advanced_config"`
	Priority            int    `orm:"priority"`
	Weight              uint   `orm:"weight"`
	PublicName          string `orm:"public_name"`
	UpstreamName        string `orm:"upstream_name"`
	GroupIDs            []uint64
	ReasoningEffort     string `orm:"-"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type attemptResult struct {
	status             int
	body               []byte
	tokens             usage.TokenUsage
	firstTokenMs       *int64
	errorMessage       string
	latency            time.Duration
	headers            http.Header
	wroteBytes         bool
	timedOut           bool
	upstreamEndpoint   string
	protocolConversion string
}

func New(appSvc *app.Service, usageSvc *usage.Service, resilienceSvc *system.Service, userSvc *user.Service, priceCache *pricingcache.Service, mailSvc *mailservice.Service, channelSvc *channel.Service, locationSvc *iplocation.Service) *Service {
	return &Service{app: appSvc, usage: usageSvc, resilience: resilienceSvc, users: userSvc, prices: priceCache, mail: mailSvc, channels: channelSvc, locations: locationSvc}
}

func (s *Service) Models(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	var rows []struct {
		PublicName string `orm:"public_name"`
	}
	err := dao.ChannelModels.Ctx(ctx).As("m").
		Fields("DISTINCT m.public_name").
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id AND c.status=1").
		Where("m.enabled", 1).
		OrderAsc("m.public_name").
		Scan(&rows)
	if err != nil {
		return ModelList{}, gerror.Wrap(err, "list public models")
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		if len(key.AllowedModels) > 0 && !containsString(key.AllowedModels, row.PublicName) {
			continue
		}
		if !s.prices.IsPriced(row.PublicName) {
			continue
		}
		candidates, routeErr := s.route(ctx, row.PublicName, key)
		if routeErr != nil {
			return ModelList{}, routeErr
		}
		if len(candidates) > 0 {
			models = append(models, Model{ID: row.PublicName, Object: "model", Created: 0, OwnedBy: "aiferry"})
		}
	}
	return ModelList{Object: "list", Data: models}, nil
}

func (s *Service) Handle(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, clientIP, endpoint string, body []byte, key apikey.AuthKey) error {
	if len(body) > maxRequestBody {
		return gerror.New("request body exceeds 16 MiB")
	}
	if !gjson.ValidBytes(body) {
		return gerror.New("request body must be valid JSON")
	}
	requestedModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if requestedModel == "" {
		return gerror.New("model is required")
	}
	isStream := gjson.GetBytes(body, "stream").Bool()
	if endpoint == "/chat/completions" && isStream {
		body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
	}
	if !keyAllowsModel(key, requestedModel) {
		return gerror.New("API key is not allowed to use model " + requestedModel)
	}
	candidates, err := s.route(ctx, requestedModel, key)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return gerror.New("no available channel for model " + requestedModel)
	}
	if !s.prices.IsPriced(requestedModel) {
		return gerror.New("当前模型未配置可用价格，无法计费")
	}
	if err = s.users.CheckBalance(ctx, key.UserId); err != nil {
		return err
	}
	requestID := newRequestID()
	startedAt := time.Now()
	settings, settingsErr := s.resilience.Get(ctx)
	if settingsErr != nil {
		settings = system.DefaultResilienceSettings()
	}
	maxAttempts := min(len(candidates), settings.MaxFailoverAttempts)
	var (
		last          attemptResult
		lastCandidate Candidate
		attempts      int
	)
	for index := 0; index < maxAttempts; index++ {
		outcome := s.attemptChannel(ctx, writer, incomingHeaders, endpoint, body, candidates[index], isStream, startedAt, key.Id, settings)
		attempts += outcome.attempts
		last = outcome.result
		lastCandidate = outcome.candidate
		if outcome.handled {
			candidate := outcome.candidate
			result := outcome.result
			if result.status >= 200 && result.status < 300 {
				s.markSuccess(ctx, candidate.ChannelCredentialID)
			}
			if recordErr := s.record(ctx, requestID, key, candidate, clientIP, endpoint, requestedModel, isStream, attempts, startedAt, result); recordErr != nil {
				if !isStream {
					s.writeBufferedResponse(writer, http.StatusPaymentRequired, openAIError("insufficient_balance", recordErr.Error()), http.Header{"Content-Type": []string{"application/json"}})
				}
				return nil
			}
			if !isStream {
				s.writeBufferedResponse(writer, result.status, result.body, result.headers)
			}
			return nil
		}
	}
	if attempts > 0 {
		last = failedAttemptResult(last, "All eligible channels failed")
		if recordErr := s.record(ctx, requestID, key, lastCandidate, clientIP, endpoint, requestedModel, isStream, attempts, startedAt, last); recordErr != nil {
			g.Log().Errorf(ctx, "record failed request %s: %v", requestID, recordErr)
		}
	} else {
		last = failedAttemptResult(last, "All eligible channels failed")
	}
	s.writeBufferedResponse(writer, last.status, last.body, http.Header{"Content-Type": []string{"application/json"}})
	return nil
}
