// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

func Signature(secret string, id int, hash string, expires int64) string {
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "spark-stream:v1:%d:%s:%d", id, hash, expires)
	return hex.EncodeToString(m.Sum(nil))
}

func Verify(secret string, id int, hash, expiry, sig string, now time.Time, maxTTL time.Duration) bool {
	exp, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || id <= 0 || len(hash) != 32 || len(sig) != 64 || len(secret) < 32 {
		return false
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return false
	}
	if exp <= now.Unix() || exp > now.Add(maxTTL).Unix() {
		return false
	}
	supplied, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	expected, _ := hex.DecodeString(Signature(secret, id, hash, exp))
	return hmac.Equal(supplied, expected)
}

func Link(host, secret string, id int, hash string, exp int64) string {
	q := url.Values{"hash": {hash}, "expires": {strconv.FormatInt(exp, 10)}, "sig": {Signature(secret, id, hash, exp)}}
	return fmt.Sprintf("%s/stream/%d?%s", strings.TrimRight(host, "/"), id, q.Encode())
}

func Disposition(filename string, download bool) string {
	name := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		name = "lecture.mp4"
	}
	kind := "inline"
	if download {
		kind = "attachment"
	}
	return mime.FormatMediaType(kind, map[string]string{"filename": name})
}

// Single HTTP range only. Reject malformed, multi-range, and unsatisfiable input.
func Range(header string, size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, errors.New("empty file")
	}
	if header == "" {
		return 0, size - 1, nil
	}
	if !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, 0, errors.New("single bytes range required")
	}
	parts := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("bad range")
	}
	parse := func(s string) (int64, error) {
		if s == "" {
			return 0, errors.New("empty")
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0, errors.New("not decimal")
			}
		}
		return strconv.ParseInt(s, 10, 64)
	}
	if parts[0] == "" {
		suffix, err := parse(parts[1])
		if err != nil || suffix <= 0 {
			return 0, 0, errors.New("bad suffix")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, err := parse(parts[0])
	if err != nil || start >= size {
		return 0, 0, errors.New("bad start")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = parse(parts[1])
		if err != nil || end < start {
			return 0, 0, errors.New("bad end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}

type bucket struct {
	tokens  float64
	updated time.Time
}
type Limiter struct {
	mu         sync.Mutex
	entries    map[string]bucket
	perSecond  float64
	burst      float64
	maxEntries int
}

func NewLimiter(perMinute, burst, maxEntries int) *Limiter {
	return &Limiter{entries: make(map[string]bucket), perSecond: float64(perMinute) / 60, burst: float64(burst), maxEntries: maxEntries}
}
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= l.maxEntries {
			for k, old := range l.entries {
				if now.Sub(old.updated) > 10*time.Minute {
					delete(l.entries, k)
				}
			}
			if len(l.entries) >= l.maxEntries {
				return false
			}
		}
		b = bucket{tokens: l.burst, updated: now}
	}
	b.tokens += now.Sub(b.updated).Seconds() * l.perSecond
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.updated = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.entries[key] = b
	return allowed
}
