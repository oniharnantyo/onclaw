package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// storageConfigHandlers serves the workspace blob-storage configuration API
// (attachments design D16): GET returns the masked view (the secret never
// leaves the server — only a last-4 hint), PUT validates, probe-gates s3
// saves, and seals the secret at rest. The default when no row exists is the
// instance's local storage.
type storageConfigHandlers struct {
	configs store.WorkspaceStorageStore
	encKey  []byte
	// probe verifies bucket connectivity for s3 saves; injected so tests stub
	// it and the composition root passes internal/storage/s3.Probe.
	probe func(context.Context, storage.StorageConfig) error
}

// NewStorageConfigHandlers creates a new storageConfigHandlers instance with
// injected dependencies.
func NewStorageConfigHandlers(configs store.WorkspaceStorageStore, encKey []byte, probe func(context.Context, storage.StorageConfig) error) *storageConfigHandlers {
	return &storageConfigHandlers{
		configs: configs,
		encKey:  encKey,
		probe:   probe,
	}
}

// GetStorage responds with the workspace's storage configuration view: the
// local default when unconfigured, the stored fields with a masked secret
// hint otherwise.
func (h *storageConfigHandlers) GetStorage(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	cfg, err := h.configs.Get(c.Request.Context(), ws.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			RespondOK(c, storageConfigView(nil, ""))
			return
		}
		RespondError(c, err)
		return
	}

	RespondOK(c, storageConfigView(cfg, h.secretHint(ws.ID, cfg.SecretAccessKey)))
}

// storageConfigBody is the shared PUT/probe request shape: one struct for both
// handlers so validation and the keep-stored merge stay identical.
type storageConfigBody struct {
	Driver          string `json:"driver"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	UsePathStyle    bool   `json:"use_path_style"`
}

// s3Resolved is the validated outcome of an s3 request body: the probe-ready
// storage config, the resolved plaintext secret, and the keep-stored envelope
// the save path carries over ("" when a replacement secret needs sealing).
type s3Resolved struct {
	probeCfg   storage.StorageConfig
	plaintext  string
	keepStored bool
	envelope   string
}

// resolveS3 validates an s3 request against the stored row and resolves the
// keep-stored secret merge exactly as the save path does. It writes the error
// response itself and returns nil when the request is rejected.
func (h *storageConfigHandlers) resolveS3(c *gin.Context, ws *domain.Workspace, req storageConfigBody) *s3Resolved {
	stored, err := h.storedConfig(c, ws.ID)
	if err != nil {
		RespondError(c, err)
		return nil
	}

	var missing []string
	for field, value := range map[string]string{
		"endpoint":      req.Endpoint,
		"region":        req.Region,
		"bucket":        req.Bucket,
		"access_key_id": req.AccessKeyID,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	hint := h.secretHint(ws.ID, storedSecretEnvelope(stored))
	if req.SecretAccessKey == "" && stored == nil {
		missing = append(missing, "secret_access_key")
	}
	if len(missing) > 0 {
		RespondError(c, fmt.Errorf("%w: missing required s3 fields: %s", domain.ErrInvalid, strings.Join(missing, ", ")))
		return nil
	}

	// Keep-stored merge (the hooks idiom): empty or hint-echoed secret
	// keeps the stored envelope; anything else is a replacement.
	keepStored := req.SecretAccessKey == "" || req.SecretAccessKey == hint
	plaintext := req.SecretAccessKey
	envelope := ""
	if keepStored && stored != nil {
		decrypted, err := secrets.Decrypt(h.encKey, []byte(ws.ID), stored.SecretAccessKey)
		if err != nil {
			RespondError(c, fmt.Errorf("workspace %s: failed to unseal stored storage secret: %w", ws.ID, err))
			return nil
		}
		plaintext = string(decrypted)
		envelope = stored.SecretAccessKey
	}

	return &s3Resolved{
		probeCfg: storage.StorageConfig{
			Driver:       "s3",
			Endpoint:     req.Endpoint,
			Region:       req.Region,
			Bucket:       req.Bucket,
			AccessKey:    req.AccessKeyID,
			SecretKey:    plaintext,
			UsePathStyle: req.UsePathStyle,
		},
		plaintext:  plaintext,
		keepStored: keepStored,
		envelope:   envelope,
	}
}

// PutStorage saves the workspace's storage configuration. Local is
// probe-free and wipes any stored s3 fields; s3 requires its fields, resolves
// the secret through the keep-stored merge, and only persists after the
// connectivity probe succeeds — a probe failure is a 422 carrying the probe's
// reason, with the previous configuration untouched (spec scenario).
func (h *storageConfigHandlers) PutStorage(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req storageConfigBody
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, fmt.Errorf("%w: request body: %v", domain.ErrInvalid, err))
		return
	}

	switch req.Driver {
	case "local":
		// The switch to Local is probe-free and clears the s3 fields (D16):
		// the row is replaced wholesale by the zero-config driver choice.
		cfg := &domain.WorkspaceStorageConfig{WorkspaceID: ws.ID, Driver: "local"}
		if err := h.configs.Upsert(c.Request.Context(), cfg); err != nil {
			RespondError(c, err)
			return
		}
		RespondOK(c, storageConfigView(nil, ""))
		return

	case "s3":
		res := h.resolveS3(c, ws, req)
		if res == nil {
			return
		}

		if err := h.probe(c.Request.Context(), res.probeCfg); err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, err.Error())
			return
		}

		envelope := res.envelope
		if !res.keepStored {
			sealed, err := secrets.Encrypt(h.encKey, []byte(ws.ID), []byte(res.plaintext))
			if err != nil {
				RespondError(c, fmt.Errorf("failed to seal storage secret: %w", err))
				return
			}
			envelope = sealed
		}

		cfg := &domain.WorkspaceStorageConfig{
			WorkspaceID:     ws.ID,
			Driver:          "s3",
			Endpoint:        req.Endpoint,
			Region:          req.Region,
			Bucket:          req.Bucket,
			AccessKeyID:     req.AccessKeyID,
			SecretAccessKey: envelope,
			UsePathStyle:    req.UsePathStyle,
		}
		if err := h.configs.Upsert(c.Request.Context(), cfg); err != nil {
			RespondError(c, err)
			return
		}
		RespondOK(c, storageConfigView(cfg, h.secretHint(ws.ID, envelope)))
		return

	default:
		RespondError(c, fmt.Errorf("%w: unsupported storage driver %q (use \"local\" or \"s3\")", domain.ErrInvalid, req.Driver))
	}
}

// ProbeStorage verifies bucket connectivity for the submitted configuration
// without persisting anything (the settings pane's Test connection). Local is
// probe-free and always succeeds; s3 is validated exactly like a save —
// including the keep-stored secret merge — and probed; a probe failure is a
// 422 carrying the probe's reason verbatim.
func (h *storageConfigHandlers) ProbeStorage(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req storageConfigBody
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, fmt.Errorf("%w: request body: %v", domain.ErrInvalid, err))
		return
	}

	switch req.Driver {
	case "local":
		RespondOK(c, gin.H{"ok": true})
		return

	case "s3":
		res := h.resolveS3(c, ws, req)
		if res == nil {
			return
		}
		if err := h.probe(c.Request.Context(), res.probeCfg); err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, err.Error())
			return
		}
		RespondOK(c, gin.H{"ok": true})
		return

	default:
		RespondError(c, fmt.Errorf("%w: unsupported storage driver %q (use \"local\" or \"s3\")", domain.ErrInvalid, req.Driver))
	}
}

// storedConfig fetches the current row, collapsing absent to nil.
func (h *storageConfigHandlers) storedConfig(c *gin.Context, workspaceID string) (*domain.WorkspaceStorageConfig, error) {
	stored, err := h.configs.Get(c.Request.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return stored, nil
}

// storedSecretEnvelope returns the stored row's sealed envelope, if any.
func storedSecretEnvelope(stored *domain.WorkspaceStorageConfig) string {
	if stored == nil {
		return ""
	}
	return stored.SecretAccessKey
}

// secretHint derives the client-safe last-4 hint from a sealed envelope
// (the hookSecretHint idiom): decrypt with the workspace id as AAD, take the
// tail; undecryptable envelopes hint as empty rather than leaking.
func (h *storageConfigHandlers) secretHint(workspaceID, envelope string) string {
	if envelope == "" {
		return ""
	}
	plaintext, err := secrets.Decrypt(h.encKey, []byte(workspaceID), envelope)
	if err != nil {
		return ""
	}
	value := string(plaintext)
	if len(value) > 4 {
		return value[len(value)-4:]
	}
	return value
}

// storageConfigView renders the client-facing masked shape. A nil config is
// the workspace-generic local default — s3 fields and updated_at omitted.
func storageConfigView(cfg *domain.WorkspaceStorageConfig, secretHint string) gin.H {
	if cfg == nil {
		return gin.H{"driver": "local"}
	}
	return gin.H{
		"driver":         cfg.Driver,
		"endpoint":       cfg.Endpoint,
		"region":         cfg.Region,
		"bucket":         cfg.Bucket,
		"access_key_id":  cfg.AccessKeyID,
		"use_path_style": cfg.UsePathStyle,
		"secret_hint":    secretHint,
		"updated_at":     cfg.UpdatedAt,
	}
}
