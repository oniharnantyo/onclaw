package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysbrowser "github.com/oniharnantyo/onclaw/internal/browser"
	_ "github.com/oniharnantyo/onclaw/internal/browser/cdp"
)

const jsonSchema = `{
  "type": "object",
  "properties": {
    "engine": {
      "type": "string",
      "enum": ["lightpanda", "chromium", "remote"],
      "default": "lightpanda",
      "description": "The browser rendering engine to use"
    },
    "headless": {
      "type": "boolean",
      "default": true,
      "description": "Run Chromium in headless mode"
    },
    "lightpanda": {
      "type": "object",
      "properties": {
        "binPath": {
          "type": "string",
          "description": "Path to lightpanda binary"
        },
        "port": {
          "type": "integer",
          "default": 9222,
          "description": "CDP port for lightpanda"
        }
      }
    },
    "chromium": {
      "type": "object",
      "properties": {
        "binPath": {
          "type": "string",
          "description": "Path to Chromium/Chrome binary"
        }
      }
    },
    "remote": {
      "type": "object",
      "properties": {
        "url": {
          "type": "string",
          "description": "HTTP URL of the remote CDP host, e.g. http://127.0.0.1:9222"
        }
      }
    }
  },
  "required": ["engine"]
}`

var (
	configMu sync.Mutex
	lastCfg  = `{"engine":"lightpanda","headless":true,"lightpanda":{"port":9222}}`
)

func init() {
	tools.RegisterConfig("Browser", jsonSchema, func(ctx context.Context, cfg string) error {
		var temp sysbrowser.Config
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
