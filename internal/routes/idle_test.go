package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type nopWriter struct{ n int }

func (w *nopWriter) Write(p []byte) (int, error) { w.n += len(p); return len(p), nil }

func TestDeadlineWriterPassesData(t *testing.T) {
	rec := httptest.NewRecorder()
	dw := &deadlineWriter{w: rec, rc: http.NewResponseController(rec)}
	if n, err := dw.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatal(n, err)
	}
	if rec.Body.String() != "abc" {
		t.Fatal(rec.Body.String())
	}
	if streamIdleTimeout < 30*time.Second {
		t.Fatal("idle timeout too short for normal buffering")
	}
}
