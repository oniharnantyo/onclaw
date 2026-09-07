// Package skills implements the workspace skill library: the install
// pipeline (author / upload / git-URL / fork sources), install-time
// dependency inference and resolution, and per-workspace dependency
// provisioning (python venv first; further kinds via the provisioner
// registry).
//
// ADAPTER NOTE: the persistence port below (Store) and the entity (Skill)
// are local mirrors of the shapes landing in internal/store
// (store.WorkspaceSkillStore) and internal/domain (domain.WorkspaceSkill).
// They live here only because those packages are being edited in parallel;
// when they land, the composition root passes the real store — either
// directly (once the local port is retargeted at the domain entity in a
// follow-up change) or through a thin field-mapping adapter. Nothing in
// this package writes to internal/store.
package skills

import (
	"context"
	"time"
)

// Source identifies how a workspace skill entered the registry.
type Source string

const (
	SourceAuthored Source = "authored"
	SourceUpload   Source = "upload"
	SourceGit      Source = "git"
	SourceFork     Source = "fork"
)

// ValidSource reports whether s is a known install source.
func ValidSource(s Source) bool {
	switch s {
	case SourceAuthored, SourceUpload, SourceGit, SourceFork:
		return true
	}
	return false
}

// DependencyStatus is the per-dependency resolution state stored on the
// registry row and re-rendered by reports without re-parsing skill files.
type DependencyStatus string

const (
	// DepMet means the dependency is satisfied right now.
	DepMet DependencyStatus = "met"
	// DepMissing means the dependency cannot be satisfied on this server
	// (binary absent from PATH, tool not allowed by the gate/allowlists).
	DepMissing DependencyStatus = "missing"
	// DepUnprovisioned means the dependency is declared but not yet
	// provisioned (python package pending venv install).
	DepUnprovisioned DependencyStatus = "unprovisioned"
)

// ToolDependency is a tool-name requirement resolved against the workspace
// tool gate and agent allowlists. The reserved shell tool is "execute".
type ToolDependency struct {
	Name   string           `json:"name"`
	Status DependencyStatus `json:"status"`
}

// BinaryDependency is an external binary requirement probed with
// exec.LookPath; never auto-installed.
type BinaryDependency struct {
	Name        string           `json:"name"`
	Status      DependencyStatus `json:"status"`
	InstallHint string           `json:"install_hint,omitempty"`
}

// PythonDependency is a pip requirement line provisioned into the shared
// per-workspace venv.
type PythonDependency struct {
	Requirement string           `json:"requirement"`
	Status      DependencyStatus `json:"status"`
}

// DependencySet is the resolved dependency state stored on the registry row
// (JSONB column) — spec: workspace-skills, "Skill dependency resolution".
type DependencySet struct {
	Tools    []ToolDependency   `json:"tools,omitempty"`
	Binaries []BinaryDependency `json:"binaries,omitempty"`
	Python   []PythonDependency `json:"python,omitempty"`
}

// Empty reports whether the set carries no dependency of any kind.
func (d DependencySet) Empty() bool {
	return len(d.Tools) == 0 && len(d.Binaries) == 0 && len(d.Python) == 0
}

// PythonRequirements returns the python requirement lines in set order.
func (d DependencySet) PythonRequirements() []string {
	reqs := make([]string, 0, len(d.Python))
	for _, dep := range d.Python {
		reqs = append(reqs, dep.Requirement)
	}
	return reqs
}

// Skill is one workspace-skill registry row. Bodies live on disk at
// <ONCLAW_DIR>/workspaces/<tenant_slug>/skills/<name>/.
type Skill struct {
	WorkspaceID  string         `json:"workspace_id"`
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name,omitempty"`
	Description  string         `json:"description"`
	Version      string         `json:"version"`
	Source       Source         `json:"source"`
	Enabled      bool           `json:"enabled"`
	Dependencies DependencySet  `json:"dependencies"`
	InstalledAt  time.Time      `json:"installed_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Store is the narrow persistence port for the workspace skill registry.
// It mirrors the planned store.WorkspaceSkillStore (tasks.md 1.3); see the
// adapter note in the package comment.
type Store interface {
	Create(ctx context.Context, skill *Skill) error
	Get(ctx context.Context, workspaceID, name string) (*Skill, error)
	List(ctx context.Context, workspaceID string) ([]Skill, error)
	ListEnabled(ctx context.Context, workspaceID string) ([]Skill, error)
	SetEnabled(ctx context.Context, workspaceID, name string, enabled bool) error
	Update(ctx context.Context, skill *Skill) error
	Delete(ctx context.Context, workspaceID, name string) error
}
