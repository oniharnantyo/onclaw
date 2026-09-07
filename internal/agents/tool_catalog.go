package agents

import (
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
)

// BrowserToolAlias is the facade allowlist name that expands to the full
// browser tool set at resolution (design.md D2). Legacy individual browser.*
// names keep resolving for backward compatibility.
const BrowserToolAlias = "browser"

// ConfigFieldType enumerates the config field kinds the settings dialog
// renders. secret fields are write-only: writes persist ciphertext, reads
// return a hint.
type ConfigFieldType string

const (
	ConfigFieldSecret  ConfigFieldType = "secret"
	ConfigFieldText    ConfigFieldType = "text"
	ConfigFieldNumber  ConfigFieldType = "number"
	ConfigFieldBoolean ConfigFieldType = "boolean"
	ConfigFieldEnum    ConfigFieldType = "enum"
)

// ConfigFieldOption is one choice of an enum config field.
type ConfigFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigField describes one structured config property of a configurable
// tool. The settings dialog renders it generically — a newly configurable
// tool needs no frontend edit.
type ConfigField struct {
	Key      string              `json:"key"`
	Label    string              `json:"label"`
	Type     ConfigFieldType     `json:"type"`
	Required bool                `json:"required"`
	Help     string              `json:"help,omitempty"`
	Default  any                 `json:"default,omitempty"`
	Options  []ConfigFieldOption `json:"options,omitempty"`
}

// ToolCatalogEntry is the metadata record for one catalog tool: the surface
// shown by the agent dialog chips and the settings Tools pane. Keys are
// allowlist names; fs middleware tools and the reserved execute name have no
// registry constructors but still appear.
type ToolCatalogEntry struct {
	Key          string        `json:"key"`
	DisplayName  string        `json:"display_name"`
	Description  string        `json:"description"`
	Group        string        `json:"group"`
	IconKey      string        `json:"icon_key"`
	Configurable bool          `json:"configurable"`
	ConfigSchema []ConfigField `json:"config_schema,omitempty"`
}

// ToolCatalog returns the full tool catalog: every registry tool, the six fs
// middleware tools, the reserved execute name, and the browser facade alias.
// The order is stable (filesystem, shell, web, browser) so surfaces render
// deterministically.
func ToolCatalog() []ToolCatalogEntry {
	return []ToolCatalogEntry{
		{
			Key:         "ls",
			DisplayName: "List Files",
			Description: "List directory contents in the agent workspace.",
			Group:       "filesystem",
			IconKey:     "folder",
		},
		{
			Key:         "read_file",
			DisplayName: "Read File",
			Description: "Read file contents, including images and PDFs.",
			Group:       "filesystem",
			IconKey:     "file",
		},
		{
			Key:         "write_file",
			DisplayName: "Write File",
			Description: "Create or overwrite files in the agent workspace.",
			Group:       "filesystem",
			IconKey:     "file-plus",
		},
		{
			Key:         "edit_file",
			DisplayName: "Edit File",
			Description: "Make targeted edits to existing files.",
			Group:       "filesystem",
			IconKey:     "edit",
		},
		{
			Key:         "glob",
			DisplayName: "Glob",
			Description: "Find files by name pattern.",
			Group:       "filesystem",
			IconKey:     "scan",
		},
		{
			Key:         "grep",
			DisplayName: "Grep",
			Description: "Search file contents by pattern.",
			Group:       "filesystem",
			IconKey:     "compass",
		},
		{
			Key:         ReservedShellTool,
			DisplayName: "Shell",
			Description: "Run shell commands inside the agent workspace jail.",
			Group:       "shell",
			IconKey:     "terminal",
		},
		{
			Key:          tools.Name,
			DisplayName:  "Web Search",
			Description:  "Search the web and return top results.",
			Group:        "web",
			IconKey:      "search",
			Configurable: true,
			ConfigSchema: webSearchConfigSchema(),
		},
		{
			Key:         tools.NameWebFetch,
			DisplayName: "Web Fetch",
			Description: "Fetch a URL and return its readable content.",
			Group:       "web",
			IconKey:     "link",
		},
		{
			Key:          BrowserToolAlias,
			DisplayName:  "Browser",
			Description:  "Drive a real browser: navigate, read, screenshot, and ref-targeted page actions.",
			Group:        "browser",
			IconKey:      "globe",
			Configurable: true,
			ConfigSchema: browserConfigSchema(),
		},
	}
}

// ToolCatalogEntryByKey returns the catalog entry for a tool key.
func ToolCatalogEntryByKey(key string) (ToolCatalogEntry, bool) {
	for _, entry := range ToolCatalog() {
		if entry.Key == key {
			return entry, true
		}
	}
	return ToolCatalogEntry{}, false
}

// webSearchConfigSchema describes the web.search workspace config. The
// credential field's requiredness is dynamic — it follows the chosen
// provider's credential kind — so the static schema marks both credential
// fields optional and server-side validation enforces the pairing.
func webSearchConfigSchema() []ConfigField {
	options := make([]ConfigFieldOption, 0, 8)
	for _, p := range tools.SearchProviders() {
		options = append(options, ConfigFieldOption{Value: p.ID, Label: p.Label})
	}
	return []ConfigField{
		{
			Key:      "provider",
			Label:    "Provider",
			Type:     ConfigFieldEnum,
			Required: true,
			Help:     "Which search backend this workspace uses.",
			Options:  options,
		},
		{
			Key:   "api_key",
			Label: "API key",
			Type:  ConfigFieldSecret,
			Help:  "Required for tavily, brave, exa, perplexity, and firecrawl. Stored encrypted; never shown again.",
		},
		{
			Key:   "base_url",
			Label: "Base URL",
			Type:  ConfigFieldText,
			Help:  "Required for searxng — the instance root, e.g. http://searxng:8080.",
		},
	}
}

// browserConfigSchema describes the browser workspace config.
func browserConfigSchema() []ConfigField {
	return []ConfigField{
		{
			Key:     "headless",
			Label:   "Headless",
			Type:    ConfigFieldBoolean,
			Default: true,
			Help:    "Run the browser without a visible window. Ignored when a remote CDP URL is set.",
		},
		{
			Key:   "remote_cdp_url",
			Label: "Remote CDP URL",
			Type:  ConfigFieldText,
			Help:  "Connect to a remote browser over CDP instead of launching one locally.",
		},
		{
			Key:     "max_pages",
			Label:   "Max pages",
			Type:    ConfigFieldNumber,
			Default: 3,
			Help:    "Maximum pages open at once per execution session.",
		},
		{
			Key:   "idle_timeout_seconds",
			Label: "Idle timeout (seconds)",
			Type:  ConfigFieldNumber,
			Help:  "Close browser sessions after this many seconds of inactivity. Empty keeps the default.",
		},
		{
			Key:   "action_timeout_seconds",
			Label: "Action timeout (seconds)",
			Type:  ConfigFieldNumber,
			Help:  "Bound each browser tool call to this many seconds. Empty keeps the default.",
		},
	}
}

// SecretConfigFields returns the schema keys whose values are secrets for the
// given entry.
func SecretConfigFields(entry ToolCatalogEntry) []string {
	keys := make([]string, 0, len(entry.ConfigSchema))
	for _, field := range entry.ConfigSchema {
		if field.Type == ConfigFieldSecret {
			keys = append(keys, field.Key)
		}
	}
	return keys
}

// FieldError is one offending config field with a client-safe message.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ConfigValidationError collects every offending config field from a
// validation pass; HTTP surfaces it as 422 with per-field details.
type ConfigValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (e *ConfigValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "invalid tool configuration"
	}
	return e.Errors[0].Message
}

func (e *ConfigValidationError) add(field, message string) {
	e.Errors = append(e.Errors, FieldError{Field: field, Message: message})
}

// ValidateToolConfig checks a config map against a catalog entry's schema:
// unknown keys are rejected, values must match their field type, and fields
// statically marked required must be present. All offending fields are
// collected into a *ConfigValidationError (nil when the config is valid).
// Dynamic requiredness (a credential field required only for some providers)
// is validated by ValidateCredentialRequirements on top.
func ValidateToolConfig(entry ToolCatalogEntry, config map[string]any) error {
	schemaByKey := make(map[string]ConfigField, len(entry.ConfigSchema))
	for _, field := range entry.ConfigSchema {
		schemaByKey[field.Key] = field
	}
	ve := &ConfigValidationError{}
	for key, value := range config {
		field, known := schemaByKey[key]
		if !known {
			ve.add(key, fmt.Sprintf("config field %q is not a valid %s option", key, entry.Key))
			continue
		}
		if err := validateConfigFieldValue(entry, field, value); err != nil {
			ve.add(field.Key, err.Error())
		}
	}
	for _, field := range entry.ConfigSchema {
		if !field.Required {
			continue
		}
		if value, present := config[field.Key]; !present || isEmptyConfigValue(value) {
			ve.add(field.Key, fmt.Sprintf("config field %q is required for %s", field.Key, entry.Key))
		}
	}
	if len(ve.Errors) == 0 {
		return nil
	}
	return ve
}

func validateConfigFieldValue(entry ToolCatalogEntry, field ConfigField, value any) error {
	if isEmptyConfigValue(value) {
		return nil
	}
	switch field.Type {
	case ConfigFieldSecret, ConfigFieldText:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("config field %q must be a string", field.Key)
		}
	case ConfigFieldNumber:
		if _, ok := numericConfigValue(value); !ok {
			return fmt.Errorf("config field %q must be a number", field.Key)
		}
	case ConfigFieldBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("config field %q must be a boolean", field.Key)
		}
	case ConfigFieldEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("config field %q must be a string", field.Key)
		}
		for _, option := range field.Options {
			if option.Value == s {
				return nil
			}
		}
		return fmt.Errorf("config field %q must be one of the allowed providers", field.Key)
	}
	return nil
}

// positiveNumberConfigKeys lists config fields that must be positive numbers
// when present, regardless of the owning tool's schema (mirrors the domain's
// shared structural constraint).
var positiveNumberConfigKeys = []string{"max_pages", "idle_timeout_seconds", "action_timeout_seconds"}

// validateStructuralConfig applies the shared structural constraints every
// tool config must satisfy (positive numbers for bounded numeric fields).
func validateStructuralConfig(config map[string]any) error {
	ve := &ConfigValidationError{}
	for _, key := range positiveNumberConfigKeys {
		value, present := config[key]
		if !present || isEmptyConfigValue(value) {
			continue
		}
		number, ok := numericConfigValue(value)
		if !ok || number <= 0 {
			ve.add(key, fmt.Sprintf("config field %q must be a positive number", key))
		}
	}
	if len(ve.Errors) == 0 {
		return nil
	}
	return ve
}

func isEmptyConfigValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	default:
		return false
	}
}

// numericConfigValue coerces the numeric kinds a decoded config map may carry
// (float64 from encoding/json, fixed-width ints from in-process callers).
func numericConfigValue(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
