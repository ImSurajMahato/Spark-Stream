// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package routes

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/catalog"
	"EverythingSuckz/fsb/internal/security"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// LoadAPI registers GET /api/stream?videoId=<slug>. It is only enabled when MINT_API_KEY is set.
// The key is for your website BACKEND. Never put it in browser JavaScript.
func (e *allRoutes) LoadAPI(r *Route) {
	if config.ValueOf.MintAPIKey == "" {
		return
	}
	r.Engine.GET("/api/stream", streamAPI)
}

func apiError(c *gin.Context, status int, code, msg string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{"error": code, "message": msg})
}

func apiKeyFrom(c *gin.Context) string {
	if k := strings.TrimSpace(c.GetHeader("X-API-Key")); k != "" {
		return k
	}
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func keyOK(supplied, expected string) bool {
	a := sha256.Sum256([]byte(supplied))
	b := sha256.Sum256([]byte(expected))
	return hmac.Equal(a[:], b[:])
}

func streamAPI(c *gin.Context) {
	if limiter != nil && !limiter.Allow(c.ClientIP(), time.Now()) {
		c.Header("Retry-After", "5")
		apiError(c, http.StatusTooManyRequests, "rate_limited", "Too many requests, retry shortly.")
		return
	}
	if !keyOK(apiKeyFrom(c), config.ValueOf.MintAPIKey) {
		apiError(c, http.StatusUnauthorized, "unauthorized", "Missing or invalid API key. Send X-API-Key or Authorization: Bearer.")
		return
	}
	slug := c.Query("videoId")
	if !catalog.ValidSlug(slug) {
		apiError(c, http.StatusBadRequest, "bad_video_id", "videoId must be 1-64 characters: letters, digits, _ or -.")
		return
	}
	if catalog.Default == nil {
		apiError(c, http.StatusServiceUnavailable, "catalog_unavailable", "Catalog database is not configured.")
		return
	}
	entry, err := catalog.Default.Get(c.Request.Context(), slug)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			apiError(c, http.StatusNotFound, "not_found", "No video with this videoId.")
			return
		}
		apiError(c, http.StatusServiceUnavailable, "catalog_error", "Could not read the catalog, try again.")
		return
	}
	expires := time.Now().Add(time.Duration(config.ValueOf.LinkTTLSeconds) * time.Second)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"videoId":    slug,
		"url":        security.Link(config.ValueOf.Host, config.ValueOf.SigningSecret, entry.MessageID, entry.Hash, expires.Unix()),
		"expires_at": expires.UTC().Format(time.RFC3339),
		"expires":    expires.Unix(),
	})
}
