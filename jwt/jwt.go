// Package jwt provides HS256 JWT signing for MyMind API authentication.
package jwt

import (
	"encoding/base64"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"
)

// Sign generates an HS256-signed JWT for MyMind API requests.
// The token is bound to a specific path and HTTP method.
func Sign(kid, secretB64, path, method string) string {
	secret, err := base64.URLEncoding.DecodeString(secretB64)
	if err != nil {
		// Try standard base64
		secret, err = base64.StdEncoding.DecodeString(secretB64)
		if err != nil {
			// Fall back to raw secret as bytes
			secret = []byte(secretB64)
		}
	}

	now := int(time.Now().Unix())
	payload := map[string]interface{}{
		"path":   path,
		"method": strings.ToUpper(method),
		"iat":    now,
		"exp":    now + 300, // 5 minutes
	}
	header := map[string]interface{}{
		"alg": "HS256",
		"typ": "JWT",
		"kid": kid,
	}

	b64 := func(data map[string]interface{}) string {
		jsonData, _ := json.Marshal(data)
		return base64.URLEncoding.EncodeToString(jsonData)
	}

	headerB64 := b64(header)
	payloadB64 := b64(payload)
	message := headerB64 + "." + payloadB64

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(message))
	sig := h.Sum(nil)
	sigB64 := base64.URLEncoding.EncodeToString(sig)

	// Strip padding
	sigB64 = strings.TrimRight(sigB64, "=")

	return message + "." + sigB64
}