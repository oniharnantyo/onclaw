package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// errInvalidToolKey builds the 400 error for a tool key that is not in the
// catalog.
func errInvalidToolKey(key string) error {
	return fmt.Errorf("%w: unknown tool %q", domain.ErrInvalid, key)
}

// ToolSettingsResponse is one tool's catalog metadata merged with the
// workspace's setting state. Secret config values are replaced by hints —
// ciphertext never crosses the HTTP boundary. Toggleable is false exactly
// for the catalog's always-on tools, whose enabled state the workspace
// cannot change.
type ToolSettingsResponse struct {
	Key          string               `json:"key"`
	DisplayName  string               `json:"display_name"`
	Description  string               `json:"description"`
	Group        string               `json:"group"`
	IconKey      string               `json:"icon_key"`
	Configurable bool                 `json:"configurable"`
	ConfigSchema []agents.ConfigField `json:"config_schema,omitempty"`
	Toggleable   bool                 `json:"toggleable"`
	Enabled      bool                 `json:"enabled"`
	Configured   bool                 `json:"configured"`
	Config       map[string]any       `json:"config"`
}

// toolSettingsHandlers handles the workspace tool settings endpoints.
type toolSettingsHandlers struct {
	settings *agents.ToolSettingsService
}

// NewToolSettingsHandlers creates a new toolSettingsHandlers instance.
func NewToolSettingsHandlers(settings *agents.ToolSettingsService) *toolSettingsHandlers {
	return &toolSettingsHandlers{settings: settings}
}

// ListTools returns the tool catalog merged with the workspace's settings.
func (h *toolSettingsHandlers) ListTools(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	views, err := h.settings.ViewForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]ToolSettingsResponse, 0)
	for _, entry := range agents.ToolCatalog() {
		view := views[entry.Key]
		items = append(items, ToolSettingsResponse{
			Key:          entry.Key,
			DisplayName:  entry.DisplayName,
			Description:  entry.Description,
			Group:        entry.Group,
			IconKey:      entry.IconKey,
			Configurable: entry.Configurable,
			ConfigSchema: entry.ConfigSchema,
			Toggleable:   !entry.AlwaysOn,
			Enabled:      view.Enabled,
			Configured:   view.Configured,
			Config:       view.Config,
		})
	}

	RespondOK(c, gin.H{"tools": items})
}

// PatchToolRequest holds the upsertable fields of a workspace tool setting.
type PatchToolRequest struct {
	Enabled *bool          `json:"enabled,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
}

// PatchTool upserts one tool's workspace setting: the global enable toggle
// and/or structured config values.
func (h *toolSettingsHandlers) PatchTool(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	key := c.Param("key")

	entry, known := agents.ToolCatalogEntryByKey(key)
	if !known {
		RespondError(c, errInvalidToolKey(key))
		return
	}

	var req PatchToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if req.Enabled == nil && req.Config == nil {
		RespondError(c, errInvalidToolKey(key))
		return
	}
	// Always-on tools are not toggleable: an enabled patch is rejected, not
	// ignored, so the API never pretends the write landed
	// (always-on-channel-tools D4). Config-only patches keep today's
	// behavior — the upsert stores the enabled=true row.
	if req.Enabled != nil && entry.AlwaysOn {
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			"channel tools are always active and cannot be disabled")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	setting := domain.WorkspaceToolSetting{
		WorkspaceID: ws.ID,
		ToolKey:     key,
		Enabled:     enabled,
		Config:      req.Config,
	}
	if err := h.settings.Upsert(c.Request.Context(), &setting); err != nil {
		var cfgErr *agents.ConfigValidationError
		if errors.As(err, &cfgErr) {
			details := make([]ErrorDetail, 0, len(cfgErr.Errors))
			for _, fe := range cfgErr.Errors {
				details = append(details, ErrorDetail{Field: fe.Field, Message: fe.Message})
			}
			AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, cfgErr.Error(), details...)
			return
		}
		RespondError(c, err)
		return
	}

	// Re-read through the view so secrets come back as hints.
	views, err := h.settings.ViewForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	view := views[key]
	RespondOK(c, gin.H{"tool": ToolSettingsResponse{
		Key:          entry.Key,
		DisplayName:  entry.DisplayName,
		Description:  entry.Description,
		Group:        entry.Group,
		IconKey:      entry.IconKey,
		Configurable: entry.Configurable,
		ConfigSchema: entry.ConfigSchema,
		Toggleable:   !entry.AlwaysOn,
		Enabled:      view.Enabled,
		Configured:   view.Configured,
		Config:       view.Config,
	}})
}
