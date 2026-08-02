package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// DirectRelayCallbackBodyLimit leaves room for JSON framing around the
// protocol's one-megabyte raw upstream error-body allowance.
func DirectRelayCallbackBodyLimit() gin.HandlerFunc {
	const maxBytes int64 = model.DirectRelayMaxErrorBodyBytes*2 + 128*1024
	return func(c *gin.Context) {
		if c.Request.Body == nil {
			c.Next()
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes+1))
		_ = c.Request.Body.Close()
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if int64(len(body)) > maxBytes {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Next()
	}
}
