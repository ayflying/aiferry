package channel

import (
	"context"
	"io"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
)

type upstreamJSONRequest struct {
	Method       string
	Endpoint     string
	AuthType     string
	HeaderName   string
	HeaderPrefix string
	BodyLimit    int64
	RequestError string
	FetchError   string
	ReadError    string
	InvalidError string
	StatusError  func(status int, body []byte) error
}

func (s *Service) fetchUpstreamJSON(ctx context.Context, channel entity.Channels, input upstreamJSONRequest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, input.Method, input.Endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, input.RequestError)
	}
	if err = s.setConfiguredHeaders(ctx, req, channel, input.AuthType, input.HeaderName, input.HeaderPrefix); err != nil {
		return nil, err
	}
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, input.FetchError)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, input.BodyLimit))
	if err != nil {
		return nil, gerror.Wrap(err, input.ReadError)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if input.StatusError != nil {
			return nil, input.StatusError(resp.StatusCode, body)
		}
		return nil, gerror.Newf("upstream request returned HTTP %d", resp.StatusCode)
	}
	if !gjson.ValidBytes(body) {
		return nil, gerror.New(input.InvalidError)
	}
	return body, nil
}
