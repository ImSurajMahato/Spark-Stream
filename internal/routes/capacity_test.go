package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := slots
	defer func() { slots = old }()
	slots = make(chan struct{}, 2)
	r := gin.New()
	r.GET("/capacity", capacityRoute)
	get := func() string {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/capacity", nil))
		if w.Code != http.StatusOK {
			t.Fatal(w.Code)
		}
		return w.Body.String()
	}
	if b := get(); !contains(b, `"active_streams":0`) || !contains(b, `"max_streams":2`) || !contains(b, `"accepting":true`) {
		t.Fatal(b)
	}
	slots <- struct{}{}
	slots <- struct{}{}
	if b := get(); !contains(b, `"active_streams":2`) || !contains(b, `"accepting":false`) {
		t.Fatal(b)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
