package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// userHandlers handles self-profile updates and avatar uploads.
type userHandlers struct {
	users   store.UserStore
	storage storage.Storage
}

// NewUserHandlers creates a new userHandlers instance with injected dependencies.
func NewUserHandlers(users store.UserStore, strg storage.Storage) *userHandlers {
	return &userHandlers{
		users:   users,
		storage: strg,
	}
}

// PatchMeRequest holds editable self-profile fields.
type PatchMeRequest struct {
	Name        *string         `json:"name"`
	Avatar      json.RawMessage `json:"avatar"`
	AvatarKey   *string         `json:"avatar_key"`
	AvatarURL   *string         `json:"avatar_url"`
	ClearAvatar *bool           `json:"clear_avatar"`
}

// PatchMe updates the authenticated user's profile (name and avatar clearing).
func (h *userHandlers) PatchMe(c *gin.Context) {
	user := MustCurrentUser(c)

	var req PatchMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Avatar == nil && req.AvatarKey == nil && req.AvatarURL == nil && req.ClearAvatar == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalid))
			return
		}
		user.Name = trimmed
	}

	shouldClearAvatar := (req.ClearAvatar != nil && *req.ClearAvatar) ||
		(len(req.Avatar) > 0 && (string(req.Avatar) == "null" || string(req.Avatar) == `""`))

	if shouldClearAvatar {
		user.AvatarKey = nil
		user.AvatarURL = nil
	} else {
		if req.AvatarKey != nil {
			if *req.AvatarKey == "" {
				user.AvatarKey = nil
			} else {
				user.AvatarKey = req.AvatarKey
			}
		}
		if req.AvatarURL != nil {
			if *req.AvatarURL == "" {
				user.AvatarURL = nil
			} else {
				user.AvatarURL = req.AvatarURL
			}
		}
	}

	if err := h.users.Update(c.Request.Context(), user); err != nil {
		RespondError(c, err)
		return
	}

	if h.storage != nil && user.AvatarKey != nil && *user.AvatarKey != "" {
		url := h.storage.URL(*user.AvatarKey)
		user.AvatarURL = &url
	}

	RespondOK(c, gin.H{"user": user})
}

const maxAvatarBytes = 2 * 1024 * 1024 // 2MB

// UploadAvatar handles avatar image uploads with magic-byte content sniffing and atomic capability key storage.
func (h *userHandlers) UploadAvatar(c *gin.Context) {
	user := MustCurrentUser(c)
	if h == nil || h.storage == nil {
		RespondError(c, fmt.Errorf("%w: storage service unavailable", domain.ErrInvalid))
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		fileHeader, err = c.FormFile("avatar")
	}

	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "avatar file exceeds 2MB limit")
			return
		}
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "missing avatar file in form-data ('file' or 'avatar')")
		return
	}

	if fileHeader.Size > maxAvatarBytes {
		AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "avatar file exceeds 2MB limit")
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		RespondError(c, fmt.Errorf("%w: failed to open avatar file", domain.ErrInvalid))
		return
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxAvatarBytes+1))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "avatar file exceeds 2MB limit")
			return
		}
		RespondError(c, err)
		return
	}

	if len(data) > maxAvatarBytes {
		AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "avatar file exceeds 2MB limit")
		return
	}

	if len(data) == 0 {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "avatar file is empty")
		return
	}

	// Sniff content type from magic bytes
	contentType := http.DetectContentType(data)
	if contentType != "image/png" && contentType != "image/jpeg" && contentType != "image/webp" {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid avatar image type; only PNG, JPEG, and WebP are allowed")
		return
	}

	key, err := storage.NewKey()
	if err != nil {
		RespondError(c, err)
		return
	}

	if err := h.storage.Put(c.Request.Context(), key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		RespondError(c, err)
		return
	}

	prevKey := user.AvatarKey
	user.AvatarKey = &key
	avatarURL := h.storage.URL(key)
	user.AvatarURL = &avatarURL

	if err := h.users.Update(c.Request.Context(), user); err != nil {
		// Best-effort cleanup of stored file on DB failure
		_ = h.storage.Delete(c.Request.Context(), key)
		RespondError(c, err)
		return
	}

	// Clean up previous avatar if it was stored
	if prevKey != nil && *prevKey != "" && *prevKey != key {
		_ = h.storage.Delete(c.Request.Context(), *prevKey)
	}

	RespondOK(c, gin.H{
		"avatar_url": avatarURL,
		"user":       user,
	})
}
