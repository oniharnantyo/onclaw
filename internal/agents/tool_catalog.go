package agents

import (
	"fmt"
	"strings"

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
	// ConfigFieldList marks an ordered list of structured entries, each
	// validated against the field's Items definitions.
	ConfigFieldList ConfigFieldType = "list"
)

// ConfigFieldOption is one choice of an enum config field.
type ConfigFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigFieldCondition makes a config field conditional: it renders only
// while the named sibling field currently holds Equals. The dialog evaluates
// it against live form state, so the field appears and disappears as the user
// edits the controlling field.
type ConfigFieldCondition struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
}

// ConfigField describes one structured config property of a configurable
// tool. The settings dialog renders it generically — a newly configurable
// tool needs no frontend edit.
type ConfigField struct {
	Key      string                `json:"key"`
	Label    string                `json:"label"`
	Type     ConfigFieldType       `json:"type"`
	Required bool                  `json:"required"`
	Help     string                `json:"help,omitempty"`
	Default  any                   `json:"default,omitempty"`
	Options  []ConfigFieldOption   `json:"options,omitempty"`
	ShowIf   *ConfigFieldCondition `json:"show_if,omitempty"`
	// Items declares the per-entry fields of a list field (Type list). Each
	// stored entry is a map keyed like a flat config and validated against
	// these definitions.
	Items []ConfigField `json:"items,omitempty"`
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
	// AlwaysOn marks a tool the workspace tool gate never strips: workspace
	// settings cannot disable it and the tools API rejects enabled patches.
	// The zero value keeps ordinary tools toggleable by default.
	AlwaysOn bool `json:"always_on"`
}

// ToolCatalog returns the full tool catalog: every registry tool, the six fs
// middleware tools, the reserved execute name, and the browser facade alias.
// The order is stable (filesystem, document, shell, memory, web, browser) so
// surfaces render deterministically.
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
			Key:         "delete_file",
			DisplayName: "Delete File",
			Description: "Permanently delete a file inside the agent workspace.",
			Group:       "filesystem",
			IconKey:     "trash",
		},
		{
			Key:         tools.NameDocumentRead,
			DisplayName: "Read Document",
			Description: "Convert PDF, Word, Excel, PowerPoint, and HTML documents to markdown.",
			Group:       "document",
			IconKey:     "file-text",
		},
		{
			Key:         tools.NameDocumentCreate,
			DisplayName: "Create Document",
			Description: "Generate xlsx, PDF, Word, or PowerPoint documents from structured content.",
			Group:       "document",
			IconKey:     "file-plus",
		},
		{
			Key:         ReservedShellTool,
			DisplayName: "Shell",
			Description: "Run shell commands inside the agent workspace jail.",
			Group:       "shell",
			IconKey:     "terminal",
		},
		{
			Key:         tools.NameMemory,
			DisplayName: "Memory",
			Description: "Persistent memory across conversations: read and append the user's memory and the shared workspace memory.",
			Group:       "memory",
			IconKey:     "memory",
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
		{
			Key:         ChannelToolPost,
			DisplayName: "Channel Post",
			Description: "Post a message into the channel the agent is running in. Channel runs only.",
			Group:       "channel",
			IconKey:     "message",
			AlwaysOn:    true,
		},
		{
			Key:         ChannelToolHistory,
			DisplayName: "Channel History",
			Description: "Page back through the channel's earlier messages. Channel runs only.",
			Group:       "channel",
			IconKey:     "history",
			AlwaysOn:    true,
		},
		{
			Key:         SessionToolClose,
			DisplayName: "Close Work Session",
			Description: "Close the channel's active work session with a stored summary. Facilitator runs only.",
			Group:       "channel",
			IconKey:     "check-circle",
			AlwaysOn:    true,
		},
		{
			Key:         tools.NameSchedule,
			DisplayName: "Schedule",
			Description: "Create, list, update, and delete named schedules that run the agent on a cron recurrence or a one-shot time, delivering results to its thread or a channel it belongs to.",
			Group:       "schedule",
			IconKey:     "calendar",
		},
		{
			Key:         tools.NameTodoWrite,
			DisplayName: "Write Todos",
			Description: "Maintain the session's todo list: each call replaces the full item list, restyling kept keys and deleting dropped ones, so the plan survives compaction.",
			Group:       "todos",
			IconKey:     "list-checks",
		},
		{
			Key:         tools.NameTodoRead,
			DisplayName: "Read Todos",
			Description: "Read the session's current todo list with statuses and revision.",
			Group:       "todos",
			IconKey:     "list",
		},
		{
			Key:         tools.NameUIChart,
			DisplayName: "Chart",
			Description: "Render a structured chart card in the transcript: headline label, value, delta, and a sparkline series.",
			Group:       "chart",
			IconKey:     "chart",
		},
		{
			Key:         tools.NameUITimeline,
			DisplayName: "Timeline",
			Description: "Render a structured timeline card in the transcript: an ordered sequence of settled and reference events.",
			Group:       "timeline",
			IconKey:     "timeline",
		},
		{
			Key:         tools.NameUIPreview,
			DisplayName: "Preview",
			Description: "Render a sandboxed web preview card in the transcript: a URL bar with an inline frame and reload control.",
			Group:       "preview",
			IconKey:     "eye",
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

// webSearchDefaultTimeoutSeconds is the default per-attempt request timeout
// applied to every provider entry in the chain (design.md D7).
const webSearchDefaultTimeoutSeconds = 10

// webSearchMaxTimeoutSeconds bounds the per-attempt request timeout.
const webSearchMaxTimeoutSeconds = 60

// webSearchConfigSchema describes the web.search workspace config: an ordered
// provider stack (design.md D1) plus a flat per-attempt timeout. The entries'
// credential requiredness is dynamic — it follows each entry's provider's
// credential kind — so the static schema marks credential fields optional and
// server-side validation enforces the pairing.
func webSearchConfigSchema() []ConfigField {
	options := make([]ConfigFieldOption, 0, 8)
	for _, p := range tools.SearchProviders() {
		options = append(options, ConfigFieldOption{Value: p.ID, Label: p.Label})
	}
	return []ConfigField{
		{
			Key:   "entries",
			Label: "Provider stack",
			Type:  ConfigFieldList,
			Help:  "Requests try the first three in order — first success wins. Lower entries stand by until promoted into the top three.",
			Items: []ConfigField{
				{
					Key:      "name",
					Label:    "Name",
					Type:     ConfigFieldText,
					Required: true,
					Help:     "Unique name for this provider entry, shown in transcripts and errors.",
				},
				{
					Key:      "provider",
					Label:    "Provider",
					Type:     ConfigFieldEnum,
					Required: true,
					Help:     "Which search backend this entry uses.",
					Options:  options,
				},
				{
					Key:   "api_key",
					Label: "API key",
					Type:  ConfigFieldSecret,
					Help:  "Required for tavily, brave, exa, perplexity, and firecrawl. Stored encrypted; never shown again.",
				},
				{
					Key:    "base_url",
					Label:  "Base URL",
					Type:   ConfigFieldText,
					Help:   "The SearXNG instance root, e.g. http://searxng:8080.",
					ShowIf: &ConfigFieldCondition{Field: "provider", Equals: tools.SearchProviderSearXNG},
				},
			},
		},
		{
			Key:     "request_timeout_seconds",
			Label:   "Request timeout (seconds)",
			Type:    ConfigFieldNumber,
			Default: webSearchDefaultTimeoutSeconds,
			Help:    "Bounds each provider attempt — worst case ≈ 3 × timeout. Max 60.",
		},
	}
}

// listConfigField returns the entry's declarative list field, if its schema
// has one. Only web.search carries one today; the helpers below stay generic
// so a second list-shaped tool needs no new machinery.
func listConfigField(entry ToolCatalogEntry) (ConfigField, bool) {
	for _, field := range entry.ConfigSchema {
		if field.Type == ConfigFieldList {
			return field, true
		}
	}
	return ConfigField{}, false
}

// secretItemKeys lists the list field's per-entry secret keys.
func secretItemKeys(field ConfigField) []string {
	keys := make([]string, 0, len(field.Items))
	for _, item := range field.Items {
		if item.Type == ConfigFieldSecret {
			keys = append(keys, item.Key)
		}
	}
	return keys
}

// configListItems coerces the decoded shapes a list config value may carry
// ([]any of map[string]any from encoding/json, []map[string]any from
// in-process callers) into per-entry maps. Any other shape is rejected.
func configListItems(value any) ([]map[string]any, bool) {
	switch list := value.(type) {
	case []map[string]any:
		return list, true
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, m)
		}
		return out, true
	default:
		return nil, false
	}
}

// configEntries reads a tool config's "entries" list as per-entry maps.
// Absent or malformed values read as no entries.
func configEntries(config map[string]any) []map[string]any {
	if config == nil {
		return nil
	}
	items, ok := configListItems(config["entries"])
	if !ok {
		return nil
	}
	return items
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
	case ConfigFieldList:
		return validateConfigListValue(field, value)
	}
	return nil
}

// isServerManagedItemKey reports whether a list-entry key is server-owned:
// the stable entry id assigned on first persist (design.md D1) and the
// per-secret last-4 hint fields (design.md D5). Clients echo them back but
// never set them; validation permits them without requiring them.
func isServerManagedItemKey(itemFields []ConfigField, key string) bool {
	if key == "id" {
		return true
	}
	if suffix := "_hint"; strings.HasSuffix(key, suffix) {
		base := strings.TrimSuffix(key, suffix)
		for _, item := range itemFields {
			if item.Key == base && item.Type == ConfigFieldSecret {
				return true
			}
		}
	}
	return false
}

// validateConfigListValue checks each entry of a list config value against
// the field's Items definitions: entries must be objects, keys must be
// declared (plus server-managed ones), values must match their field type,
// and statically required item fields must be present. Dynamic per-entry
// requiredness (credential following provider kind) is layered on top by the
// owning tool's validation.
func validateConfigListValue(field ConfigField, value any) error {
	items, ok := configListItems(value)
	if !ok {
		return fmt.Errorf("config field %q must be a list of objects", field.Key)
	}
	itemFields := make(map[string]ConfigField, len(field.Items))
	for _, item := range field.Items {
		itemFields[item.Key] = item
	}
	for i, entryItem := range items {
		for key, value := range entryItem {
			itemField, known := itemFields[key]
			if !known {
				if isServerManagedItemKey(field.Items, key) {
					continue
				}
				return fmt.Errorf("config field %q entry %d: %q is not a valid option", field.Key, i+1, key)
			}
			if err := validateConfigFieldValue(ToolCatalogEntry{}, itemField, value); err != nil {
				return fmt.Errorf("config field %q entry %d: %w", field.Key, i+1, err)
			}
		}
		for _, itemField := range field.Items {
			if !itemField.Required {
				continue
			}
			if value, present := entryItem[itemField.Key]; !present || isEmptyConfigValue(value) {
				return fmt.Errorf("config field %q entry %d: %q is required", field.Key, i+1, itemField.Key)
			}
		}
	}
	return nil
}

// positiveNumberConfigKeys lists config fields that must be positive numbers
// when present, regardless of the owning tool's schema (mirrors the domain's
// shared structural constraint).
var positiveNumberConfigKeys = []string{"max_pages", "idle_timeout_seconds", "action_timeout_seconds", "request_timeout_seconds"}

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
