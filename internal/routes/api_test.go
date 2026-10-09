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

func apiCall(t *testing.T, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.ValueOf.MintAPIKey = strings.Repeat("k", 40)
	limiter = security.NewLimiter(600, 100, 16)
	r := gin.New()
	r.GET("/api/stream", streamAPI)
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestStreamAPIAuth(t *testing.T) {
	w := apiCall(t, "/api/stream?videoId=abc", nil)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	w = apiCall(t, "/api/stream?videoId=abc", map[string]string{"X-API-Key": "wrong"})
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestStreamAPIBadID(t *testing.T) {
	h := map[string]string{"Authorization": "Bearer " + strings.Repeat("k", 40)}
	for _, p := range []string{"/api/stream", "/api/stream?videoId=a/b", "/api/stream?videoId=" + strings.Repeat("a", 65)} {
		w := apiCall(t, p, h)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s got %d", p, w.Code)
		}
	}
}

func TestStreamAPINoCatalog(t *testing.T) {
	w := apiCall(t, "/api/stream?videoId=abc", map[string]string{"X-API-Key": strings.Repeat("k", 40)})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
}
