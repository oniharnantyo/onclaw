package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Tier represents a skill tier in the precedence hierarchy.
type Tier string

const (
	TierSystem    Tier = "system"
	TierWorkspace Tier = "workspace"
	TierAgent     Tier = "agent"
)

// EnabledSkillReader reads the names of workspace skills whose registry rows
// are enabled for a workspace. It is the skill backend's read-only dependency
// on the workspace-skill registry; the store's WorkspaceSkillStore satisfies
// it via a thin adapter. A nil reader (unit tests only) treats every
// on-disk workspace skill as enabled.
type EnabledSkillReader interface {
	EnabledSkillNames(ctx context.Context, workspaceSlug string) ([]string, error)
}

// SkillMetadata represents minimal skill metadata for progressive disclosure.
type SkillMetadata struct {
	Name         string
	Tier         Tier
	Description  string
	Dependencies SkillDependencies
}

// SkillDependencies lists the runtime dependencies a SKILL.md declares in its
// frontmatter (design D5): tools the skill expects to be allowed, host
// binaries it shells out to, and python packages it imports.
type SkillDependencies struct {
	Tools    []string
	Binaries []string
	Python   []string
}

// SkillBody represents the full content of a skill.
type SkillBody struct {
	Name          string
	Tier          Tier
	Content       string
	BaseDirectory string
}

// skillBackend resolves skills across three tiers with precedence:
// agent > workspace > system, adapting them to the Eino skill middleware's
// progressive-disclosure Backend (metadata list, body on demand).
// The workspace tier is governed by the registry master switch: only skills
// whose rows are enabled attach; system and agent tiers need no registry.
type skillBackend struct {
	rootDir       string
	tenantSlug    string
	agentSlug     string
	enabledReader EnabledSkillReader
}

// NewSkillBackend creates a skillBackend. enabledReader may be nil in unit
// tests, in which case every discovered workspace skill is considered enabled.
func NewSkillBackend(rootDir, tenantSlug, agentSlug string, enabledReader EnabledSkillReader) *skillBackend {
	return &skillBackend{
		rootDir:       rootDir,
		tenantSlug:    tenantSlug,
		agentSlug:     agentSlug,
		enabledReader: enabledReader,
	}
}

// enabledWorkspaceSet returns the set of enabled workspace skill names.
// The bool reports whether registry gating applies at all (false when the
// reader is nil — unit-test mode).
func (b *skillBackend) enabledWorkspaceSet(ctx context.Context) (map[string]struct{}, bool, error) {
	if b.enabledReader == nil {
		return nil, false, nil
	}
	names, err := b.enabledReader.EnabledSkillNames(ctx, b.tenantSlug)
	if err != nil {
		return nil, false, fmt.Errorf("skillBackend: read enabled workspace skills: %w", err)
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set, true, nil
}

// List returns metadata for all available skills, applying precedence and
// registry gating: the workspace tier contributes only enabled skills; when
// no workspace skill is enabled (master switch off) the tier is absent.
func (b *skillBackend) List(ctx context.Context) ([]einoskill.FrontMatter, error) {
	enabled, gated, err := b.enabledWorkspaceSet(ctx)
	if err != nil {
		return nil, err
	}

	systemSkills, err := b.collectTier(ctx, domain.SystemSkillsDir(b.rootDir), TierSystem)
	if err != nil {
		return nil, fmt.Errorf("skillBackend.List: failed to collect system skills: %w", err)
	}

	workspaceSkills, err := b.collectTier(ctx, domain.WorkspaceSkillsDir(b.rootDir, b.tenantSlug), TierWorkspace)
	if err != nil {
		return nil, fmt.Errorf("skillBackend.List: failed to collect workspace skills: %w", err)
	}

	agentSkills, err := b.collectTier(ctx, domain.AgentSkillsDir(b.rootDir, b.tenantSlug, b.agentSlug), TierAgent)
	if err != nil {
		return nil, fmt.Errorf("skillBackend.List: failed to collect agent skills: %w", err)
	}

	// Apply precedence: most specific wins on name collision
	// Precedence: agent > workspace > system
	skillsMap := make(map[string]SkillMetadata)

	// Add system skills first (lowest precedence)
	for _, skill := range systemSkills {
		skillsMap[skill.Name] = skill
	}

	// Override with enabled workspace skills
	for _, skill := range workspaceSkills {
		if gated {
			if _, ok := enabled[skill.Name]; !ok {
				continue
			}
		}
		skillsMap[skill.Name] = skill
	}

	// Override with agent skills (highest precedence)
	for _, skill := range agentSkills {
		skillsMap[skill.Name] = skill
	}

	result := make([]einoskill.FrontMatter, 0, len(skillsMap))
	for _, skill := range skillsMap {
		result = append(result, toFrontMatter(skill))
	}

	return result, nil
}

// Get retrieves the full content of a skill by name, resolving across tiers with precedence.
// Disabled workspace skills are skipped, so a same-name system skill can win.
// Returns an error if the skill is not found.
func (b *skillBackend) Get(ctx context.Context, name string) (einoskill.Skill, error) {
	enabled, gated, err := b.enabledWorkspaceSet(ctx)
	if err != nil {
		return einoskill.Skill{}, err
	}

	// Search tiers in precedence order: agent > workspace > system
	tiers := []struct {
		dir  string
		tier Tier
	}{
		{
			dir:  domain.AgentSkillsDir(b.rootDir, b.tenantSlug, b.agentSlug),
			tier: TierAgent,
		},
		{
			dir:  domain.WorkspaceSkillsDir(b.rootDir, b.tenantSlug),
			tier: TierWorkspace,
		},
		{
			dir:  domain.SystemSkillsDir(b.rootDir),
			tier: TierSystem,
		},
	}

	for _, tier := range tiers {
		if tier.tier == TierWorkspace && gated {
			if _, ok := enabled[name]; !ok {
				continue
			}
		}

		skillPath := filepath.Join(tier.dir, name, "SKILL.md")
		content, err := os.ReadFile(skillPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return einoskill.Skill{}, fmt.Errorf("skillBackend.Get: failed to read skill %s: %w", name, err)
		}

		return einoskill.Skill{
			FrontMatter: einoskill.FrontMatter{
				Name: name,
			},
			Content:       string(content),
			BaseDirectory: filepath.Join(tier.dir, name),
		}, nil
	}

	return einoskill.Skill{}, fmt.Errorf("skillBackend.Get: skill not found: %s", name)
}

// AvailableSkillNames returns the names of all skills attachable to the
// addressed agent: the system tier, the enabled workspace tier, and the
// agent tier. It backs $name explicit-invocation matching (design D7).
func AvailableSkillNames(ctx context.Context, rootDir, tenantSlug, agentSlug string, reader EnabledSkillReader) ([]string, error) {
	b := NewSkillBackend(rootDir, tenantSlug, agentSlug, reader)
	enabled, gated, err := b.enabledWorkspaceSet(ctx)
	if err != nil {
		return nil, err
	}

	systemSkills, err := b.collectTier(ctx, domain.SystemSkillsDir(rootDir), TierSystem)
	if err != nil {
		return nil, fmt.Errorf("AvailableSkillNames: collect system skills: %w", err)
	}
	workspaceSkills, err := b.collectTier(ctx, domain.WorkspaceSkillsDir(rootDir, tenantSlug), TierWorkspace)
	if err != nil {
		return nil, fmt.Errorf("AvailableSkillNames: collect workspace skills: %w", err)
	}
	agentSkills, err := b.collectTier(ctx, domain.AgentSkillsDir(rootDir, tenantSlug, agentSlug), TierAgent)
	if err != nil {
		return nil, fmt.Errorf("AvailableSkillNames: collect agent skills: %w", err)
	}

	names := make(map[string]struct{})
	for _, skill := range systemSkills {
		names[skill.Name] = struct{}{}
	}
	for _, skill := range workspaceSkills {
		if gated {
			if _, ok := enabled[skill.Name]; !ok {
				continue
			}
		}
		names[skill.Name] = struct{}{}
	}
	for _, skill := range agentSkills {
		names[skill.Name] = struct{}{}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	return result, nil
}

// collectTier collects all skills from a specific tier directory.
func (b *skillBackend) collectTier(ctx context.Context, tierDir string, tier Tier) ([]SkillMetadata, error) {
	entries, err := os.ReadDir(tierDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, no skills in this tier
		}
		return nil, fmt.Errorf("failed to read tier directory %s: %w", tierDir, err)
	}

	var skills []SkillMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		skillPath := filepath.Join(tierDir, skillName, "SKILL.md")
		content, err := os.ReadFile(skillPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Not a valid skill directory (no SKILL.md)
			}
			return nil, fmt.Errorf("failed to read skill file %s: %w", skillPath, err)
		}

		description, deps := parseFrontmatter(string(content))

		skills = append(skills, SkillMetadata{
			Name:         skillName,
			Tier:         tier,
			Description:  description,
			Dependencies: deps,
		})
	}

	return skills, nil
}

func toFrontMatter(m SkillMetadata) einoskill.FrontMatter {
	return einoskill.FrontMatter{
		Name:        m.Name,
		Description: m.Description,
	}
}

// parseFrontmatter extracts the description and declared dependencies from a
// SKILL.md. When the file carries a YAML frontmatter block (leading `---`),
// the description comes from its `description:` key and dependencies from its
// `dependencies.{tools,binaries,python}` sub-keys; malformed frontmatter
// degrades to the description heuristics below and zero dependencies. This
// parser is deliberately independent of eino's FrontMatter type (design D5).
func parseFrontmatter(content string) (string, SkillDependencies) {
	block, ok := frontmatterBlock(content)
	if !ok {
		return parseDescription(content), SkillDependencies{}
	}
	description, deps := parseFrontmatterBlock(block)
	if description == "" {
		description = parseDescription(content)
	}
	return description, deps
}

// frontmatterBlock returns the raw text between the leading `---` markers.
func frontmatterBlock(content string) (string, bool) {
	rest, ok := strings.CutPrefix(content, "---")
	if !ok {
		return "", false
	}
	// Require the marker to sit on its own line.
	rest = strings.TrimPrefix(rest, "\r")
	if !strings.HasPrefix(rest, "\n") {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false // unterminated frontmatter: treat as body
	}
	return rest[:end], true
}

// parseFrontmatterBlock parses the description key and the dependencies
// sub-keys from a frontmatter block. Malformed values are ignored.
func parseFrontmatterBlock(block string) (string, SkillDependencies) {
	var deps SkillDependencies
	var description string
	var currentList *[]string

	lines := strings.Split(block, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			currentList = nil
			continue
		}
		indented := line != trimmed // still inside a nested block

		if !indented {
			currentList = nil
			key, value, hasValue := splitKeyValue(trimmed)
			switch key {
			case "description":
				description = strings.TrimSpace(value)
			case "dependencies":
				// A following indented block (or inline map) fills the deps.
				if hasValue {
					parseInlineDependencies(value, &deps)
				}
			}
			continue
		}

		// Indented line: either "key:" opening a dependency list or a "- item".
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			if currentList != nil {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				item = strings.Trim(item, `"'`)
				if item != "" {
					*currentList = append(*currentList, item)
				}
			}
			continue
		}
		key, value, _ := splitKeyValue(trimmed)
		switch key {
		case "tools":
			deps.Tools = parseInlineList(value, deps.Tools)
			currentList = &deps.Tools
		case "binaries":
			deps.Binaries = parseInlineList(value, deps.Binaries)
			currentList = &deps.Binaries
		case "python":
			deps.Python = parseInlineList(value, deps.Python)
			currentList = &deps.Python
		default:
			currentList = nil
		}
	}
	return description, deps
}

// splitKeyValue splits "key: value"; hasValue is false when no colon present.
func splitKeyValue(trimmed string) (key, value string, hasValue bool) {
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return trimmed, "", false
	}
	return strings.TrimSpace(trimmed[:idx]), strings.TrimSpace(trimmed[idx+1:]), true
}

// parseInlineList appends items from a "[a, b]" flow style to dst.
func parseInlineList(value string, dst []string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return dst
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	for _, item := range strings.Split(inner, ",") {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"'`)
		if item != "" {
			dst = append(dst, item)
		}
	}
	return dst
}

// parseInlineDependencies handles the one-line flow form
// `dependencies: {tools: [a], binaries: [b], python: [c]}`.
func parseInlineDependencies(value string, deps *SkillDependencies) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}")
	for _, pair := range strings.Split(inner, ",") {
		key, val, ok := splitKeyValue(strings.TrimSpace(pair))
		if !ok {
			continue
		}
		switch key {
		case "tools":
			deps.Tools = parseInlineList(val, deps.Tools)
		case "binaries":
			deps.Binaries = parseInlineList(val, deps.Binaries)
		case "python":
			deps.Python = parseInlineList(val, deps.Python)
		}
	}
}

// parseDescription extracts a description from SKILL.md content.
// Minimal parsing: looks for a description: line in the frontmatter-like section,
// or uses the first non-heading line after the frontmatter.
func parseDescription(content string) string {
	lines := strings.Split(content, "\n")

	// First pass: look for "description:" anywhere in the file
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "description:") {
			desc := strings.TrimPrefix(trimmed, "description:")
			desc = strings.TrimSpace(desc)
			if desc != "" {
				return desc
			}
		}
	}

	// Second pass: use first non-heading, non-empty, non-comment line
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip YAML frontmatter markers and heading/comment-like lines
		if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Use the first meaningful line as description
		if !strings.HasPrefix(trimmed, "name:") && !strings.HasPrefix(trimmed, "context:") {
			return trimmed
		}
	}

	return "A skill"
}
