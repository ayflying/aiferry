package relay

import (
	"net/http"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// applyOpencodeGoHeaders 把转发候选转换为上游客户端身份，交给渠道层补齐
// 上游要求的客户端标识头（OpenCode Go 的 x-opencode-session 等）。
func applyOpencodeGoHeaders(target, incoming http.Header, candidate Candidate, userID uint64) {
	channel.ApplyUpstreamClientHeaders(target, incoming, channel.UpstreamClientIdentity{
		ChannelType:  candidate.ChannelType,
		ChannelID:    candidate.ChannelID,
		CredentialID: candidate.ChannelCredentialID,
		ModelName:    candidate.PublicName,
		UserID:       userID,
		BaseURL:      candidate.BaseURL,
	})
}
