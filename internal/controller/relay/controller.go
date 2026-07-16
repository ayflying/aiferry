package relay

import (
	"io"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/service/apikey"
	relaysvc "github.com/yunloli/aiferry/internal/service/relay"
)

type Controller struct {
	apiKeys *apikey.Service
	relay   *relaysvc.Service
}

func New(apiKeySvc *apikey.Service, relaySvc *relaysvc.Service) *Controller {
	return &Controller{apiKeys: apiKeySvc, relay: relaySvc}
}

func (c *Controller) Register(group *ghttp.RouterGroup) {
	group.GET("/models", c.models)
	group.POST("/chat/completions", c.proxy("/chat/completions"))
	group.POST("/responses", c.proxy("/responses"))
	group.POST("/embeddings", c.proxy("/embeddings"))
}

func (c *Controller) models(r *ghttp.Request) {
	if _, ok := c.authenticate(r); !ok {
		return
	}
	data, err := c.relay.Models(r.Context())
	if err != nil {
		writeError(r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.WriteJson(data)
	r.Exit()
}

func (c *Controller) proxy(endpoint string) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		key, ok := c.authenticate(r)
		if !ok {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
		if err != nil {
			writeError(r, http.StatusBadRequest, "invalid_request_error", "Unable to read request body")
			return
		}
		if err = c.relay.Handle(r.Context(), r.Response.RawWriter(), r.Header, endpoint, body, key); err != nil {
			writeError(r, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		r.Exit()
	}
}

func (c *Controller) authenticate(r *ghttp.Request) (apikey.AuthKey, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		writeError(r, http.StatusUnauthorized, "authentication_error", "Missing Bearer API key")
		return apikey.AuthKey{}, false
	}
	key, err := c.apiKeys.Authenticate(r.Context(), strings.TrimSpace(authorization[7:]))
	if err != nil {
		writeError(r, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return apikey.AuthKey{}, false
	}
	return key, true
}

func writeError(r *ghttp.Request, status int, kind, message string) {
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.WriteStatus(status)
	r.Response.WriteJson(map[string]any{"error": map[string]any{"type": kind, "message": message}})
	r.Exit()
}
