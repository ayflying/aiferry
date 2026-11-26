package channel

import (
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// The list only projects identity/project metadata; it never refreshes tokens
// or calls upstream. A disabled credential can still be explicitly relogged.
func (s *sChannel) populateCredentialLoginView(view *CredentialView, channel entity.Channels, adapter string, row credentialRow) {
	switch adapter {
	case channeltype.AdapterLoginGeminiOAuth:
		cfg, err := readGeminiOAuthConfig()
		if err != nil {
			return
		}
		credential, err := decodeGeminiCredential(cfg, row)
		if err == nil {
			view.ProjectID = geminiCredentialProject(credential, channel.ProjectId)
			view.CanRelogin = view.ProjectID != ""
		}
	case channeltype.AdapterLoginCodexDevice:
		_, err := s.decodeCodexLoginTarget(row)
		view.CanRelogin = err == nil
	}
}
