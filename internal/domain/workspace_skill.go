package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SkillSource indicates how a workspace skill entered the registry.
type SkillSource string

const (
	SkillSourceAuthored SkillSource = "authored"
	SkillSourceUpload   SkillSource = "upload"
	SkillSourceGit      SkillSource = "git"
	SkillSourceFork     SkillSource = "fork"
)

// DefaultSkillVersion is the version assigned to freshly authored skills.
const DefaultSkillVersion = "0.1.0"

// MaxSkillDescriptionBytes is the maximum allowed size of a skill description.
const MaxSkillDescriptionBytes = 4096

var skillVersionRegex = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// SkillDependencies is the resolved dependency set stored on a workspace skill
// registry row: tool names the skill references, binaries its scripts invoke,
// and python packages its scripts import (declared plus inferred at import).
type SkillDependencies struct {
	Tools    []string `json:"tools,omitempty"`
	Binaries []string `json:"binaries,omitempty"`
	Python   []string `json:"python,omitempty"`
}

// WorkspaceSkill is one row of the workspace skill registry. The multi-file
// skill body lives on disk at <ONCLAW_DIR>/workspaces/<slug>/skills/<name>/;
// the row carries the metadata and the enabled master switch that governs
// whether every agent in the workspace attaches the skill.
type WorkspaceSkill struct {
	ID           string            `json:"id"`
	WorkspaceID  string            `json:"workspace_id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Source       SkillSource       `json:"source"`
	Enabled      bool              `json:"enabled"`
	Dependencies SkillDependencies `json:"dependencies"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ValidateSkillSource validates that source is one of the allowed values
// (authored, upload, git, fork).
func ValidateSkillSource(source SkillSource) error {
	switch source {
	case SkillSourceAuthored, SkillSourceUpload, SkillSourceGit, SkillSourceFork:
		return nil
	default:
		return fmt.Errorf("%w: invalid skill source %q, must be authored, upload, git, or fork", ErrInvalid, source)
	}
}

// ValidateSkillVersion validates that version is a bare semantic version
// string of the shape MAJOR.MINOR.PATCH (e.g. 0.1.0).
func ValidateSkillVersion(version string) error {
	if !skillVersionRegex.MatchString(version) {
		return fmt.Errorf("%w: skill version %q must match MAJOR.MINOR.PATCH (e.g. 0.1.0)", ErrInvalid, version)
	}
	return nil
}

// ValidateSkillName validates that a workspace skill name is a valid DNS label
// and not in the reserved slug list shared with agents and workspaces.
func ValidateSkillName(name string) error {
	return ValidateSlug(name)
}

// ValidateSkillDependencies validates the structural constraints of a skill's
// resolved dependency set: each entry must be a non-blank string with no
// whitespace or path separators.
func ValidateSkillDependencies(deps SkillDependencies) error {
	for _, tool := range deps.Tools {
		if err := validateDependencyEntry("tools", tool); err != nil {
			return err
		}
	}
	for _, binary := range deps.Binaries {
		if err := validateDependencyEntry("binaries", binary); err != nil {
			return err
		}
	}
	for _, pkg := range deps.Python {
		if err := validateDependencyEntry("python", pkg); err != nil {
			return err
		}
	}
	return nil
}

func validateDependencyEntry(kind, entry string) error {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return fmt.Errorf("%w: dependencies.%s entries cannot be empty", ErrInvalid, kind)
	}
	if trimmed != entry || strings.ContainsAny(entry, " \t\r\n/") {
		return fmt.Errorf("%w: dependencies.%s entry %q must not contain whitespace, surrounding blanks, or path separators", ErrInvalid, kind, entry)
	}
	return nil
}

// Validate reports whether the workspace skill is structurally valid: required
// identifiers present, DNS-label name outside the reserved list, valid source
// and version, and a structurally valid dependency set.
func (s *WorkspaceSkill) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if err := ValidateSkillName(s.Name); err != nil {
		return err
	}
	if len(s.Description) > MaxSkillDescriptionBytes {
		return fmt.Errorf("%w: description exceeds maximum size of %d bytes", ErrInvalid, MaxSkillDescriptionBytes)
	}
	if err := ValidateSkillSource(s.Source); err != nil {
		return err
	}
	if err := ValidateSkillVersion(s.Version); err != nil {
		return err
	}
	return ValidateSkillDependencies(s.Dependencies)
}
