package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSiteConfigIsPublicAndContainsOnlyPublicValues(t *testing.T) {
	config := SiteConfig{TermsURL: "https://example.com/terms", PrivacyURL: "https://example.com/privacy", SupportURL: "https://example.com/support", AbuseEmail: "abuse@example.com", SecurityEmail: "security@example.com"}
	router := NewRouter(nil, nil, RouterOptions{APIToken: "a-secret-token-that-is-at-least-32-characters", SiteConfig: config})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/site/config", nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(config.TermsURL)) || bytes.Contains(response.Body.Bytes(), []byte("a-secret-token")) {
		t.Fatalf("unexpected site config response: %d %s", response.Code, response.Body.String())
	}
}

func TestSiteConfigReturnsNotFoundWhenIncomplete(t *testing.T) {
	router := NewRouter(nil, nil, RouterOptions{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/site/config", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unexpected site config status: %d %s", response.Code, response.Body.String())
	}
}
