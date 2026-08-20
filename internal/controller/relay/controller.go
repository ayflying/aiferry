package relay

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/logic/apikey"
	relaysvc "github.com/yunloli/aiferry/internal/logic/relay"
	"github.com/yunloli/aiferry/internal/logic/requestfirewall"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/user"
)

type Controller struct {
	apiKeys  *apikey.Service
	relay    *relaysvc.Service
	firewall *requestfirewall.Service
}

func New(apiKeySvc *apikey.Service, relaySvc *relaysvc.Service, firewallSvc *requestfirewall.Service) *Controller {
	return &Controller{apiKeys: apiKeySvc, relay: relaySvc, firewall: firewallSvc}
}

func (c *Controller) Register(group *ghttp.RouterGroup) {
	group.GET("/models", c.models)
	group.POST("/chat/completions", c.proxy("/chat/completions"))
	group.POST("/responses", c.proxy("/responses"))
	group.POST("/embeddings", c.proxy("/embeddings"))
	group.POST("/images/generations", c.proxy("/images/generations"))
	group.POST("/video/generations", c.videoGenerations)
	group.GET("/video/generations/{task_id}", c.videoTask)
}

func (c *Controller) models(r *ghttp.Request) {
	clientRelease, limited := c.admitClient(r)
	if limited {
		return
	}
	defer clientRelease()
	key, ok := c.authenticate(r)
	if !ok {
		return
	}
	keyRelease, limited := c.admitAPIKey(r, key)
	if limited {
		return
	}
	defer keyRelease()
	data, err := c.relay.Models(r.Context(), key)
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
		clientRelease, limited := c.admitClient(r)
		if limited {
			return
		}
		defer clientRelease()
		key, ok := c.authenticate(r)
		if !ok {
			return
		}
		keyRelease, limited := c.admitAPIKey(r, key)
		if limited {
			return
		}
		defer keyRelease()
		body, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
		if err != nil {
			writeError(r, http.StatusBadRequest, "invalid_request_error", "Unable to read request body")
			return
		}
		if err = c.relay.Handle(r.Context(), r.Response.RawWriter(), r.Header, clientIP(r), r.Host, endpoint, body, key); err != nil {
			if relaysvc.IsRetryableAvailabilityError(err) {
				writeRetryableAvailabilityError(r)
				return
			}
			if system.IsImageInputDisabled(err) {
				writeError(r, http.StatusBadRequest, "invalid_request_error", err.Error())
				return
			}
			if system.IsSensitiveWordBlocked(err) {
				writeError(r, http.StatusBadRequest, "sensitive_word_blocked", err.Error())
				return
			}
			if user.IsInsufficientBalance(err) {
				writeError(r, http.StatusPaymentRequired, "insufficient_balance", err.Error())
				return
			}
			writeError(r, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		r.Exit()
	}
}

func (c *Controller) videoGenerations(r *ghttp.Request) {
	c.withAuthenticatedKey(r, func(key apikey.AuthKey) {
		body, err := io.ReadAll(io.LimitReader(r.Body, (64<<20)+1))
		if err != nil {
			writeError(r, http.StatusBadRequest, "invalid_request_error", "Unable to read request body")
			return
		}
		status, response, headers, err := c.relay.CreateVideo(r.Context(), r.Header, body, key)
		if err != nil {
			c.writeRelayError(r, err)
			return
		}
		copyUpstreamResponseHeaders(r.Response.Header(), headers)
		r.Response.Status = status
		r.Response.Write(response)
		r.Exit()
	})
}

func (c *Controller) videoTask(r *ghttp.Request) {
	c.withAuthenticatedKey(r, func(key apikey.AuthKey) {
		status, response, headers, err := c.relay.GetVideoTask(r.Context(), r.Header, r.GetRouter("task_id").String(), key)
		if err != nil {
			c.writeRelayError(r, err)
			return
		}
		copyUpstreamResponseHeaders(r.Response.Header(), headers)
		r.Response.Status = status
		r.Response.Write(response)
		r.Exit()
	})
}

func (c *Controller) withAuthenticatedKey(r *ghttp.Request, handler func(apikey.AuthKey)) {
	clientRelease, limited := c.admitClient(r)
	if limited {
		return
	}
	defer clientRelease()
	key, ok := c.authenticate(r)
	if !ok {
		return
	}
	keyRelease, limited := c.admitAPIKey(r, key)
	if limited {
		return
	}
	defer keyRelease()
	handler(key)
}

func (c *Controller) writeRelayError(r *ghttp.Request, err error) {
	if relaysvc.IsRetryableAvailabilityError(err) {
		writeRetryableAvailabilityError(r)
		return
	}
	if user.IsInsufficientBalance(err) {
		writeError(r, http.StatusPaymentRequired, "insufficient_balance", err.Error())
		return
	}
	writeError(r, http.StatusBadRequest, "invalid_request_error", err.Error())
}

func (c *Controller) admitClient(r *ghttp.Request) (func(), bool) {
	if c.firewall == nil {
		return func() {}, false
	}
	release, rejection := c.firewall.AcquireClient(r.Context(), requestfirewall.RequestInput{ClientIP: clientIP(r)})
	return c.handleFirewallAdmission(r, release, rejection)
}

func (c *Controller) admitAPIKey(r *ghttp.Request, key apikey.AuthKey) (func(), bool) {
	if c.firewall == nil {
		return func() {}, false
	}
	release, rejection := c.firewall.AcquireAPIKey(requestfirewall.RequestInput{APIKeyID: key.Id})
	return c.handleFirewallAdmission(r, release, rejection)
}

func (c *Controller) handleFirewallAdmission(r *ghttp.Request, release func(), rejection *requestfirewall.Rejection) (func(), bool) {
	if rejection == nil {
		return release, false
	}
	r.Response.Header().Set("Retry-After", strconv.Itoa(rejection.RetryAfter))
	writeError(r, http.StatusTooManyRequests, "rate_limit_exceeded", rejection.Message)
	return nil, true
}

func (c *Controller) authenticate(r *ghttp.Request) (apikey.AuthKey, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		writeError(r, http.StatusUnauthorized, "authentication_error", "Missing Bearer API key")
		return apikey.AuthKey{}, false
	}
	key, err := c.apiKeys.Authenticate(r.Context(), strings.TrimSpace(authorization[7:]))
	if err != nil {
		if apikey.IsDailySpendLimitReached(err) {
			writeError(r, http.StatusTooManyRequests, "daily_spend_limit_exceeded", err.Error())
			return apikey.AuthKey{}, false
		}
		writeError(r, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return apikey.AuthKey{}, false
	}
	return key, true
}

func copyUpstreamResponseHeaders(target, source http.Header) {
	for name, values := range source {
		switch strings.ToLower(name) {
		case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "content-length":
			continue
		}
		for _, value := range values {
			target.Add(name, value)
		}
	}
}

func writeRetryableAvailabilityError(r *ghttp.Request) {
	response := relaysvc.RetryableAvailabilityClientError()
	if response.RetryAfter != "" {
		r.Response.Header().Set("Retry-After", response.RetryAfter)
	}
	writeError(r, response.Status, response.Type, response.Message)
}

func writeError(r *ghttp.Request, status int, kind, message string) {
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.Status = status
	r.Response.WriteJson(map[string]any{"error": map[string]any{"type": kind, "message": message}})
	r.Exit()
}
