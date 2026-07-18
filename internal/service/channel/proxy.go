package channel

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	appservice "github.com/yunloli/aiferry/internal/service/app"
)

func (s *Service) encryptProxyURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if _, err := appservice.NewSOCKS5HTTPClient(s.app.HTTP, value); err != nil {
		return "", err
	}
	cipher, err := s.app.Secrets.Encrypt(value)
	return cipher, gerror.Wrap(err, "encrypt channel proxy URL")
}

func (s *Service) HTTPClientForProxy(proxyURLCipher string) (*http.Client, error) {
	if proxyURLCipher == "" {
		return s.app.HTTP, nil
	}
	proxyURL, err := s.app.Secrets.Decrypt(proxyURLCipher)
	if err != nil {
		return nil, gerror.Wrap(err, "decrypt channel proxy URL")
	}
	return appservice.NewSOCKS5HTTPClient(s.app.HTTP, proxyURL)
}
