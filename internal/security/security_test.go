package security

import (
	"mime"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignedLinks(t *testing.T) {
	now := time.Unix(1700000000, 0)
	key := strings.Repeat("k", 64)
	hash := strings.Repeat("a", 32)
	link := Link("https://test.invalid/", key, 12, hash, now.Add(time.Hour).Unix())
	u, _ := url.Parse(link)
	q := u.Query()
	if !Verify(key, 12, hash, q.Get("expires"), q.Get("sig"), now, 2*time.Hour) {
		t.Fatal("valid link rejected")
	}
	for _, tc := range []struct {
		id             int
		hash, exp, sig string
	}{
		{13, hash, q.Get("expires"), q.Get("sig")}, {12, strings.Repeat("b", 32), q.Get("expires"), q.Get("sig")},
		{12, hash, "1700000000", q.Get("sig")}, {12, hash, "1709999999", q.Get("sig")}, {12, hash, q.Get("expires"), ""},
	} {
		if Verify(key, tc.id, tc.hash, tc.exp, tc.sig, now, 2*time.Hour) {
			t.Fatal("tampered link accepted")
		}
	}
	if Verify("short", 12, hash, q.Get("expires"), q.Get("sig"), now, 2*time.Hour) {
		t.Fatal("short secret accepted")
	}
	if Verify(key, 12, hash, q.Get("expires"), q.Get("sig"), now.Add(time.Hour), 2*time.Hour) {
		t.Fatal("expired accepted")
	}
}
func TestRanges(t *testing.T) {
	for _, tc := range []struct {
		h    string
		s, e int64
	}{{"", 0, 99}, {"bytes=0-0", 0, 0}, {"bytes=25-", 25, 99}, {"bytes=-20", 80, 99}, {"bytes=0-999", 0, 99}, {"bytes=-999", 0, 99}} {
		s, e, err := Range(tc.h, 100)
		if err != nil || s != tc.s || e != tc.e {
			t.Fatalf("%s: %d %d %v", tc.h, s, e, err)
		}
	}
	for _, h := range []string{"bytes=", "bytes=-0", "bytes=100-", "bytes=25-10", "bytes=0-1,2-3", "bytes=-1-2", "bytes=+1-2", "bytes=0--1", "items=0-1", "bytes=99999999999999999999-"} {
		if _, _, err := Range(h, 100); err == nil {
			t.Fatal(h)
		}
	}
}
func TestFilename(t *testing.T) {
	v := Disposition("../../evil\r\nX-Injected: yes\".mp4", false)
	if strings.ContainsAny(v, "\r\n") {
		t.Fatal(v)
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil || strings.Contains(params["filename"], "/") {
		t.Fatal(v, err)
	}
}
func TestLimiter(t *testing.T) {
	l := NewLimiter(60, 2, 2)
	now := time.Unix(1700000000, 0)
	if !l.Allow("a", now) || !l.Allow("a", now) || l.Allow("a", now) {
		t.Fatal("burst")
	}
	if !l.Allow("a", now.Add(time.Second)) {
		t.Fatal("refill")
	}
	if !l.Allow("b", now) || l.Allow("c", now) {
		t.Fatal("unbounded map")
	}
	if !l.Allow("c", now.Add(11*time.Minute)) {
		t.Fatal("cleanup")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Allow("c", now.Add(11*time.Minute)) }()
	}
	wg.Wait()
}
