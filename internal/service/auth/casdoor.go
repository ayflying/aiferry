package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

func (s *Service) exchangeCode(ctx context.Context, code, callbackURL string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     s.app.Config.CasdoorClientID,
		"client_secret": s.app.Config.CasdoorClientSecret,
		"code":          code,
		"redirect_uri":  callbackURL,
	})
	if err != nil {
		return "", gerror.Wrap(err, "encode Casdoor token request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.app.Config.CasdoorEndpoint+"/api/login/oauth/access_token", bytes.NewReader(payload))
	if err != nil {
		return "", gerror.Wrap(err, "create Casdoor token request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.app.HTTP.Do(req)
	if err != nil {
		return "", gerror.Wrap(err, "request Casdoor access token")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAuthBodySize))
	if err != nil {
		return "", gerror.Wrap(err, "read Casdoor token response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure tokenErrorResponse
		if json.Unmarshal(body, &failure) == nil && strings.TrimSpace(failure.Error) != "" {
			return "", gerror.Newf("Casdoor token exchange failed: %s", strings.TrimSpace(failure.Error))
		}
		return "", gerror.Newf("Casdoor token exchange failed with HTTP %d", response.StatusCode)
	}
	var token tokenResponse
	if err = json.Unmarshal(body, &token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", gerror.New("Casdoor token response is invalid")
	}
	return token.AccessToken, nil
}

func (s *Service) getAccount(ctx context.Context, accessToken string) (casdoorAccount, error) {
	endpoint, err := url.Parse(s.app.Config.CasdoorEndpoint + "/api/get-account")
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "parse Casdoor account endpoint")
	}
	query := endpoint.Query()
	query.Set("access_token", accessToken)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "create Casdoor account request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := s.app.HTTP.Do(req)
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "request Casdoor account")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAuthBodySize))
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "read Casdoor account response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return casdoorAccount{}, gerror.Newf("Casdoor account request failed with HTTP %d", response.StatusCode)
	}
	var envelope accountEnvelope
	if err = json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		if envelope.Status != "" && !strings.EqualFold(envelope.Status, "ok") {
			return casdoorAccount{}, gerror.New("Casdoor account request was rejected")
		}
		body = envelope.Data
	}
	var account casdoorAccount
	if err = json.Unmarshal(body, &account); err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "decode Casdoor account")
	}
	if strings.TrimSpace(account.Uid) == "" && strings.TrimSpace(account.Id) == "" {
		return casdoorAccount{}, gerror.New("Casdoor account has no uid or id")
	}
	return account, nil
}
