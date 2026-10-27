package handler

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/stretchr/testify/require"
)

func encryptRevocationFixture(t *testing.T, text, key string) []byte {
	t.Helper()
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	require.NoError(t, err)
	raw := []byte(text)
	padding := aes.BlockSize - len(raw)%aes.BlockSize
	raw = append(raw, bytes.Repeat([]byte{byte(padding)}, padding)...)
	iv := bytes.Repeat([]byte{7}, aes.BlockSize)
	result := make([]byte, len(raw))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(result, raw)
	body, err := json.Marshal(
		map[string]string{"encrypt": base64.StdEncoding.EncodeToString(append(iv, result...))},
	)
	require.NoError(t, err)
	return body
}

func TestFeishuRevocationEventAuthenticityAndChallenge(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EVENTS_ENABLED", "1")
	t.Setenv("FEISHU_PERSONAL_EVENT_ENCRYPT_KEY", "synthetic-encryption-key")
	t.Setenv("FEISHU_PERSONAL_EVENT_VERIFICATION_TOKEN", "synthetic-verification")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &FeishuPersonalHandler{}
	r.POST("/events", h.RevocationEvent)
	send := func(body []byte, timestamp, signature string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
		req.Header.Set("X-Lark-Request-Timestamp", timestamp)
		req.Header.Set("X-Lark-Request-Nonce", "synthetic-nonce")
		req.Header.Set("X-Lark-Signature", signature)
		r.ServeHTTP(w, req)
		return w.Code
	}
	challenge := encryptRevocationFixture(
		t,
		`{"type":"url_verification","token":"synthetic-verification","challenge":"synthetic-challenge"}`,
		"synthetic-encryption-key",
	)
	require.Equal(t, 200, send(challenge, "", ""))
	bad := encryptRevocationFixture(
		t,
		`{"type":"url_verification","token":"forged","challenge":"synthetic"}`,
		"synthetic-encryption-key",
	)
	require.Equal(t, 401, send(bad, "", ""))
	body := encryptRevocationFixture(
		t,
		`{"schema":"2.0","header":{"event_type":"auth.user_access_token.revoked_v4","token":"forged"},"event":{}}`,
		"synthetic-encryption-key",
	)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	require.Equal(t, 401, send(body, now, "forged"))
	require.Equal(
		t,
		400,
		send(
			body,
			now,
			larkevent.Signature(now, "synthetic-nonce", "synthetic-encryption-key", string(body)),
		),
	)
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	require.Equal(
		t,
		401,
		send(
			body,
			old,
			larkevent.Signature(old, "synthetic-nonce", "synthetic-encryption-key", string(body)),
		),
	)
	require.Equal(t, 401, send([]byte(`{"encrypt":"`+strings.Repeat("?", 20)+`"}`), now, "forged"))
	malformed := encryptRevocationFixture(t, "}x{", "synthetic-encryption-key")
	require.NotPanics(t, func() { require.Equal(t, 401, send(malformed, now, "forged")) })
}
