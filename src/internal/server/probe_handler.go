package server

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const maxEchoBytes = 1 << 30 // 1 GiB hard cap

func echoHandler(c *gin.Context) {
	raw := c.Query("bytes")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required query parameter: bytes"})
		return
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || n > maxEchoBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bytes must be an integer in [0, 1073741824]"})
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(n, 10))
	c.Status(http.StatusOK)
	if n == 0 {
		return
	}
	const chunk = 64 * 1024
	remaining := n
	for remaining > 0 {
		write := int64(chunk)
		if remaining < write {
			write = remaining
		}
		if _, err := io.CopyN(c.Writer, &zeroReader{}, write); err != nil {
			return
		}
		remaining -= write
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
