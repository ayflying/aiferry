package mail

import (
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/system"
)

func renderNotification(notification system.AutoDisableNotification) (string, string) {
	return renderAutoDisableTransition("AiFerry", notification)
}

// 密钥全部不可用时，标题落在渠道上，正文带上最后一把不可用密钥的细节。
func TestRenderCredentialAvailabilityLost(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindCredentialAvailability,
		ChannelID:           26,
		ChannelName:         "智谱",
		CredentialID:        62,
		CredentialKeyPrefix: "sk-ab",
		Source:              system.AutoDisableSourceRelayRequest,
		StatusCode:          401,
		Reason:              "上游返回鉴权失败",
	})
	for _, want := range []string{"渠道已无可用密钥", "智谱"} {
		if !strings.Contains(subject, want) {
			t.Fatalf("标题缺少 %q：%s", want, subject)
		}
	}
	for _, want := range []string{"全部不可用", "sk-ab", "转发请求", "401", "上游返回鉴权失败"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
}

// 恢复时说明当前还有几个可用密钥，并带上当初不可用的来源。
func TestRenderCredentialAvailabilityRestored(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                  system.NotificationKindCredentialAvailability,
		Recovered:             true,
		ChannelID:             26,
		ChannelName:           "智谱",
		ActiveCredentialCount: 1,
		Source:                "manual",
		Reason:                "管理端手动调整密钥状态",
	})
	if !strings.Contains(subject, "渠道已恢复可用") {
		t.Fatalf("标题不符：%s", subject)
	}
	for _, want := range []string{"当前启用 1 个", "管理端手动操作"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
}

// 单个模型不可用时只提醒这个模型，并说明渠道里还有别的模型可用。
func TestRenderModelUnavailable(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindModelAvailability,
		ChannelID:           26,
		ChannelName:         "智谱",
		ModelID:             8,
		ModelName:           "glm-5.3-flash",
		AvailableModelCount: 3,
	})
	if !strings.Contains(subject, "渠道模型不可用") || !strings.Contains(subject, "glm-5.3-flash") {
		t.Fatalf("标题不符：%s", subject)
	}
	for _, want := range []string{"所有启用密钥组合都在隔离中", "其它模型不受影响", "仍有可用模型：3 个"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
}

// 手动关渠道用「手动关闭」措辞，与「模型全挂导致不可用」区分开。
func TestRenderChannelManualDisabled(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:        system.NotificationKindChannelAvailability,
		Manual:      true,
		ChannelID:   26,
		ChannelName: "智谱",
	})
	if !strings.Contains(subject, "手动关闭") {
		t.Fatalf("标题不符：%s", subject)
	}
	if !strings.Contains(body, "退出路由直到重新启用") {
		t.Fatalf("正文不符：%s", body)
	}
}

// 渠道内一个可用模型都不剩时升级为渠道级措辞。
func TestRenderChannelNoAvailableModel(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindChannelAvailability,
		ChannelID:           26,
		ChannelName:         "智谱",
		AvailableModelCount: 0,
	})
	if !strings.Contains(subject, "渠道已无可用模型") {
		t.Fatalf("标题不符：%s", subject)
	}
	if !strings.Contains(body, "当前可用模型：0 个") {
		t.Fatalf("正文不符：%s", body)
	}
}

func TestRenderChannelRecovered(t *testing.T) {
	subject, _ := renderNotification(system.AutoDisableNotification{
		Kind:        system.NotificationKindChannelAvailability,
		Recovered:   true,
		ChannelID:   26,
		ChannelName: "智谱",
	})
	if !strings.Contains(subject, "渠道已恢复") {
		t.Fatalf("标题不符：%s", subject)
	}
}

// 原有的自动禁用通知行为不变：未指定 Kind 时仍按 CredentialID / ModelID 生成标题。
func TestRenderLegacyAutoDisableUnchanged(t *testing.T) {
	subject, _ := renderNotification(system.AutoDisableNotification{
		ChannelID:   26,
		ChannelName: "智谱",
		ModelID:     8,
		ModelName:   "glm-5.3-flash",
		Source:      system.AutoDisableSourceModelTest,
	})
	if !strings.Contains(subject, "渠道模型已自动禁用") {
		t.Fatalf("原有通知行为被破坏：%s", subject)
	}
}
