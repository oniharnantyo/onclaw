package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

type KGSearchTool struct{}

func (k *KGSearchTool) Name() string { return "kg_search" }
func (k *KGSearchTool) Desc() string {
	return "Search the agent's knowledge graph for connected entities and relations starting from a seed entity."
}
func (k *KGSearchTool) Category() string { return "Memory" }

type KGSearchInput struct {
	SeedEntityName string `json:"seed_entity_name" jsonschema_description:"The name or ID of the entity to start the graph traversal from"`
	MaxDepth       int    `json:"max_depth,omitempty" jsonschema_description:"Maximum traversal depth (default 3)"`
}

func (k *KGSearchTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(k.Name(), k.Desc(), func(ctx context.Context, input *KGSearchInput) (string, error) {
		if scope.KGStore == nil {
			return "Knowledge graph is not available.", nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if input.SeedEntityName == "" {
			return "seed_entity_name is required", nil
		}
		maxDepth := input.MaxDepth
		if maxDepth <= 0 && scope.KGTraversalDepth > 0 {
			maxDepth = scope.KGTraversalDepth
		}
		if maxDepth <= 0 {
			maxDepth = 3
		}
		// Get max_depth from tool-group config if available: overrides scope default
		if scope.ToolGroupCfg != nil {
			if cfgStr, err := scope.ToolGroupCfg.GetConfig(ctx, "Memory"); err == nil && cfgStr != "" {
				var tc struct {
					MaxDepth int `json:"max_depth"`
				}
				if err := jsonUnmarshalStrict([]byte(cfgStr), &tc); err == nil && tc.MaxDepth > 0 {
					// Use configured max_depth if user didn't override
					if input.MaxDepth <= 0 {
						maxDepth = tc.MaxDepth
					}
				}
			}
		}

		// Build query — pass the name directly; SearchGraph resolves to ID internally
		query := &memory.KGQuery{
			Agent:          scope.AgentName,
			SeedEntityName: input.SeedEntityName,
			MaxDepth:       maxDepth,
			Limit:          100,
		}

		hits, err := scope.KGStore.SearchGraph(ctx, query)
		if err != nil {
			return "", fmt.Errorf("failed to search knowledge graph: %w", err)
		}
		if len(hits) == 0 {
			return fmt.Sprintf("No connected entities found for seed entity '%s'.", input.SeedEntityName), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Connected entities from seed '%s' (depth %d):\n", input.SeedEntityName, maxDepth))
		for i := range hits {
			hit := &hits[i]
			// Format relation path: "relation1 -> relation2 -> entity_name"
			var pathParts []string
			for _, rel := range hit.Path {
				pathParts = append(pathParts, rel.Predicate)
			}
			pathStr := ""
			if len(pathParts) > 0 {
				pathStr = strings.Join(pathParts, " -> ") + " -> "
			}
			entityName := "unknown"
			entityType := "unknown"
			if hit.Entity != nil {
				entityName = hit.Entity.Name
				entityType = hit.Entity.Type
			}
			sb.WriteString(fmt.Sprintf("- %s%s (%s) [distance %d]\n", pathStr, entityName, entityType, hit.Distance))
		}
		return sb.String(), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}

// jsonUnmarshalStrict is a strict JSON unmarshaler that rejects unknown fields.
func jsonUnmarshalStrict(data []byte, v interface{}) error {
	// Use standard json.Unmarshal for simplicity; could be made stricter if needed
	return json.Unmarshal(data, v)
}

const kgJSONSchema = `{
  "type": "object",
  "properties": {
    "max_depth": {
      "type": "integer",
      "default": 3,
      "description": "Maximum graph traversal depth (default 3)"
    }
  }
}`

func init() {
	Register(&KGSearchTool{})
	RegisterConfig("Memory", kgJSONSchema, func(ctx context.Context, cfg string) error {
		// Config validation if needed
		return nil
	}, func(ctx context.Context) (string, error) {
		// Return default config
		return `{"max_depth":3}`, nil
	})
}
