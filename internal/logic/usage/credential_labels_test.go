package usage

import (
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 同一渠道下的密钥按传入顺序编号，不同渠道各自从 1 开始。
func TestCredentialLabelsNumbersPerChannel(t *testing.T) {
	credentials := []entity.ChannelCredentials{
		{Id: 11, ChannelId: 3, Remark: "主密钥"},
		{Id: 12, ChannelId: 3},
		{Id: 21, ChannelId: 7, Remark: "备用"},
	}
	indexes, remarks := credentialLabels(credentials)
	if indexes[11] != 1 || indexes[12] != 2 {
		t.Fatalf("渠道 3 的序号 = %d/%d，期望 1/2", indexes[11], indexes[12])
	}
	if indexes[21] != 1 {
		t.Fatalf("渠道 7 的序号 = %d，期望 1", indexes[21])
	}
	if remarks[11] != "主密钥" || remarks[21] != "备用" {
		t.Fatalf("备注 = %v，期望带出非空备注", remarks)
	}
	if _, ok := remarks[12]; ok {
		t.Fatalf("没有备注的密钥不应出现在备注表里")
	}
}

// 备注前后空白要去掉；只有空白等于没有备注。
func TestCredentialLabelsTrimsBlankRemark(t *testing.T) {
	credentials := []entity.ChannelCredentials{
		{Id: 1, ChannelId: 3, Remark: "  前后有空格  "},
		{Id: 2, ChannelId: 3, Remark: "   "},
	}
	_, remarks := credentialLabels(credentials)
	if remarks[1] != "前后有空格" {
		t.Fatalf("备注 = %q，期望去掉前后空白", remarks[1])
	}
	if _, ok := remarks[2]; ok {
		t.Fatalf("全空白备注不应被带出")
	}
}

// 没有密钥时返回空表而不是 nil，调用方可以直接按下标取值。
func TestCredentialLabelsHandlesEmptyInput(t *testing.T) {
	indexes, remarks := credentialLabels(nil)
	if len(indexes) != 0 || len(remarks) != 0 {
		t.Fatalf("空输入应返回空表，实际 indexes=%v remarks=%v", indexes, remarks)
	}
}
