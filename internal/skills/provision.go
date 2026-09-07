package skills

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Python provisioning tunables (deferred per design.md "Open Questions").
const (
	// PipTimeout bounds each pip invocation (network + build).
	PipTimeout = 10 * time.Minute
	// MaxRequirementsBytes caps a single skill's declared requirements.
	MaxRequirementsBytes = 64 << 10 // 64 KiB
	// PythonBinary is the interpreter probed for on the server PATH.
	PythonBinary = "python3"
)

// SkillRequirements pairs a skill name with its python requirement lines —
// one entry per enabled skill in the union install.
type SkillRequirements struct {
	Skill        string
	Requirements []string
}

// ProvisionRequest asks a provisioner to bring dependencies of its kind to
// a satisfied state for one workspace.
type ProvisionRequest struct {
	WorkspaceID string
	// SkillsRoot is the workspace skills directory
	// (<onClawDir>/workspaces/<slug>/skills); the shared venv lives at
	// SkillsRoot/.venv.
	SkillsRoot string
	// Requirements carries, per enabled skill, the python requirement lines
	// to union-install.
	Requirements []SkillRequirements
}

// CheckRequest asks a provisioner to report current statuses without
// changing anything.
type CheckRequest struct {
	WorkspaceID string
	SkillsRoot  string
	// Requirements is the same union shape as ProvisionRequest.
	Requirements []SkillRequirements
}

// DependencyProvisioner is the per-kind provisioning extension point
// (plugins are first-class: python ships as the first registration, and
// further kinds register beside it without touching this package's core).
type DependencyProvisioner interface {
	// Kind is the dependency kind this provisioner owns ("python").
	Kind() string
	// Check reports per-requirement statuses (met / missing /
	// unprovisioned) without side effects.
	Check(ctx context.Context, req CheckRequest) ([]PythonDependency, error)
	// Provision brings the workspace to a satisfied state for the union of
	// requirements, or fails (naming the conflict) without touching already
	// installed dependencies.
	Provision(ctx context.Context, req ProvisionRequest) ([]PythonDependency, error)
}

var (
	provisionerMu       sync.RWMutex
	provisionerRegistry = map[string]DependencyProvisioner{}
)

// RegisterProvisioner adds a provisioner to the registry, keyed by Kind.
// Registering the same kind twice replaces the prior registration (last
// registration wins, mirroring the other registries in the codebase).
func RegisterProvisioner(p DependencyProvisioner) {
	provisionerMu.Lock()
	defer provisionerMu.Unlock()
	provisionerRegistry[p.Kind()] = p
}

// ProvisionerFor returns the registered provisioner for a kind.
func ProvisionerFor(kind string) (DependencyProvisioner, bool) {
	provisionerMu.RLock()
	defer provisionerMu.RUnlock()
	p, ok := provisionerRegistry[kind]
	return p, ok
}

// ErrDependencyConflict marks a cross-skill version conflict: the step
// fails naming both sides and installed skills are untouched (spec:
// "Cross-skill version conflict named").
type ErrDependencyConflict struct {
	Package string
	Left    SkillRequirements
	Right   SkillRequirements
}

func (e *ErrDependencyConflict) Error() string {
	return fmt.Sprintf("python dependency conflict on %q: %q (%s) vs %q (%s)",
		e.Package, strings.Join(e.Left.Requirements, ", "), e.Left.Skill,
		strings.Join(e.Right.Requirements, ", "), e.Right.Skill)
}

// IsDependencyConflict reports whether err is a cross-skill conflict.
func IsDependencyConflict(err error) bool {
	var conflict *ErrDependencyConflict
	return errors.As(err, &conflict)
}

// PythonProvisioner maintains one shared venv per workspace at
// <SkillsRoot>/.venv, pip-installing the union of all enabled skills'
// requirements (design D5). It never uses a custom index (args are built
// from sanitized requirement lines only) and verifies imports after
// installation.
type PythonProvisioner struct {
	runner CommandRunner
}

// NewPythonProvisioner builds the provisioner from its granular dependency.
func NewPythonProvisioner(runner CommandRunner) *PythonProvisioner {
	return &PythonProvisioner{runner: runner}
}

// Kind implements DependencyProvisioner.
func (p *PythonProvisioner) Kind() string { return "python" }

// VenvDir returns the shared workspace venv path for a skills root.
func VenvDir(skillsRoot string) string { return filepath.Join(skillsRoot, ".venv") }

// VenvInterpreter returns the venv's python binary path.
func VenvInterpreter(skillsRoot string) string {
	return filepath.Join(VenvDir(skillsRoot), "bin", "python")
}

// Check implements DependencyProvisioner: requirements are unprovisioned
// when python3 is absent or the venv does not yet import them, met
// otherwise.
func (p *PythonProvisioner) Check(ctx context.Context, req CheckRequest) ([]PythonDependency, error) {
	return p.probe(ctx, req.SkillsRoot, unionRequirements(req.Requirements))
}

// Provision implements DependencyProvisioner: conflict-check first (fail
// without touching installed skills), then ensure the venv, then one union
// pip install, then per-package import verification.
func (p *PythonProvisioner) Provision(ctx context.Context, req ProvisionRequest) ([]PythonDependency, error) {
	union := unionRequirements(req.Requirements)
	if err := checkConflicts(req.Requirements); err != nil {
		return nil, err
	}
	if _, err := p.runner.LookPath(PythonBinary); err != nil {
		deps := make([]PythonDependency, 0, len(union))
		for _, r := range union {
			deps = append(deps, PythonDependency{Requirement: r, Status: DepUnprovisioned})
		}
		return deps, nil
	}
	venv := VenvDir(req.SkillsRoot)
	interp := VenvInterpreter(req.SkillsRoot)
	if _, err := p.runner.LookPath(interp); err != nil {
		if out, err := p.runPip(ctx, "", req.SkillsRoot, "-m", "venv", venv); err != nil {
			return nil, fmt.Errorf("create venv: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if len(union) > 0 {
		args := append([]string{"-m", "pip", "install", "--no-input", "--disable-pip-version-check", "--timeout", "60"}, union...)
		if out, err := p.runPip(ctx, interp, req.SkillsRoot, args...); err != nil {
			return nil, fmt.Errorf("pip install: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return p.probe(ctx, req.SkillsRoot, union)
}

// runPip runs an interpreter-scoped command under the pip timeout.
func (p *PythonProvisioner) runPip(ctx context.Context, interpreter, dir string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, PipTimeout)
	defer cancel()
	name := PythonBinary
	if interpreter != "" {
		name = interpreter
	}
	return p.runner.Run(cctx, dir, name, nil, args...)
}

// probe verifies imports in the venv and maps results to statuses.
func (p *PythonProvisioner) probe(ctx context.Context, skillsRoot string, union []string) ([]PythonDependency, error) {
	deps := make([]PythonDependency, 0, len(union))
	if _, err := p.runner.LookPath(PythonBinary); err != nil {
		for _, r := range union {
			deps = append(deps, PythonDependency{Requirement: r, Status: DepUnprovisioned})
		}
		return deps, nil
	}
	interp := VenvInterpreter(skillsRoot)
	for _, req := range union {
		module := ImportName(req)
		if _, err := p.runner.Run(ctx, "", interp, nil, "-c", "import "+module); err != nil {
			deps = append(deps, PythonDependency{Requirement: req, Status: DepUnprovisioned})
			continue
		}
		deps = append(deps, PythonDependency{Requirement: req, Status: DepMet})
	}
	return deps, nil
}

// unionRequirements merges per-skill requirement lists, deduplicated in
// first-seen order.
func unionRequirements(perSkill []SkillRequirements) []string {
	seen := map[string]bool{}
	var union []string
	for _, entry := range perSkill {
		for _, req := range entry.Requirements {
			if !seen[req] {
				seen[req] = true
				union = append(union, req)
			}
		}
	}
	return union
}

// checkConflicts fails when two enabled skills declare the same
// distribution with differing requirement lines.
func checkConflicts(perSkill []SkillRequirements) error {
	type claim struct {
		req  string
		from SkillRequirements
	}
	byName := map[string]claim{}
	for _, entry := range perSkill {
		for _, req := range entry.Requirements {
			name := RequirementName(req)
			prior, ok := byName[name]
			if ok && prior.req != req {
				return &ErrDependencyConflict{Package: name, Left: prior.from, Right: entry}
			}
			if !ok {
				byName[name] = claim{req: req, from: entry}
			}
		}
	}
	return nil
}

// ImportName converts a requirement line to its importable module name
// heuristic: dots and hyphens become underscores.
func ImportName(req string) string {
	name := strings.TrimSpace(req)
	if i := strings.IndexAny(name, "=<>!~; "); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "["); i >= 0 {
		name = name[:i]
	}
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

// binaryInstallHints names the per-platform package-manager command for a
// missing binary (spec: "Missing binary reports guidance"). Keys are
// runtime.GOOS values.
var binaryInstallHints = map[string]func(binary string) string{
	"darwin": func(b string) string { return "brew install " + b },
	"linux":  func(b string) string { return "sudo apt-get install " + b },
	"windows": func(b string) string {
		return "choco install " + b + " (or download from the vendor site)"
	},
}

// BinaryInstallHint returns the platform install command for a binary.
func BinaryInstallHint(binary string) string {
	if hint, ok := binaryInstallHints[runtime.GOOS]; ok {
		return hint(binary)
	}
	return fmt.Sprintf("install %q via your platform's package manager", binary)
}

// CheckBinaries probes each binary name on the server PATH via LookPath,
// returning statuses (met / missing) with per-platform install hints for
// misses. Binaries are never auto-installed (design D5).
func CheckBinaries(runner CommandRunner, names []string) []BinaryDependency {
	deps := make([]BinaryDependency, 0, len(names))
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for _, name := range sorted {
		if _, err := runner.LookPath(name); err != nil {
			deps = append(deps, BinaryDependency{Name: name, Status: DepMissing, InstallHint: BinaryInstallHint(name)})
			continue
		}
		deps = append(deps, BinaryDependency{Name: name, Status: DepMet})
	}
	return deps
}

func init() {
	// Python ships as the first provisioner registration (plugins register
	// beside it via RegisterProvisioner).
	RegisterProvisioner(NewPythonProvisioner(NewExecRunner()))
}
