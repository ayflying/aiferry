package channel

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Route CONNECT requests for the pinned Google hosts to a local TLS fixture only.
func geminiPickerProxyFixture(t *testing.T) (*http.Client, string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var exchanges, searches atomic.Int32
	google := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Host + r.URL.Path {
		case "oauth2.googleapis.com/token":
			attempt := exchanges.Add(1)
			if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.Form.Get("code") != "fixture-code" || r.Form.Get("code_verifier") == "" {
				t.Error("expected the pinned authorization-code exchange with PKCE")
			}
			scope := geminiOAuthScope
			if attempt == 1 {
				scope += " " + geminiProjectReadOnlyScope
			}
			_, _ = fmt.Fprintf(w, `{"access_token":"picker-fixture-access","refresh_token":"picker-fixture-refresh","token_type":"Bearer","expires_in":3600,"scope":%q}`, scope)
		case "openidconnect.googleapis.com/v1/userinfo":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer picker-fixture-access" {
				t.Error("expected pinned bearer userinfo GET")
			}
			_, _ = io.WriteString(w, `{"sub":"picker-account"}`)
		case "cloudresourcemanager.googleapis.com/v3/projects:search":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer picker-fixture-access" {
				t.Error("expected pinned bearer project search GET")
			}
			if searches.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"projects":[{"projectId":"listed-project","displayName":"Listed","state":"ACTIVE"}],"nextPageToken":"test-cursor"}`)
			} else {
				http.Redirect(w, r, "https://untrusted.invalid/", http.StatusTemporaryRedirect)
			}
		default:
			t.Error("unexpected request to mock Google server")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(google.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "oauth2.googleapis.com:443", "openidconnect.googleapis.com:443", "cloudresourcemanager.googleapis.com:443":
		default:
			t.Errorf("attempt to proxy to unexpected host %s", r.Host)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method != http.MethodConnect {
			t.Error("Google requests must go through CONNECT")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", google.Listener.Addr().String())
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := rw.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, rw)
			_ = upstream.(*net.TCPConn).CloseWrite()
			close(done)
		}()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	}))
	t.Cleanup(proxy.Close)
	client := google.Client()
	// httptest trusts only its own cert. Verify it against the loopback fixture name
	// rather than the Google hostname being forwarded by the local proxy.
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.ServerName = strings.Split(google.Listener.Addr().String(), ":")[0]
	transport.DisableKeepAlives = true
	return client, proxy.URL, &exchanges, &searches
}
