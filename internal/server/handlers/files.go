package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

// fileHandlers handles capability file serving.
type fileHandlers struct {
	storage storage.Storage
}

// NewFileHandlers creates a new fileHandlers instance with injected dependencies.
func NewFileHandlers(strg storage.Storage) *fileHandlers {
	return &fileHandlers{
		storage: strg,
	}
}

// ServeFile serves stored capability files with immutable caching headers.
func (h *fileHandlers) ServeFile(c *gin.Context) {
	if h == nil || h.storage == nil {
		AbortNotFound(c, "file not found")
		return
	}

	key := c.Param("key")
	if key == "" {
		key = c.Param("name")
	}
	if key == "" {
		AbortNotFound(c, "file not found")
		return
	}

	file, err := h.storage.Open(c.Request.Context(), key)
	if err != nil {
		AbortNotFound(c, "file not found")
		return
	}
	defer file.Close()

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Type", file.ContentType())

	http.ServeContent(c.Writer, c.Request, key, time.Time{}, file)
}
