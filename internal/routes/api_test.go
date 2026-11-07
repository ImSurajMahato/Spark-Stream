package routes

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/security"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func apiCall(t *testing.T, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	limiter = security.NewLimiter(600, 100, 16)
	r := gin.New()
	if config.ValueOf.APIAuth {
		r.POST("/api/stream", streamAPI)
		r.GET("/api/stream", func(c *gin.Context) {
			apiError(c, http.StatusMethodNotAllowed, "method_not_allowed", "API_AUTH=true: use POST /api/stream with X-API-Key.")
		})
	} else {
		r.GET("/api/stream", streamAPI)
	}
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// authMode sets API_AUTH=true with a 40-char test key for the duration of a test.
func authMode(t *testing.T) {
	t.Helper()
	config.ValueOf.APIAuth = true
	config.ValueOf.MintAPIKey = strings.Repeat("k", 40)
	t.Cleanup(func() {
		config.ValueOf.APIAuth = false
		config.ValueOf.MintAPIKey = ""
	})
}

func TestStreamAPIAuth(t *testing.T) {
	authMode(t)
	w := apiCall(t, "POST", "/api/stream?videoId=abc", nil)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	w = apiCall(t, "POST", "/api/stream?videoId=abc", map[string]string{"X-API-Key": "wrong"})
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestStreamAPIAuthBlocksGET(t *testing.T) {
	authMode(t)
	w := apiCall(t, "GET", "/api/stream?videoId=abc", map[string]string{"X-API-Key": strings.Repeat("k", 40)})
	if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Body.String(), "method_not_allowed") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestStreamAPIBadID(t *testing.T) {
	authMode(t)
	h := map[string]string{"Authorization": "Bearer " + strings.Repeat("k", 40)}
	for _, p := range []string{"/api/stream", "/api/stream?videoId=a/b", "/api/stream?videoId=" + strings.Repeat("a", 65)} {
		w := apiCall(t, "POST", p, h)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s got %d", p, w.Code)
		}
	}
}

func TestStreamAPINoCatalog(t *testing.T) {
	authMode(t)
	w := apiCall(t, "POST", "/api/stream?videoId=abc", map[string]string{"X-API-Key": strings.Repeat("k", 40)})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
}

func TestStreamAPIPublicMode(t *testing.T) {
	config.ValueOf.APIAuth = false
	config.ValueOf.MintAPIKey = ""
	// No key at all: bad ID is still rejected.
	if w := apiCall(t, "GET", "/api/stream", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id got %d", w.Code)
	}
	// No key at all: valid slug passes auth and reaches the catalog check.
	w := apiCall(t, "GET", "/api/stream?videoId=abc", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}
