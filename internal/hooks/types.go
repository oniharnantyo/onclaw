package hooks

// Event defines the type of hook event.
type Event string

const (
	EventSessionStart     Event = "session_start"
	EventUserPromptSubmit Event = "user_prompt_submit"
	EventPreToolUse       Event = "pre_tool_use"
	EventPostToolUse      Event = "post_tool_use"
	EventStop             Event = "stop"
)

// Decision defines the outcome of a blocking hook evaluation.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionBlock Decision = "block"
)

// Payload represents the JSON event payload passed to hook handlers.
type Payload struct {
	Event      Event                  `json:"event"`
	Channel    string                 `json:"channel"`
	Agent      string                 `json:"agent"`
	SessionID  string                 `json:"session_id,omitempty"`
	Prompt     string                 `json:"prompt,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolArgs   map[string]interface{} `json:"tool_args,omitempty"`
	ToolResult string                 `json:"tool_result,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// CommandConfig represents configuration for the command handler.
type CommandConfig struct {
	Command        string   `json:"command"`
	Cwd            string   `json:"cwd,omitempty"`
	AllowedEnvVars []string `json:"allowed_env_vars,omitempty"`
}

// ScriptConfig represents configuration for the script handler.
type ScriptConfig struct {
	Script string `json:"script"`
}
