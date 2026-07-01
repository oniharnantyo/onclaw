package middlewares

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/adk"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/schema"
)

// ResolveDirs returns the 3-tier skill directory paths for an agent in precedence order:
//  1. <home>/workspace/<agent>/skills
//  2. <home>/workspace/<agent>/.agents/skills
//  3. <home>/skills (global)
//
// Skill install/management (where installs are written) uses internal/skill.TargetDir.
func ResolveDirs(home, agent string) []string {
	if agent == "" {
		return []string{
			filepath.Join(home, "skills"),
		}
	}
	return []string{
		filepath.Join(home, "workspace", agent, "skills"),
		filepath.Join(home, "workspace", agent, ".agents", "skills"),
		filepath.Join(home, "skills"),
	}
}

// BuildMiddleware constructs the skill middleware for an agent, exposing resolved
// skills to the model via eino's progressive-disclosure skill tool. If none of the
// resolved skill directories exist, it returns (nil, nil) so agent assembly is unchanged.
func BuildMiddleware(ctx context.Context, home string, agent string) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	dirs := ResolveDirs(home, agent)

	anyExist := false
	var existingDirs []string
	for _, dir := range dirs {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			anyExist = true
			existingDirs = append(existingDirs, dir)
		}
	}

	if !anyExist {
		return nil, nil
	}

	backend := NewMultiDirBackend(existingDirs)
	mw, err := einoskill.NewTyped[*schema.AgenticMessage](ctx, &einoskill.TypedConfig[*schema.AgenticMessage]{
		Backend: backend,
	})
	if err != nil {
		return nil, err
	}

	return mw, nil
}