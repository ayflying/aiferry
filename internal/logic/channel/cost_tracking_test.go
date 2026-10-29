package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// 汇总不能把「已用」镜像成「用量」。
//
// 2026-10-29 生产实证：WorkBuddy 三把密钥的已用分别是 337.93 / 152.21 / 52.34，
// 页头却显示「积分余额 剩余 542.48 积分」——正是这三个数的和。原因有两层：
//  1. channelCostSummaries 把 UsedAmount 镜像成 Usage；
//  2. 前端「剩余」优先取 usage，于是拿到的是已用合计。
//
// 这里锁住第一层：汇总只产出已用与余额，单位与文案交给
// applyUsageSummaryMetadata 按渠道类型补齐。
func TestCostSummaryDoesNotMirrorUsedAsUsage(t *testing.T) {
	config, err := channeltype.ParseConfig([]byte(`{"costs":{"adapter":"workbuddy_credits","method":"POST","path":"/billing/meter/get-user-resource-summary","valueType":"usage","usageType":"积分余额","usageUnit":"积分"}}`))
	if err != nil {
		t.Fatalf("解析渠道类型配置失败：%v", err)
	}
	summaries := []CostSummary{{Currency: "XXX", UsedAmount: floatPtr(542.48), RemainingAmount: floatPtr(7297.52)}}
	applyUsageSummaryMetadata(summaries, config.Costs)
	if summaries[0].Usage != nil {
		t.Fatalf("Usage 不应由已用镜像产生：%v", *summaries[0].Usage)
	}
	if summaries[0].UsageType != "积分余额" || summaries[0].UsageUnit != "积分" {
		t.Fatalf("文案元数据应由渠道类型补齐：type=%q unit=%q", summaries[0].UsageType, summaries[0].UsageUnit)
	}
	if summaries[0].RemainingAmount == nil || *summaries[0].RemainingAmount != 7297.52 {
		t.Fatalf("余额必须原样保留：%v", summaries[0].RemainingAmount)
	}
}
