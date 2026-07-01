package skill

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// Fetcher defines the interface for downloading and extracting a source.
type Fetcher interface {
	Fetch(ctx context.Context, source string, branch string) (tempDir string, err error)
}

// Installer handles installing, removing, updating, and listing skills.
type Installer struct {
	store store.SkillStore
	home  string
}

// NewInstaller creates a new Installer.
func NewInstaller(s store.SkillStore, home string) *Installer {
	return &Installer{store: s, home: home}
}

// DiscoverSource fetches a source to a temporary directory and returns the package name,
// whether it is a plugin, the list of discovered candidates, and the temporary directory path.
// The caller is responsible for deleting the tempDir.
func (inst *Installer) DiscoverSource(ctx context.Context, source string, branch string, isPluginForced bool) (pkgName string, isPlugin bool, candidates []*Candidate, tempDir string, err error) {
	// 1. Classify and fetch source
	var fetcher Fetcher
	if isGithubSource(source) {
		fetcher = NewGithubFetcher()
	} else if isHTTPSource(source) {
		fetcher = NewHTTPFetcher()
	} else {
		fetcher = NewLocalFetcher()
	}

	tempDir, err = fetcher.Fetch(ctx, source, branch)
	if err != nil {
		return "", false, nil, "", fmt.Errorf("failed to fetch source: %w", err)
	}

	// 2. Detect plugin
	isPlugin = isPluginForced || detectPlugin(tempDir)
	restrict := ""
	if isPlugin {
		restrict = "skills"
	}

	// 3. Discover candidates
	candidates, err = Discover(tempDir, restrict)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", false, nil, "", fmt.Errorf("failed to discover skills: %w", err)
	}

	// 4. Determine package name
	pkgName = determinePackageName(source, tempDir, isPlugin)

	return pkgName, isPlugin, candidates, tempDir, nil
}

// InstallOpts controls installation behavior (overrides, custom names).
type InstallOpts struct {
	Force  bool
	AsName string
	Branch string
}

// Install installs selected skills from a source into the target scope.
func (inst *Installer) Install(ctx context.Context, source string, selectedNames []string, scope string, opts InstallOpts) ([]*store.Skill, error) {
	_, isPlugin, candidates, tempDir, err := inst.DiscoverSource(ctx, source, opts.Branch, false)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no skills found in source")
	}

	// If no names are selected, we require explicit selection unless there is exactly 1 candidate.
	if len(selectedNames) == 0 {
		if len(candidates) == 1 {
			selectedNames = []string{candidates[0].Name}
		} else {
			return nil, fmt.Errorf("multiple skills found in source; please specify which skills to install or use --all to install all")
		}
	}

	selectedMap := make(map[string]bool)
	for _, n := range selectedNames {
		selectedMap[n] = true
	}

	var installed []*store.Skill

	for _, cand := range candidates {
		if !selectedMap[cand.Name] {
			continue
		}

		// Calculate install target name
		var targetName string
		if opts.AsName != "" {
			targetName = opts.AsName
		} else {
			targetName = cand.Name
		}

		// Calculate target directory
		targetDir := filepath.Join(TargetDir(inst.home, scope), targetName)

		// Calculate hash of raw/original content of SKILL.md
		h := sha256.New()
		h.Write([]byte(cand.Content))
		hashStr := fmt.Sprintf("%x", h.Sum(nil))

		// Check idempotency against ledger
		existing, err := inst.store.GetSkill(ctx, targetName, scope)
		if err == nil {
			// Record exists in ledger. Compare source + hash.
			if existing.Source == source && existing.Hash == hashStr && !opts.Force {
				// Same source + same hash + not forced -> no-op
				installed = append(installed, existing)
				continue
			}

			if existing.Source != source && !opts.Force {
				// Collision with a different source!
				return nil, fmt.Errorf("skill name collision: skill %q is already installed from %q. Use --force or --as to install", targetName, existing.Source)
			}
		}

		// Copy files & normalize manifest
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
		}

		// Copy all files from candidate path to targetDir
		err = copyDirectory(cand.Path, targetDir, cand.Content, targetName)
		if err != nil {
			return nil, fmt.Errorf("failed to copy skill files: %w", err)
		}

		// Determine SourceType (plugin support)
		srcType := classifySourceType(source)
		if isPlugin {
			srcType = "plugin"
		}

		// Upsert in database ledger
		sk := &store.Skill{
			Name:        targetName,
			Scope:       scope,
			SourceType:  srcType,
			Source:      source,
			SkillPath:   targetDir,
			Version:     cand.Version,
			Hash:        hashStr,
			Description: cand.Description,
			Enabled:     1,
		}

		if existing != nil {
			sk.InstalledAt = existing.InstalledAt
			err = inst.store.UpdateSkill(ctx, sk)
		} else {
			err = inst.store.AddSkill(ctx, sk)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to write skill ledger: %w", err)
		}

		installed = append(installed, sk)
	}

	return installed, nil
}

// GetSkill retrieves an installed skill from the ledger by its name and scope.
func (inst *Installer) GetSkill(ctx context.Context, name string, scope string) (*store.Skill, error) {
	return inst.store.GetSkill(ctx, name, scope)
}

// Remove uninstalls a skill by deleting its directory and ledger record.
func (inst *Installer) Remove(ctx context.Context, name string, scope string) error {
	sk, err := inst.store.GetSkill(ctx, name, scope)
	if err != nil {
		return err
	}

	// Delete target directory
	if sk.SkillPath != "" {
		_ = os.RemoveAll(sk.SkillPath)
	}

	// Delete from ledger
	return inst.store.RemoveSkill(ctx, name, scope)
}

// Update updates an installed skill by re-fetching its original source.
func (inst *Installer) Update(ctx context.Context, name string, scope string) (*store.Skill, error) {
	sk, err := inst.store.GetSkill(ctx, name, scope)
	if err != nil {
		return nil, err
	}

	// Re-install
	var selected []string
	// If name contains a colon, it's namespaced e.g. "pkg:skillA".
	// The candidate name discovered from the source will be just "skillA".
	candidateName := name
	if idx := strings.Index(name, ":"); idx != -1 {
		candidateName = name[idx+1:]
	}
	selected = []string{candidateName}

	installed, err := inst.Install(ctx, sk.Source, selected, sk.Scope, InstallOpts{
		Force:  true,
		AsName: name, // retain the same name
	})
	if err != nil {
		return nil, err
	}

	if len(installed) == 0 {
		return nil, fmt.Errorf("failed to find skill %q in source %q", name, sk.Source)
	}

	return installed[0], nil
}

// List lists all installed skills from the ledger.
func (inst *Installer) List(ctx context.Context) ([]*store.Skill, error) {
	return inst.store.ListSkills(ctx)
}

// Helper: classify source type
func classifySourceType(source string) string {
	if isGithubSource(source) {
		return "github"
	}
	if isHTTPSource(source) {
		return "http"
	}
	return "local"
}

func isGithubSource(source string) bool {
	if strings.HasPrefix(source, "github.com/") || strings.HasPrefix(source, "https://github.com/") {
		return true
	}
	// owner/repo shape (no scheme, has exactly one slash)
	parts := strings.Split(source, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(source, ":")
}

func isHTTPSource(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func determinePackageName(source string, tempDir string, isPlugin bool) string {
	if isPlugin {
		pluginJSONPath := filepath.Join(tempDir, ".claude-plugin", "plugin.json")
		if data, err := os.ReadFile(pluginJSONPath); err == nil {
			var parsed struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &parsed); err == nil && parsed.Name != "" {
				return parsed.Name
			}
		}
	}

	if isGithubSource(source) {
		// e.g. github.com/owner/repo or owner/repo
		clean := strings.TrimPrefix(source, "https://")
		clean = strings.TrimPrefix(clean, "github.com/")
		parts := strings.Split(clean, "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s-%s", parts[0], parts[1])
		}
	}

	// Local or HTTP: use basename of source/dir
	base := filepath.Base(source)
	base = strings.TrimSuffix(base, ".tar.gz")
	base = strings.TrimSuffix(base, ".tgz")
	base = strings.TrimSuffix(base, ".zip")
	return base
}

func copyDirectory(src, dst string, normalizedManifest string, skillName string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// If this is the SKILL.md file, write the normalized content instead of copying
		if strings.ToLower(info.Name()) == "skill.md" {
			normalized, _, err := ParseAndNormalizeManifest(normalizedManifest, skillName)
			if err != nil {
				normalized = normalizedManifest
			}
			// Write as exactly SKILL.md (force case)
			targetSkillPath := filepath.Join(filepath.Dir(targetPath), "SKILL.md")
			return os.WriteFile(targetSkillPath, []byte(normalized), 0644)
		}

		// Copy file verbatim
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}
