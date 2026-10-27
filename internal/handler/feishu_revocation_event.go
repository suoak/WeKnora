package handler

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkauth "github.com/larksuite/oapi-sdk-go/v3/service/auth/v4"
)

// RevocationEvent uses the already pinned official SDK cryptography and event
// shape, without its request-body debug logger. It has no JWT requirement: both
// event authenticity and the application/tenant/current grant are checked here.
// Disabled until a real encrypted event channel has been configured.
func (h *FeishuPersonalHandler) RevocationEvent(c *gin.Context) {
	secret, verification := os.Getenv("FEISHU_PERSONAL_EVENT_ENCRYPT_KEY"), os.Getenv("FEISHU_PERSONAL_EVENT_VERIFICATION_TOKEN")
	if os.Getenv("FEISHU_PERSONAL_EVENTS_ENABLED") != "1" || secret == "" || verification == "" {
		c.Status(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 128*1024+1))
	if err != nil || len(body) > 128*1024 {
		c.Status(http.StatusBadRequest)
		return
	}
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Encrypt == "" {
		c.Status(http.StatusUnauthorized)
		return
	}
	plain, err := larkevent.EventDecrypt(envelope.Encrypt, secret)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	var request struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
	}
	if json.Unmarshal(plain, &request) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	// URL verification uses the official encrypted challenge + verification token.
	if request.Type == "url_verification" {
		if subtle.ConstantTimeCompare([]byte(request.Token), []byte(verification)) != 1 {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.JSON(http.StatusOK, gin.H{"challenge": request.Challenge})
		return
	}
	timestamp, nonce, signature := c.GetHeader("X-Lark-Request-Timestamp"), c.GetHeader("X-Lark-Request-Nonce"), c.GetHeader("X-Lark-Signature")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	age := time.Since(time.Unix(seconds, 0))
	if err != nil || nonce == "" || len(nonce) > 256 || age > 10*time.Minute || age < -time.Minute || subtle.ConstantTimeCompare([]byte(signature), []byte(larkevent.Signature(timestamp, nonce, secret, string(body)))) != 1 {
		c.Status(http.StatusUnauthorized)
		return
	}
	var event larkauth.P2UserAccessTokenRevokedV4
	if json.Unmarshal(plain, &event) != nil || event.EventV2Base == nil || event.Header == nil || event.Event == nil || event.Schema != "2.0" || event.Header.EventType != "auth.user_access_token.revoked_v4" || subtle.ConstantTimeCompare([]byte(event.Header.Token), []byte(verification)) != 1 {
		c.Status(http.StatusBadRequest)
		return
	}
	milliseconds, err := strconv.ParseInt(event.Header.CreateTime, 10, 64)
	if err != nil || event.Event.OpenId == nil || event.Event.RevokeTokenType == nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if err = h.svc.HandleRevocationHint(c.Request.Context(), event.Header.EventID, event.Header.AppID, event.Header.TenantKey, *event.Event.OpenId, time.UnixMilli(milliseconds), *event.Event.RevokeTokenType); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "event_not_confirmed"})
		return
	}
	// The durable, deduplicated hint schedules a current-grant probe. Never repeat
	// secrets/provider payloads in the response, audit log or error handler.
	c.JSON(http.StatusOK, gin.H{"received": true})
}
