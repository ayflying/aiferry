package channel

import "github.com/yunloli/aiferry/internal/logic/channeltype"

// populateCredentialLoginView 仅判断凭据能否显式重新登录；不会调用上游。
func (s *sChannel) populateCredentialLoginView(view *CredentialView, adapter string, row credentialRow) {
	if adapter == channeltype.AdapterLoginCodexDevice {
		_, err := s.decodeCodexLoginTarget(row)
		view.CanRelogin = err == nil
	}
}
