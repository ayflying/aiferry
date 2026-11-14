package relay

// developer 角色兼容层（过渡实现，纯函数、无副作用）。
//
// 【背景】developer 是 OpenAI 后续引入的消息角色，语义上等价于 system，用来承载
// 优先级更高的指令。新版客户端（DSH、Codex 等）已经改用 developer 代替 system，
// 但国内多数上游的 /chat/completions 兼容层仍只接受
// system / user / assistant / tool 四种角色。
//
// 生产实证（2026-11-14 22:23，afreq_8d9dc997518d6e75f21dc5cf）：请求首条消息为
// {"role":"developer",...}，打到智谱 glm-5.3-flash（渠道 26）后 180ms 即返回
// HTTP 400，body 为 {"code":"1214","message":"角色信息不正确"}；同一份请求体打其它
// 渠道正常，说明差异只在上游是否认识这个角色名，与模型能力、网关转发链路无关。
//
// 【做法】网关在所有 /chat/completions 上游请求上把 developer 改写成 system。
// 只改角色名，content、多模态 parts、工具字段一律原样保留；developer 与 system
// 在 OpenAI 语义中就是同一条指令通道，改写后模型读到的指令内容完全不变。
//
// 【为什么是全局而不是按渠道开关】这是上游兼容性缺口，不是某家渠道的特性。按渠道
// 开关意味着每接入一家不认 developer 的上游都要补一次配置，且新渠道默认踩坑；全局
// 归一让所有渠道对新客户端一致可用，符合网关"抹平上游差异"的职责。
//
// 【何时删除】一旦国内主流上游（智谱、Kimi、通义、豆包等）的 /chat/completions
// 都接受 developer 角色，这一层就变成多余的历史包袱，应当整体删除，不要保留开关或
// 空实现：删掉本文件的 normalizeDeveloperRole 调用点（prepareRequestBody 里的一行）
// 与本文件即可，不涉及其它模块。
// 判断标准：拿 {"role":"developer"} 的首条消息直打各上游，全部返回 200 即可删。
//
// 注意：workbuddy 渠道另有一套自身的 developer 归一（internal/logic/channel 的
// ApplyWorkBuddyBody），那是该渠道风控适配的一部分，与本层互不影响；本层先执行，
// 改写成 system 后 workbuddy 的归一逻辑看到的已经是 system，行为不变。

const (
	// roleDeveloper 是 OpenAI 引入的高优先级指令角色名。
	roleDeveloper = "developer"
	// roleSystem 是所有 /chat/completions 上游都接受的标准指令角色名。
	roleSystem = "system"
)

// normalizeDeveloperRole 把 messages 里所有 developer 角色就地改写成 system。
//
// 返回改写发生的次数，0 表示请求里没有 developer，调用方可以据此跳过重新编码。
// 非 /chat/completions 端点或 messages 结构不符合预期时原样返回 0，不做任何猜测。
func normalizeDeveloperRole(endpoint string, payload map[string]any) int {
	if endpoint != "/chat/completions" {
		return 0
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return 0
	}
	changed := 0
	for _, value := range messages {
		message, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if role, ok := message["role"].(string); ok && role == roleDeveloper {
			message["role"] = roleSystem
			changed++
		}
	}
	return changed
}
