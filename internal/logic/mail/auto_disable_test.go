package mail

import (
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/system"
)

func renderNotification(notification system.AutoDisableNotification) (string, string) {
	return renderAutoDisableTransition("AiFerry", notification)
}

// 密钥被自动禁用：标题带渠道名，正文点名具体是哪把密钥。
func TestRenderCredentialDisabled(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindCredentialStatus,
		ChannelID:           26,
		ChannelName:         "智谱",
		CredentialID:        62,
		CredentialKeyPrefix: "sk-ab",
		Source:              system.AutoDisableSourceRelayRequest,
		StatusCode:          401,
		Reason:              "上游返回鉴权失败",
	})
	for _, want := range []string{"上游密钥已禁用", "智谱"} {
		if !strings.Contains(subject, want) {
			t.Fatalf("标题缺少 %q：%s", want, subject)
		}
	}
	for _, want := range []string{"sk-ab", "自动禁用", "转发请求", "401", "上游返回鉴权失败"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
}

// 密钥自动恢复：说明恢复方式并带上当初禁用原因。
func TestRenderCredentialRecovered(t *testing.T) {
	subject, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindCredentialStatus,
		Recovered:           true,
		ChannelID:           26,
		ChannelName:         "智谱",
		CredentialID:        62,
		CredentialKeyPrefix: "sk-ab",
		Source:              system.AutoDisableSourceRelayRequest,
		StatusCode:          429,
		Reason:              "限流",
	})
	if !strings.Contains(subject, "上游密钥已恢复") {
		t.Fatalf("标题不符：%s", subject)
	}
	for _, want := range []string{"自动恢复启用", "上次禁用来源", "429"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
}

// 手动禁用要用手动措辞，不能写成「因上游异常被自动禁用」。
func TestRenderCredentialManualDisabled(t *testing.T) {
	_, body := renderNotification(system.AutoDisableNotification{
		Kind:                system.NotificationKindCredentialStatus,
		Manual:              true,
		ChannelID:           26,
		ChannelName:         "智谱",
		CredentialID:        62,
		CredentialKeyPrefix: "sk-ab",
		Source:              "manual",
	})
	for _, want := range []string{"管理端手动禁用", "管理端手动操作"} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文缺少 %q：%s", want, body)
		}
	}
	if strings.Contains(body, "因上游异常") {
		t.Fatalf("手动禁用不应套用自动禁用措辞：%s", body)
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

func TestRenderMappedModelAvailability(t *testing.T) {
	for _, kind := range []system.NotificationKind{system.NotificationKindModelAvailability, system.NotificationKindChannelAvailability} {
		for _, recovered := range []bool{false, true} {
			_, body := renderNotification(system.AutoDisableNotification{
				Kind: kind, Recovered: recovered, ChannelID: 9, ChannelName: "adesk",
				ModelID: 849, ModelName: "gpt-6.1-sol", UpstreamModelName: "gpt-6.2-sol",
			})
			for _, want := range []string{"对外模型 gpt-6.1-sol", "ID：849", "实际请求上游：gpt-6.2-sol"} {
				if !strings.Contains(body, want) {
					t.Fatalf("正文缺少 %q：%s", want, body)
				}
			}
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

// 未指定 Kind 时仍按 CredentialID / ModelID 生成标题，保证原有自动禁用通知不变。
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
