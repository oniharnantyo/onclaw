package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// The skill backend (spec agent-skills, design D6): skills resolve across two
// tiers — the global skills library (<HOME>/.buddies/skills) and the agent's
// own workspace skills directory (workspaces/<slug>/skills) — with the agent
// tier winning on a name conflict. Both tiers are read fresh on every call:
// there is no cache to invalidate, so a skill dropped into the tree applies
// to the next resolve without an app restart. Onclaw's embedded system tier
// is dropped (its two system skills serve tools buddies does not have).
//
// The backend adapts the two tiers to the Eino skill middleware's
// progressive-disclosure Backend (metadata list → body on demand) and owns
// the eligibility gate (spec "Skill eligibility gate"): a skill is offered
// only when it is enabled globally and permitted by the agent's skill
// allowlist (an empty allowlist permits all discovered skills).

// Tier names a skill's source tier in the precedence hierarchy.
type Tier string

const (
	// TierGlobal is the shared global skills library.
	TierGlobal Tier = "global"
	// TierAgent is the agent's own workspace skills directory; it shadows
	// TierGlobal on a name conflict.
	TierAgent Tier = "agent"
)

// SkillEligibility is the skill backend's read-only dependency on the global
// enable state of the skill library (the store's skill registry, which
// indexes the global tier for the settings pane). DisabledSkillNames returns
// the names switched off globally. Eligibility is name-level: a disabled
// name is ineligible across both tiers, so disabling a globally-shadows-
// agent skill never leaves the agent tier serving it. A nil reader (unit
// tests only) treats every skill as enabled; a name absent from the
// disabled set is enabled — the registry only knows the global library, so
// a never-indexed workspace skill is eligible by default (the tool gate's
// absence-means-enabled convention).
type SkillEligibility interface {
	DisabledSkillNames(ctx context.Context) ([]string, error)
}

// SkillMetadata is one skill's minimal metadata for progressive disclosure.
type SkillMetadata struct {
	Name        string
	Tier        Tier
	Description string
}

// SkillBody is one skill's full loaded content.
type SkillBody struct {
	Name string
	Tier Tier
	// Content is the raw SKILL.md text (frontmatter included, exactly as on
	// disk — the middleware presents it to the model as written).
	Content string
	// BaseDirectory is the skill's directory, for resolving the supporting
	// files a body references.
	BaseDirectory string
}

// SkillBackend resolves skills across the two tiers with precedence
// agent > global, gated by the eligibility reader and the agent's allowlist.
type SkillBackend struct {
	globalDir string
	agentDir  string
	eligible  SkillEligibility
	allowlist map[string]struct{}
}

// NewSkillBackend creates the backend for one agent's turns. globalDir is
// the global skills library and agentDir the agent's workspace skills
// directory (either may not exist yet — an absent tier contributes nothing).
// eligibility may be nil (unit tests only): every skill counts as enabled.
// allowlist nil or empty permits all discovered skills.
func NewSkillBackend(globalDir, agentDir string, eligibility SkillEligibility, allowlist []string) *SkillBackend {
	set := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		set[name] = struct{}{}
	}
	return &SkillBackend{
		globalDir: globalDir,
		agentDir:  agentDir,
		eligible:  eligibility,
		allowlist: set,
	}
}

// List returns the eligible skills' metadata: both tiers read fresh, agent
// tier wins on a name conflict, then the eligibility gate filters. The
// result is sorted by name so the turn context is deterministic.
func (b *SkillBackend) List(ctx context.Context) ([]einoskill.FrontMatter, error) {
	disabled, err := b.disabledSet(ctx)
	if err != nil {
		return nil, err
	}

	global, err := b.collectTier(ctx, b.globalDir, TierGlobal)
	if err != nil {
		return nil, fmt.Errorf("skillBackend.List: failed to collect global skills: %w", err)
	}
	agent, err := b.collectTier(ctx, b.agentDir, TierAgent)
	if err != nil {
		return nil, fmt.Errorf("skillBackend.List: failed to collect agent skills: %w", err)
	}

	// Apply precedence: agent tier overrides global on a name conflict.
	byName := make(map[string]SkillMetadata, len(global)+len(agent))
	for _, skill := range global {
		byName[skill.Name] = skill
	}
	for _, skill := range agent {
		byName[skill.Name] = skill
	}

	names := make([]string, 0, len(byName))
	for name, skill := range byName {
		if !b.permitted(name, disabled) {
			continue
		}
		names = append(names, skill.Name)
	}
	sort.Strings(names)

	result := make([]einoskill.FrontMatter, 0, len(names))
	for _, name := range names {
		result = append(result, einoskill.FrontMatter{
			Name:        name,
			Description: byName[name].Description,
		})
	}
	return result, nil
}

// Get loads one skill's full body, resolving across the tiers with
// precedence agent > global. A skill that fails the eligibility gate is not
// found — a disabled or non-allowlisted name must never load a body. The
// read is fresh: an edit on disk applies to the next call.
func (b *SkillBackend) Get(ctx context.Context, name string) (einoskill.Skill, error) {
	disabled, err := b.disabledSet(ctx)
	if err != nil {
		return einoskill.Skill{}, err
	}
	if !b.permitted(name, disabled) {
		return einoskill.Skill{}, fmt.Errorf("skillBackend.Get: skill not available: %s", name)
	}

	// Search tiers in precedence order: agent > global.
	tiers := []struct {
		dir  string
		tier Tier
	}{
		{dir: b.agentDir, tier: TierAgent},
		{dir: b.globalDir, tier: TierGlobal},
	}
	for _, tier := range tiers {
		skillPath := filepath.Join(tier.dir, name, SkillMDFileName)
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

// AvailableSkillNames returns the names of all skills eligible to the
// addressed agent — the same set List advertises, as bare names. It backs
// $name explicit-invocation matching: an unknown or ineligible token stays
// plain text.
func (b *SkillBackend) AvailableSkillNames(ctx context.Context) ([]string, error) {
	metadata, err := b.List(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(metadata))
	for _, m := range metadata {
		names = append(names, m.Name)
	}
	return names, nil
}

// permitted applies the eligibility gate: the name must be globally enabled
// (absent from the disabled set) and, when an allowlist is configured,
// listed on it.
func (b *SkillBackend) permitted(name string, disabled map[string]struct{}) bool {
	if _, off := disabled[name]; off {
		return false
	}
	if len(b.allowlist) == 0 {
		return true
	}
	_, ok := b.allowlist[name]
	return ok
}

// disabledSet resolves the globally disabled names once per call. A nil
// reader disables nothing (unit-test mode); a reader failure propagates —
// a broken eligibility read must not silently widen the offered set.
func (b *SkillBackend) disabledSet(ctx context.Context) (map[string]struct{}, error) {
	if b.eligible == nil {
		return nil, nil
	}
	names, err := b.eligible.DisabledSkillNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("skillBackend: read disabled skills: %w", err)
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set, nil
}

// collectTier collects every skill in one tier directory, read fresh.
// Directory entries without a SKILL.md are not skills and are skipped; an
// absent tier directory is the normal empty state.
func (b *SkillBackend) collectTier(ctx context.Context, tierDir string, tier Tier) ([]SkillMetadata, error) {
	entries, err := os.ReadDir(tierDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no tier directory yet: no skills in this tier
		}
		return nil, fmt.Errorf("failed to read tier directory %s: %w", tierDir, err)
	}

	var skills []SkillMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		skillPath := filepath.Join(tierDir, skillName, SkillMDFileName)
		content, err := os.ReadFile(skillPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // not a valid skill directory
			}
			return nil, fmt.Errorf("failed to read skill file %s: %w", skillPath, err)
		}
		skills = append(skills, SkillMetadata{
			Name:        skillName,
			Tier:        tier,
			Description: ParseSkillDescription(string(content)),
		})
	}
	return skills, nil
}

// SkillMDFileName is the convention marker file of a skill directory. It
// mirrors the scanner's SkillMD constant; the backend keeps its own copy so
// the two packages stay decoupled.
const SkillMDFileName = "SKILL.md"

// ParseSkillDescription extracts a description from SKILL.md content: the
// `description:` key of the leading frontmatter block when present,
// otherwise the first meaningful body line as a fallback. Shared by the
// backend's two tiers so both parse identically.
func ParseSkillDescription(content string) string {
	if block, ok := frontmatterBlock(content); ok {
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "description:") {
				if desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")); desc != "" {
					return desc
				}
			}
		}
	}
	return parseDescriptionFallback(content)
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

// parseDescriptionFallback extracts a description when no frontmatter
// description exists: the first non-empty line that is not a heading, a
// frontmatter marker, or a `name:`/`context:` key.
func parseDescriptionFallback(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "name:") || strings.HasPrefix(trimmed, "context:") {
			continue
		}
		return trimmed
	}
	return "A skill"
}
