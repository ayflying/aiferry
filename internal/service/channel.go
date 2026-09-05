// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	"net/http"

	"github.com/shopspring/decimal"
	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type (
	IChannel interface {
		SetStatus(ctx context.Context, channelID uint64, status int) error
		List(ctx context.Context) ([]View, error)
		Get(ctx context.Context, id uint64) (entity.Channels, error)
		Create(ctx context.Context, input adminapi.ChannelInput) (uint64, error)
		Update(ctx context.Context, id uint64, input adminapi.ChannelInput) error
		Delete(ctx context.Context, id uint64) error
		QueryCost(ctx context.Context, channelID uint64, input adminapi.CostQueryInput) (CostResult, error)
		QueryQuota(ctx context.Context, channelID uint64, refresh bool) (QuotaView, error)
		// RestoreCostQueryDisabledCredentials restores only credentials that were
		// previously disabled by the removed balance-query rule. Cost data is for
		// display and notification, not proof that a credential is unusable.
		RestoreCostQueryDisabledCredentials(ctx context.Context) error
		StartCostSync(ctx context.Context)
		// ApplyUsageCost preserves the legacy channel-level accounting entry point for
		// callers that do not know the selected credential. Relay requests use the
		// credential-aware method below.
		ApplyUsageCost(ctx context.Context, channelID uint64, amount decimal.Decimal) error
		ApplyCredentialUsageCost(ctx context.Context, channelID uint64, credentialID uint64, amount decimal.Decimal) error
		CreateCredential(ctx context.Context, channelID uint64, input adminapi.ChannelCredentialInput) (uint64, error)
		ListCredentials(ctx context.Context, channelID uint64) ([]CredentialView, error)
		SetCredentialStatus(ctx context.Context, channelID uint64, credentialID uint64, input adminapi.ChannelCredentialStatusInput) error
		DeleteCredential(ctx context.Context, channelID uint64, credentialID uint64) error
		HasAvailableCredential(ctx context.Context, channelID uint64) (bool, error)
		SelectCredential(ctx context.Context, apiKeyID uint64, channelID uint64, excluded map[uint64]struct{}) (RouteCredential, error)
		CredentialForTest(ctx context.Context, channelID uint64, credentialID uint64) (RouteCredential, error)
		StartHealthChecks(ctx context.Context)
		InvalidateListCache(ctx context.Context)
		DiscoverModels(ctx context.Context, channelID uint64) ([]DiscoveredModel, error)
		SelectModels(ctx context.Context, channelID uint64, input adminapi.ModelSelectionInput) ([]ModelView, error)
		ListModels(ctx context.Context, channelID uint64) ([]ModelView, error)
		DeleteFailedModels(ctx context.Context, channelID uint64) (int, error)
		ListPublicModels(ctx context.Context) ([]PublicModelView, error)
		UpdateModel(ctx context.Context, id uint64, input adminapi.ModelInput) error
		UpdatePublicModelPrice(ctx context.Context, id uint64, input adminapi.ModelPriceInput) error
		SyncAllPrices(ctx context.Context) (PriceSyncResult, error)
		SyncPriceSource(ctx context.Context, channelID uint64) (PriceSyncResult, error)
		SyncExternalPriceSource(ctx context.Context, sourceID uint64, sourceName string, baseURL string, config channeltype.PricingConfig) (PriceSyncResult, error)
		ListPriceRules(ctx context.Context, modelID uint64) ([]PriceRuleView, error)
		CreatePriceRule(ctx context.Context, modelID uint64, input adminapi.PriceRuleInput) (uint64, error)
		UpdatePriceRule(ctx context.Context, id uint64, input adminapi.PriceRuleInput) error
		DeletePriceRule(ctx context.Context, id uint64) error
		HTTPClientForProxy(proxyURLCipher string) (*http.Client, error)
		TestModel(ctx context.Context, input adminapi.ModelTestInput, userID uint64) (TestResult, error)
	}
)

var (
	localChannel IChannel
)

func Channel() IChannel {
	if localChannel == nil {
		panic("implement not found for interface IChannel, forgot register?")
	}
	return localChannel
}

func RegisterChannel(i IChannel) {
	localChannel = i
}
