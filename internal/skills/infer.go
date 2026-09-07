package skills

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// StagedFile is one file of a materialized skill body, not yet written to
// the workspace skills directory.
type StagedFile struct {
	Path    string // slash-separated, relative to the skill directory
	Content []byte
	Mode    uint32 // filesystem mode bits (regular files only)
}

// StagedSkill is a materialized skill awaiting the write-files step.
type StagedSkill struct {
	Name        string
	Description string
	Version     string
	Source      Source
	Files       []StagedFile
}

// pythonImportPattern matches top-level module names in `import x` and
// `from x import ...` statements.
var pythonImportPattern = regexp.MustCompile(`(?m)^\s*(?:import|from)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// pythonStdlibModules are import names that never imply a pip requirement.
var pythonStdlibModules = map[string]bool{
	"argparse": true, "asyncio": true, "base64": true, "bz2": true,
	"collections": true, "configparser": true, "csv": true, "dataclasses": true,
	"datetime": true, "decimal": true, "email": true, "enum": true,
	"fractions": true, "functools": true, "glob": true, "gzip": true,
	"hashlib": true, "html": true, "http": true, "io": true, "itertools": true,
	"json": true, "logging": true, "lzma": true, "math": true, "os": true,
	"pathlib": true, "random": true, "re": true, "secrets": true,
	"shutil": true, "socket": true, "sqlite3": true, "ssl": true,
	"statistics": true, "string": true, "struct": true, "subprocess": true,
	"tarfile": true, "tempfile": true, "textwrap": true, "threading": true,
	"time": true, "traceback": true, "typing": true, "unittest": true,
	"urllib": true, "uuid": true, "xml": true, "zipfile": true,
}

// KnownToolNames are tool-name patterns scanned for in SKILL.md bodies when
// a skill does not declare its tool dependencies. Mirrors the runtime tool
// catalog keys (kept as a local constant to avoid an import cycle with
// internal/agents, which will depend on this package for tier state).
var KnownToolNames = []string{
	"execute", "web.search", "web.fetch", "browser",
	"ls", "read_file", "write_file", "edit_file", "glob", "grep",
}

// DependenciesFromFrontmatter parses the OnClaw `dependencies` frontmatter
// extension (tools/binaries/python) out of SKILL.md content. Malformed
// frontmatter yields empty declarations, never an error — dependency
// inference treats unparsable declarations as absent.
func DependenciesFromFrontmatter(content string) (tools, binaries, python []string) {
	fm := frontmatterBlock(content)
	if fm == "" {
		return nil, nil, nil
	}
	tools = frontmatterList(fm, "tools")
	binaries = frontmatterList(fm, "binaries")
	python = frontmatterList(fm, "python")
	return tools, binaries, python
}

// frontmatterBlock returns the raw text between leading `---` markers.
func frontmatterBlock(content string) string {
	trimmed := strings.TrimLeft(content, "\n\r\t ")
	if !strings.HasPrefix(trimmed, "---") {
		return ""
	}
	rest := trimmed[3:]
	rest = strings.TrimLeft(rest, " \t")
	newline := strings.IndexAny(rest, "\r\n")
	if newline < 0 {
		return ""
	}
	body := rest[newline+1:]
	end := regexp.MustCompile(`(?m)^---\s*$`).FindStringIndex(body)
	if end == nil {
		return ""
	}
	return body[:end[0]]
}

// frontmatterList extracts a `key:` list value (block or inline form) from
// raw frontmatter text.
func frontmatterList(fm, key string) []string {
	lines := strings.Split(fm, "\n")
	var values []string
	inList := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !inList {
			if !strings.HasPrefix(trimmed, key+":") {
				continue
			}
			inline := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
			inline = strings.Trim(inline, "[]")
			if inline != "" {
				for _, item := range strings.Split(inline, ",") {
					item = strings.TrimSpace(strings.Trim(item, `"'`))
					if item != "" {
						values = append(values, item)
					}
				}
				return values
			}
			inList = true // block form: collect the following "- item" lines
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		item = strings.Trim(item, `"'`)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

// FrontmatterField returns a scalar frontmatter value (name, description,
// version) from SKILL.md content.
func FrontmatterField(content, key string) string {
	fm := frontmatterBlock(content)
	if fm == "" {
		return ""
	}
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(trimmed, key+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
		return strings.Trim(value, `"'`)
	}
	return ""
}

// InferDependencies derives the full dependency set for a staged skill
// without modifying any staged file: declared frontmatter dependencies,
// requirements.txt beside bundled scripts, python imports in bundled
// scripts, tool-name patterns in the SKILL.md body, and the `execute`
// implication of a non-empty scripts/ directory (spec: "Skill dependency
// resolution"). Statuses are left zero; the install service resolves them.
func InferDependencies(staged *StagedSkill) DependencySet {
	var set DependencySet

	toolSet := map[string]bool{}
	binarySet := map[string]bool{}
	pythonSet := map[string]bool{}
	addTool := func(name string) {
		if name != "" && !toolSet[name] {
			toolSet[name] = true
			set.Tools = append(set.Tools, ToolDependency{Name: name})
		}
	}
	addBinary := func(name string) {
		if name != "" && !binarySet[name] {
			binarySet[name] = true
			set.Binaries = append(set.Binaries, BinaryDependency{Name: name})
		}
	}
	addPython := func(req string) {
		if req != "" && !pythonSet[req] {
			pythonSet[req] = true
			set.Python = append(set.Python, PythonDependency{Requirement: req})
		}
	}

	var skillBody string
	scriptsNonEmpty := false
	for _, file := range staged.Files {
		base := path.Base(file.Path)
		if file.Path == "SKILL.md" {
			skillBody = string(file.Content)
			continue
		}
		if strings.HasPrefix(file.Path, "scripts/") {
			if len(file.Content) > 0 {
				scriptsNonEmpty = true
			}
			switch {
			case base == "requirements.txt":
				for _, req := range ParseRequirements(string(file.Content)) {
					addPython(req)
				}
			case strings.HasSuffix(base, ".py"):
				for _, module := range pythonImportPattern.FindAllStringSubmatch(string(file.Content), -1) {
					name := module[1]
					if !pythonStdlibModules[name] {
						addPython(name)
					}
				}
			}
		}
	}

	// Declared frontmatter dependencies win the ordering; inference fills gaps.
	declaredTools, declaredBinaries, declaredPython := DependenciesFromFrontmatter(skillBody)
	for _, name := range declaredTools {
		addTool(name)
	}
	for _, name := range declaredBinaries {
		addBinary(name)
	}
	for _, req := range declaredPython {
		addPython(req)
	}

	// A non-empty scripts/ directory implies the reserved execute tool.
	if scriptsNonEmpty {
		addTool("execute")
	}

	// Body tool-name patterns (word-bounded so "grep" in prose counts but
	// "regrexample" does not).
	for _, name := range KnownToolNames {
		if toolSet[name] {
			continue
		}
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if pattern.MatchString(skillBody) {
			addTool(name)
		}
	}

	return set
}

// requirementNamePattern captures the distribution name of a requirement
// line, before any version specifier or extras.
var requirementNamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?`)

// ParseRequirements sanitizes and parses a requirements.txt body: blank
// lines and comments are skipped; option lines (leading `-`, e.g.
// --index-url) and direct URL requirements are dropped — pip never sees
// them, enforcing the declared-requirements-only / no-custom-index policy
// (design D5) at every stage downstream of inference.
func ParseRequirements(content string) []string {
	var reqs []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.Contains(line, "://") {
			continue
		}
		reqs = append(reqs, line)
	}
	return reqs
}

// RequirementName returns the normalized distribution name of a requirement
// line (lowercased, `-`/`_` unified) for conflict detection.
func RequirementName(req string) string {
	m := requirementNamePattern.FindString(strings.TrimSpace(req))
	name := strings.ToLower(m)
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

// SortRequirementsByName orders requirement lines by normalized name — used
// to surface conflicts deterministically.
func SortRequirementsByName(reqs []string) {
	sort.Slice(reqs, func(i, j int) bool {
		return RequirementName(reqs[i]) < RequirementName(reqs[j])
	})
}
