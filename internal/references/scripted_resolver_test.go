package references_test

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
)

// scriptedResolver is the fake workspace-storage resolver the service tests
// script (route-reference-documents-through-workspace-storage D5): two
// in-memory drivers — the instance-default local fake and a second "s3"
// fake — with each workspace's configured driver held in a map the tests
// mutate to simulate a Storage settings-pane switch. No S3, no network: the
// resolver contract (ForWorkspace / ForBackend / DriverName) is scripted
// directly, mirroring resolver.WorkspaceStorage's semantics — unconfigured
// workspaces resolve the instance default named "local" (D15/D16), and
// recorded-backend reads always dispatch on the name the row carries, never
// on the current configuration.
type scriptedResolver struct {
	local  storage.Storage
	s3     storage.Storage
	driver map[string]string // workspaceID -> configured driver name; "local" when absent
}

// newScriptedResolver builds the double over the given instance-default
// driver; the second "s3" driver is a fresh in-memory fake.
func newScriptedResolver(local storage.Storage) *scriptedResolver {
	return &scriptedResolver{
		local:  local,
		s3:     storagefake.New(),
		driver: make(map[string]string),
	}
}

// configure scripts the workspace's Storage settings-pane choice: the driver
// NEW writes land on and the name NEW rows record.
func (r *scriptedResolver) configure(workspaceID, driver string) {
	r.driver[workspaceID] = driver
}

func (r *scriptedResolver) configured(workspaceID string) string {
	if d, ok := r.driver[workspaceID]; ok {
		return d
	}
	return "local"
}

// ForWorkspace resolves the configured driver new writes go through;
// unconfigured workspaces fall back to the instance default.
func (r *scriptedResolver) ForWorkspace(_ context.Context, workspaceID string) (storage.Storage, error) {
	switch d := r.configured(workspaceID); d {
	case "local":
		return r.local, nil
	case "s3":
		return r.s3, nil
	default:
		return nil, fmt.Errorf("%w: workspace %s configures unknown storage driver %q", domain.ErrInvalid, workspaceID, d)
	}
}

// DriverName names the backend new rows record — the workspace's configured
// driver, "local" when unconfigured (the recorded-row semantics D2 keeps).
func (r *scriptedResolver) DriverName(_ context.Context, workspaceID string) (string, error) {
	return r.configured(workspaceID), nil
}

// ForBackend resolves the driver holding an existing blob's bytes by the
// backend recorded on its row — never the current configuration.
func (r *scriptedResolver) ForBackend(_ context.Context, workspaceID, backend string) (storage.Storage, error) {
	switch backend {
	case "local":
		return r.local, nil
	case "s3":
		return r.s3, nil
	default:
		return nil, fmt.Errorf("%w: workspace %s has no driver for recorded backend %q", domain.ErrInvalid, workspaceID, backend)
	}
}
