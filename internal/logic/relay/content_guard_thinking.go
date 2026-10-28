package relay

import "strings"

// 思考块开头的识别与拆分。
//
// 上游有两种「思考写进正文」的写法，都按同一套规则处理——搬进 reasoning_content、
// 剥掉标签，正文只留真正的回答：
//
//	1. <thinking>…</thinking>：模型把思考过程当正文写出（adesk 渠道 deepseek 实证）；
//	2. <analysis>…</analysis>：模型跑偏时按「先梳理、再摘要」的草稿格式输出，
//	   后面往往紧跟 <summary>…</summary> 摘要信封（adesk 渠道 afreq_fba9d1d6 实证，
//	   2026-10-28）。客户端自己的上下文压缩用的是 <compacted-summary>，与此无关，
//	   所以这段草稿不是任何客户端的合法协议内容。
//
// 两个块的结束标签不同，因此 opener 与 closer 必须成对识别。

const (
	// leakCloser 是 <thinking> 系的结束标签；上游连结束标签都没给时，整段正文都算思考。
	leakCloser = "</thinking>"
	// analysisOpener/analysisCloser 是「梳理 + 摘要」草稿块的边界。
	analysisOpener = "<analysis>"
	analysisCloser = "</analysis>"
)

// leakOpeners 是观察到的思考块开头写法。只认这几种，避免误伤 "<" 开头的普通文本。
var leakOpeners = []string{
	"<thinking>", "<thinking ", "<thinking:", "<thinkingState:",
	analysisOpener, "<analysis ",
}

// leakCloserFor 返回某个思考块开头对应的结束标签。
func leakCloserFor(opener string) string {
	if strings.HasPrefix(opener, "<analysis") {
		return analysisCloser
	}
	return leakCloser
}

// stripLeakOpener 判断文本是否以完整思考块开头，返回去掉开头后的剩余文本与命中的开头写法。
func stripLeakOpener(text string) (rest string, opener string, ok bool) {
	for _, candidate := range leakOpeners {
		if strings.HasPrefix(text, candidate) {
			return text[len(candidate):], candidate, true
		}
	}
	return text, "", false
}

// isLeakOpenerPrefix 判断文本是否仍是某个思考块开头的**前缀**（含完整开头）。
func isLeakOpenerPrefix(text string) bool {
	for _, opener := range leakOpeners {
		if strings.HasPrefix(opener, text) {
			return true
		}
	}
	return false
}

// splitLeakedThinking 把一段以思考块开头的文本拆成思考内容与正文。
// content 是结束标签之后的正文（已去掉前导空白）；closer 命中的结束标签，
// 调用方用它判断思考块是否在这段文本内闭合。第四个返回值表示这段文本确实以思考块开头。
func splitLeakedThinking(text string) (thinking, content, closer string, ok bool) {
	rest, opener, ok := stripLeakOpener(text)
	if !ok {
		return "", "", "", false
	}
	closer = leakCloserFor(opener)
	if at := strings.Index(rest, closer); at >= 0 {
		return rest[:at], strings.TrimLeft(rest[at+len(closer):], " \t\r\n"), closer, true
	}
	return rest, "", closer, true
}
