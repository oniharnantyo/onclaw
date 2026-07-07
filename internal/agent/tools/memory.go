package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

type memorySearchTool struct{}

func (m *memorySearchTool) Name() string { return "memory_search" }
func (m *memorySearchTool) Desc() string {
	return "Search the agent's long-term memory archive for relevant facts and context."
}
func (m *memorySearchTool) Category() string { return "Memory" }

type MemorySearchInput struct {
	Query string `json:"query" jsonschema_description:"The semantic search query for long-term memory"`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum number of results to return (default 5)"`
}

func (m *memorySearchTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(m.Name(), m.Desc(), func(ctx context.Context, input *MemorySearchInput) (string, error) {
		if scope.MemoryStore == nil {
			return "Memory archive is not available.", nil
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 5
		}
		var vector []float32
		if scope.Embedder != nil {
			vector, _ = scope.Embedder.Embed(ctx, input.Query)
		}
		ftsW := 0.3
		vecW := 0.7
		if scope.ToolGroupCfg != nil {
			if cfgStr, err := scope.ToolGroupCfg.GetConfig(ctx, "Memory"); err == nil && cfgStr != "" {
				var tc struct {
					FtsWeight    float64 `json:"fts_weight"`
					VectorWeight float64 `json:"vector_weight"`
				}
				if err := json.Unmarshal([]byte(cfgStr), &tc); err == nil {
					if tc.FtsWeight > 0 {
						ftsW = tc.FtsWeight
					}
					if tc.VectorWeight > 0 {
						vecW = tc.VectorWeight
					}
				}
			}
		}

		hits, err := scope.MemoryStore.SearchArchive(ctx, &memory.ArchiveQuery{
			Query:        input.Query,
			Agent:        scope.AgentName,
			Scope:        "global",
			Vector:       vector,
			Limit:        limit,
			FtsWeight:    ftsW,
			VectorWeight: vecW,
		})
		if err != nil {
			return "", fmt.Errorf("failed to search memory archive: %w", err)
		}
		if len(hits) == 0 {
			return "No matching long-term memories found.", nil
		}
		var sb strings.Builder
		sb.WriteString("Matching long-term memories:\n")
		for _, hit := range hits {
			sb.WriteString(fmt.Sprintf("- %s (relevance: %.2f)\n", hit.Document.Content, hit.Score))
		}
		return sb.String(), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}

type sessionSearchTool struct{}

func (s *sessionSearchTool) Name() string { return "session_search" }
func (s *sessionSearchTool) Desc() string {
	return "Search all past conversation messages across all sessions using full-text search."
}
func (s *sessionSearchTool) Category() string { return "Memory" }

type SessionSearchInput struct {
	Query string `json:"query" jsonschema_description:"The search term to match against past conversation messages"`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum number of results to return (default 5)"`
}

func sanitizeFts(q string) string {
	q = strings.ReplaceAll(q, `"`, "")
	q = strings.ReplaceAll(q, `'`, "")
	q = strings.ReplaceAll(q, `*`, "")
	q = strings.ReplaceAll(q, `:`, "")
	words := strings.Fields(q)
	if len(words) == 0 {
		return ""
	}
	var escaped []string
	for _, w := range words {
		escaped = append(escaped, `"`+w+`*"`)
	}
	return strings.Join(escaped, " AND ")
}

func (s *sessionSearchTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(s.Name(), s.Desc(), func(ctx context.Context, input *SessionSearchInput) (string, error) {
		if scope.Db == nil {
			return "Session history is not available.", nil
		}
		sanitized := sanitizeFts(input.Query)
		if sanitized == "" {
			return "No results found.", nil
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 5
		}
		q := `
			SELECT m.conversation_id, m.sequence_num, m.question, m.answer, m.created_at
			FROM conversation_messages m
			JOIN conversation_messages_fts fts ON m.id = fts.rowid
			WHERE conversation_messages_fts MATCH ?
			ORDER BY fts.rank ASC
			LIMIT ?
		`
		rows, err := scope.Db.QueryContext(ctx, q, sanitized, limit)
		if err != nil {
			return "", fmt.Errorf("failed to query past conversations: %w", err)
		}
		defer rows.Close()

		var results []string
		for rows.Next() {
			var convID, seq int64
			var question, answer, createdAt string
			if err := rows.Scan(&convID, &seq, &question, &answer, &createdAt); err != nil {
				return "", err
			}
			results = append(results, fmt.Sprintf("- [Session %d, Turn %d, Date %s]:\n  Q: %s\n  A: %s", convID, seq, createdAt, question, answer))
		}
		if len(results) == 0 {
			return "No matching past conversation messages found.", nil
		}
		return strings.Join(results, "\n"), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}

type memoryTool struct{}

func (m *memoryTool) Name() string { return "memory" }
func (m *memoryTool) Desc() string {
	return "Manage curated core memories (MEMORY.md) using add, replace, or remove operations."
}
func (m *memoryTool) Category() string { return "Memory" }

type MemoryInput struct {
	Op      string `json:"op" jsonschema_description:"The operation to perform: add, replace, or remove"`
	Target  string `json:"target,omitempty" jsonschema_description:"The exact substring to replace or remove (required for replace/remove)"`
	Content string `json:"content,omitempty" jsonschema_description:"The memory text to add or replace with (required for add/replace)"`
}

func (m *memoryTool) Build(scope *Scope) tool.InvokableTool {
	t, err := utils.InferTool(m.Name(), m.Desc(), func(ctx context.Context, input *MemoryInput) (string, error) {
		// Check if write_approval is enabled
		writeApproval := false
		if scope.ToolGroupCfg != nil {
			if cfgStr, err := scope.ToolGroupCfg.GetConfig(ctx, "Memory"); err == nil && cfgStr != "" {
				var tc struct {
					WriteApproval bool `json:"write_approval"`
				}
				if err := json.Unmarshal([]byte(cfgStr), &tc); err == nil {
					writeApproval = tc.WriteApproval
				}
			}
		}

		// If write approval is enabled, stage the write instead of applying it
		if writeApproval {
			if scope.StagedWriteStore == nil {
				return "Write approval is enabled but staging store is not available.", nil
			}
			id, err := scope.StagedWriteStore.StageWrite(ctx, scope.AgentName, input.Op, input.Target, input.Content)
			if err != nil {
				return "", fmt.Errorf("stage memory write for approval: %w", err)
			}
			return fmt.Sprintf("Memory write staged for approval (ID: %d). Use the approval workflow to review and apply this change.", id), nil
		}

		// Direct write path (write_approval disabled)
		limit := scope.CharLimit
		if limit <= 0 {
			limit = 3200
		}
		coreStore := memory.NewFileCoreStore(limit)
		newContent, err := coreStore.WriteCore(ctx, scope.Workspace, input.Op, input.Target, input.Content)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Successfully updated MEMORY.md. New memory state:\n%s", newContent), nil
	})
	if err != nil {
		panic(err)
	}
	return t
}

const jsonSchema = `{
  "type": "object",
  "properties": {
    "enabled": {
      "type": "boolean",
      "default": true,
      "description": "Enable or disable agent long-term memory"
    },
    "embedding_provider": {
      "type": "string",
      "default": "",
      "description": "Provider name for embeddings (e.g. openai, ollama, gemini)"
    },
    "embedding_model": {
      "type": "string",
      "default": "",
      "description": "Model name for embeddings (e.g. text-embedding-3-small, text-embedding-004)"
    },
    "char_limit": {
      "type": "integer",
      "default": 3200,
      "description": "Curated memory character limit (default 3200)"
    },
    "write_approval": {
      "type": "boolean",
      "default": false,
      "description": "Require human approval before writing to MEMORY.md (stages writes for review)"
    },
    "fts_weight": {
      "type": "number",
      "default": 0.3,
      "description": "FTS similarity weight for hybrid search"
    },
    "vector_weight": {
      "type": "number",
      "default": 0.7,
      "description": "Vector similarity weight for hybrid search"
    }
  }
}`

var (
	configMu sync.Mutex
	lastCfg  = `{"enabled":true,"embedding_provider":"","embedding_model":"","char_limit":3200,"fts_weight":0.3,"vector_weight":0.7}`
)

func init() {
	Register(&memorySearchTool{})
	Register(&sessionSearchTool{})
	Register(&memoryTool{})

	RegisterConfig("Memory", jsonSchema, func(ctx context.Context, cfg string) error {
		configMu.Lock()
		lastCfg = cfg
		configMu.Unlock()
		return nil
	}, func(ctx context.Context) (string, error) {
		configMu.Lock()
		defer configMu.Unlock()
		return lastCfg, nil
	})
}
