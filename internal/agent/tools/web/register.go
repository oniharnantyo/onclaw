package web

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysweb "github.com/oniharnantyo/onclaw/internal/web"
	_ "github.com/oniharnantyo/onclaw/internal/web/ddg"
	_ "github.com/oniharnantyo/onclaw/internal/web/exa"
	_ "github.com/oniharnantyo/onclaw/internal/web/google"
	_ "github.com/oniharnantyo/onclaw/internal/web/http"
	_ "github.com/oniharnantyo/onclaw/internal/web/lightpanda"
	_ "github.com/oniharnantyo/onclaw/internal/web/tavily"
)

const jsonSchema = `{
  "type": "object",
  "properties": {
    "search_provider": {
      "type": "string",
      "enum": ["duckduckgo", "tavily", "exa", "google"],
      "default": "duckduckgo",
      "description": "Preferred search provider backend"
    },
    "fetch_provider": {
      "type": "string",
      "enum": ["http", "lightpanda"],
      "default": "http",
      "description": "Preferred fetch provider backend"
    },
    "user_agent": {
      "type": "string",
      "description": "HTTP User-Agent header"
    },
    "timeout_seconds": {
      "type": "integer",
      "default": 10,
      "description": "HTTP request timeout in seconds"
    },
    "max_bytes": {
      "type": "integer",
      "default": 1048576,
      "description": "Max allowed response size in bytes"
    },
    "google_cx": {
      "type": "string",
      "description": "Google Custom Search Engine ID (CX)"
    },
    "lightpanda_bin_path": {
      "type": "string",
      "default": "lightpanda",
      "description": "Path to lightpanda binary"
    }
  },
  "required": ["search_provider", "fetch_provider"]
}`

var (
	configMu sync.Mutex
	lastCfg  = `{"search_provider":"duckduckgo","fetch_provider":"http","timeout_seconds":10,"max_bytes":1048576,"lightpanda_bin_path":"lightpanda"}`
)

func init() {
	tools.RegisterConfig("Web", jsonSchema, func(ctx context.Context, cfg string) error {
		var temp sysweb.Config
		if err := json.Unmarshal([]byte(cfg), &temp); err != nil {
			return err
		}
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
