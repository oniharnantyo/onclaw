package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListAgents handles listing of all agent profiles.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListAgents(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// CreateAgent handles creating a new agent profile.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var input service.AgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Agent name is required")
		return
	}

	a, err := h.svc.CreateAgent(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, a)
}

// GetAgent handles retrieving a single agent profile.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	resp, err := h.svc.GetAgent(ctx, name)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// UpdateAgent handles updating an existing agent profile.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var input service.AgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	a, err := h.svc.UpdateAgent(ctx, name, input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, a)
}

// DeleteAgent handles removing an agent profile.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	if err := h.svc.DeleteAgent(ctx, name); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetAgentMCP handles GET /api/agents/{name}/mcp.
func (h *Handler) GetAgentMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.PathValue("name")

	resp, err := h.svc.ListAgentMCP(ctx, agentName)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// SetAgentMCP handles PUT /api/agents/{name}/mcp.
func (h *Handler) SetAgentMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.PathValue("name")

	var input struct {
		ServerName string `json:"server_name"`
		Enabled    bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.SetAgentMCP(ctx, agentName, input.ServerName, input.Enabled); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// SetAgentTools handles PUT /api/agents/{name}/tools.
func (h *Handler) SetAgentTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.PathValue("name")

	var input struct {
		Tool    string `json:"tool"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Tool == "" {
		httpx.Error(w, http.StatusBadRequest, "Tool name must not be empty")
		return
	}

	if err := h.svc.SetAgentTools(ctx, agentName, input.Tool, input.Enabled); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// GetAgentPersona handles GET /api/agents/{name}/persona/{file}.
func (h *Handler) GetAgentPersona(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	file := r.PathValue("file")

	resp, err := h.svc.GetAgentPersona(ctx, name, file)
	if err != nil {
		h.handleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(resp))
}

// SetAgentPersona handles PUT /api/agents/{name}/persona/{file}.
func (h *Handler) SetAgentPersona(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	file := r.PathValue("file")

	var input struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.SetAgentPersona(ctx, name, file, input.Content); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
