package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestNewSessionCookieUsesPersistentSecureSlidingAttributes(t *testing.T) {
	const token = "session-token"
	ttl := 7 * 24 * time.Hour
	before := time.Now()

	cookie := newSessionCookie(true, token, ttl)
	if cookie.Name != sessionCookieName || cookie.Value != token {
		t.Fatalf("unexpected cookie identity: %#v", cookie)
	}
	if cookie.MaxAge != int(ttl.Seconds()) {
		t.Fatalf("MaxAge = %d, want %d", cookie.MaxAge, int(ttl.Seconds()))
	}
	if cookie.Expires.Before(before.Add(ttl - time.Second)) || cookie.Expires.After(time.Now().Add(ttl+time.Second)) {
		t.Fatalf("Expires = %s, want approximately %s after now", cookie.Expires, ttl)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("unexpected cookie security attributes: %#v", cookie)
	}
}
