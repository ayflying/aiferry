package channel

import (
	"reflect"
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 测试扣费与正式计费同口径：先公开名，再上游名兜底，重复名只取一次。
func TestModelPriceNamesPrefersPublicThenUpstream(t *testing.T) {
	for _, test := range []struct {
		name  string
		model entity.ChannelModels
		want  []string
	}{
		{"映射模型", entity.ChannelModels{PublicName: "gpt-6.1-sol", UpstreamName: "gpt-6.2-sol"}, []string{"gpt-6.1-sol", "gpt-6.2-sol"}},
		{"未映射", entity.ChannelModels{PublicName: "glm-5", UpstreamName: "glm-5"}, []string{"glm-5"}},
		{"上游名为空", entity.ChannelModels{PublicName: "glm-5"}, []string{"glm-5"}},
		{"公开名为空", entity.ChannelModels{PublicName: " ", UpstreamName: "gpt-6.2-sol"}, []string{"gpt-6.2-sol"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := modelPriceNames(test.model); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("modelPriceNames = %v, want %v", got, test.want)
			}
		})
	}
}
