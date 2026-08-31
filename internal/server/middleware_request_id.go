package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// HeaderRequestID is the HTTP header name for request correlation.
const HeaderRequestID = "X-Request-ID"

// RequestIDMiddleware issues or propagates a request ID for each HTTP request.
// If the inbound X-Request-ID header is well-formed (non-empty, <=64 chars, [A-Za-z0-9-]),
// it is reused. Otherwise, a random 16-character hex string is generated via crypto/rand.
// The request ID is stored in the Gin context and set on the X-Request-ID response header.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader(HeaderRequestID)
		if !isValidRequestID(reqID) {
			reqID = generateRequestID()
		}

		c.Set(RequestIDContextKey, reqID)
		c.Header(HeaderRequestID, reqID)
		c.Next()
	}
}

// isValidRequestID checks whether the inbound request ID is well-formed:
// non-empty, <= 64 characters, containing only [A-Za-z0-9-].
func isValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

// generateRequestID generates a 16-character lowercase hex string using crypto/rand.
func generateRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
