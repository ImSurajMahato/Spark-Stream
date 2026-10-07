package routes

import (
	"EverythingSuckz/fsb/config"
	"EverythingSuckz/fsb/internal/bot"
	"EverythingSuckz/fsb/internal/security"
	"EverythingSuckz/fsb/internal/stream"
	"EverythingSuckz/fsb/internal/utils"
	"context"
	"crypto/hmac"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var log *zap.Logger
var limiter *security.Limiter
var slots chan struct{}

func (e *allRoutes) LoadHome(r *Route) {
	log = e.log.Named("Stream")
	limiter = security.NewLimiter(config.ValueOf.RequestsPerMinute, config.ValueOf.RequestBurst, 4096)
	slots = make(chan struct{}, config.ValueOf.MaxActiveStreams)
	r.Engine.GET("/stream/:messageID", getStreamRoute)
	r.Engine.HEAD("/stream/:messageID", getStreamRoute)
	// Optional trusted backend link minting. This endpoint never goes in a browser/student app.
	if config.ValueOf.MintAPIKey != "" {
		r.Engine.POST("/api/link/:messageID", mintLink)
	}
}
func rateOK(c *gin.Context) bool {
	if !limiter.Allow(c.ClientIP(), time.Now()) {
		c.Header("Retry-After", "5")
		c.AbortWithStatus(http.StatusTooManyRequests)
		return false
	}
	return true
}
func mintLink(c *gin.Context) {
	if !rateOK(c) {
		return
	}
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	key := strings.TrimPrefix(auth, "Bearer ")
	if !hmac.Equal([]byte(key), []byte(config.ValueOf.MintAPIKey)) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(c.Param("messageID"))
	if err != nil || id <= 0 {
		c.AbortWithStatus(400)
		return
	}
	requestCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	worker := bot.GetNextWorker()
	metadataCtx, metadataCancel := context.WithTimeout(requestCtx, 30*time.Second)
	file, err := utils.FileFromMessage(metadataCtx, worker.Client, id)
	metadataCancel()
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	hash := utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID)
	expiry := time.Now().Add(time.Duration(config.ValueOf.LinkTTLSeconds) * time.Second).Unix()
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"url": security.Link(config.ValueOf.Host, config.ValueOf.SigningSecret, id, hash, expiry), "expires": expiry})
}
func getStreamRoute(c *gin.Context) {
	if !rateOK(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("messageID"))
	if err != nil || id <= 0 {
		c.AbortWithStatus(400)
		return
	}
	hash := c.Query("hash")
	if !security.Verify(config.ValueOf.SigningSecret, id, hash, c.Query("expires"), c.Query("sig"), time.Now(), time.Duration(config.ValueOf.LinkTTLSeconds)*time.Second) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	// Authentication before all Telegram operations; global admission bounds stream RAM.
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		c.Header("Retry-After", "5")
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	requestCtx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Hour)
	defer cancel()
	worker := bot.GetNextWorker()
	metadataCtx, metadataCancel := context.WithTimeout(requestCtx, 30*time.Second)
	file, err := utils.FileFromMessage(metadataCtx, worker.Client, id)
	metadataCancel()
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	expected := utils.PackFile(file.FileName, file.FileSize, file.MimeType, file.ID)
	if !hmac.Equal([]byte(hash), []byte(expected)) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	// Do not let uploaded HTML or SVG execute as your streaming site's origin.
	download := c.Query("d") == "true"
	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !strings.HasPrefix(mimeType, "video/") && !strings.HasPrefix(mimeType, "audio/") && mimeType != "application/pdf" && mimeType != "image/jpeg" && mimeType != "image/png" {
		download = true
	}
	c.Header("Content-Disposition", security.Disposition(file.FileName, download))
	c.Header("Content-Type", mimeType)
	if file.FileSize == 0 {
		if c.Request.Method == "HEAD" {
			c.Status(http.StatusOK)
			return
		}
		res, err := worker.Client.API().UploadGetFile(requestCtx, &tg.UploadGetFileRequest{Location: file.Location, Offset: 0, Limit: 1024 * 1024})
		if err != nil {
			c.AbortWithStatus(502)
			return
		}
		result, ok := res.(*tg.UploadFile)
		if !ok {
			c.AbortWithStatus(502)
			return
		}
		c.Data(200, mimeType, result.Bytes)
		return
	}
	start, end, err := security.Range(c.GetHeader("Range"), file.FileSize)
	if err != nil {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", file.FileSize))
		c.AbortWithStatus(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	length := end - start + 1
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	status := http.StatusOK
	if c.GetHeader("Range") != "" {
		status = http.StatusPartialContent
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.FileSize))
	}
	if c.Request.Method == "HEAD" {
		c.Status(status)
		return
	}
	pipe, err := stream.NewStreamPipe(requestCtx, worker.Client, file.Location, start, end, log)
	if err != nil {
		c.AbortWithStatus(502)
		return
	}
	defer pipe.Close()
	// Fetch a small first slice before committing headers, so startup failures return 502.
	first := make([]byte, min(int64(32*1024), length))
	n, err := io.ReadFull(pipe, first)
	if err != nil {
		log.Warn("stream startup failed", zap.Int("messageID", id))
		c.Header("Content-Length", "")
		c.AbortWithStatus(502)
		return
	}
	c.Status(status)
	if _, err = c.Writer.Write(first[:n]); err != nil {
		return
	}
	c.Writer.Flush()
	if _, err = io.CopyN(c.Writer, pipe, length-int64(n)); err != nil && requestCtx.Err() == nil && !utils.IsClientDisconnectError(err) {
		log.Warn("stream interrupted", zap.Int("messageID", id))
	}
}
