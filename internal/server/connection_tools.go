package server

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// The runner's connection tool source wiring (add-connection-http tasks 2.1/
// 3.3): the connections service implements the credential seam directly, and
// this adapter maps its domain-typed attachment listing onto the runner's
// narrow AttachedConnectionLister seam — the same interface-first composition
// as RuntimeCredentialSource, kept OUT of internal/services because services
// cannot import internal/agents (the runner consumes these interfaces
// structurally without importing services either).

// connectionListerAdapter adapts *services.ConnectionsService to the runner's
// agents.AttachedConnectionLister: the service resolves the agent's attached
// http-kind connections (resolve-as-server-first-else-connection per contract
// §2); the adapter re-shapes each row into the runner's ref, re-resolving the
// registered recipe the verb generation needs (nil recipes degrade
// skip-and-mark inside the source).
type connectionListerAdapter struct {
	connections *services.ConnectionsService
}

// AttachedHTTPConnections implements agents.AttachedConnectionLister.
func (a connectionListerAdapter) AttachedHTTPConnections(ctx context.Context, workspaceID, agentID string) ([]agents.HTTPConnectionRef, error) {
	connections, err := a.connections.AttachedHTTPConnections(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	refs := make([]agents.HTTPConnectionRef, 0, len(connections))
	for i := range connections {
		refs = append(refs, agents.HTTPConnectionRef{
			ConnectionID: connections[i].ID,
			Service:      connections[i].Service,
			Status:       connections[i].Status,
			Recipe:       domain.RecipeByID(connections[i].Service),
		})
	}
	return refs, nil
}

// NewConnectionToolSource builds the runner's connection tool source over the
// connections service, which implements both seams structurally: the
// attachment listing rides the adapter above (domain → runner ref re-shaping),
// the credential resolution is the service's own CredentialForConnection
// (agents.ConnectionCredentials, contract §3). Shared by the composition root
// and the router's fallback assembly — one wiring shape.
func NewConnectionToolSource(connections *services.ConnectionsService) *agents.ConnectionToolSource {
	return agents.NewConnectionToolSource(connectionListerAdapter{connections: connections}, connections)
}
