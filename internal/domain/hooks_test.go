package domain_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func validHookBase() domain.HookBase {
	return domain.HookBase{
		Name:        "Gate prod deploys",
		Event:       domain.HookEventPreToolUse,
		HandlerType: domain.HookHandlerHTTP,
		Config:      json.RawMessage(`{"url":"https://hooks.example.com/onclaw"}`),
		TimeoutMS:   domain.DefaultHookTimeoutMS,
		OnFailure:   domain.HookFailureAllow,
		Enabled:     true,
	}
}

func TestValidateHook(t *testing.T) {
	tests := []struct {
		name        string
		level       domain.HookLevel
		mutate      func(base *domain.HookBase)
		wantErr     bool
		errContains string
	}{
		{
			name:  "valid http hook",
			level: domain.HookLevelWorkspace,
		},
		{
			name:  "empty on_failure defaults to allow",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.OnFailure = ""
			},
		},
		{
			name:  "block failure policy",
			level: domain.HookLevelAgent,
			mutate: func(base *domain.HookBase) {
				base.OnFailure = domain.HookFailureBlock
			},
		},
		{
			name:  "max timeout is accepted",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.TimeoutMS = domain.MaxHookTimeoutMS
			},
		},
		{
			name:  "one millisecond timeout is accepted",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.TimeoutMS = 1
			},
		},
		{
			name:        "nil base",
			level:       domain.HookLevelWorkspace,
			mutate:      nil,
			wantErr:     true,
			errContains: "",
		},
		{
			name:        "unknown level",
			level:       "galaxy",
			mutate:      func(base *domain.HookBase) {},
			wantErr:     true,
			errContains: "level",
		},
		{
			name:  "empty name",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.Name = ""
			},
			wantErr:     true,
			errContains: "name",
		},
		{
			name:  "blank name",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.Name = "   "
			},
			wantErr:     true,
			errContains: "name",
		},
		{
			name:  "unknown event",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.Event = "before_run"
			},
			wantErr:     true,
			errContains: `event: unknown event "before_run"`,
		},
		{
			name:  "empty event",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.Event = ""
			},
			wantErr:     true,
			errContains: "event",
		},
		{
			name:  "unknown handler type",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.HandlerType = "shell"
			},
			wantErr:     true,
			errContains: `handler_type: unknown handler type "shell"`,
		},
		{
			name:  "empty handler type",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.HandlerType = ""
			},
			wantErr:     true,
			errContains: "handler_type",
		},
		{
			name:  "unknown failure policy",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.OnFailure = "abort"
			},
			wantErr:     true,
			errContains: "on_failure",
		},
		{
			name:  "zero timeout",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.TimeoutMS = 0
			},
			wantErr:     true,
			errContains: "timeout_ms",
		},
		{
			name:  "negative timeout",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.TimeoutMS = -5
			},
			wantErr:     true,
			errContains: "timeout_ms",
		},
		{
			name:  "timeout over the cap",
			level: domain.HookLevelWorkspace,
			mutate: func(base *domain.HookBase) {
				base.TimeoutMS = domain.MaxHookTimeoutMS + 1
			},
			wantErr:     true,
			errContains: "timeout_ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil base" {
				err := domain.ValidateHook(tt.level, nil, "ws-1", nil)
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				return
			}
			base := validHookBase()
			if tt.mutate != nil {
				tt.mutate(&base)
			}
			err := domain.ValidateHook(tt.level, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookMatcher(t *testing.T) {
	tests := []struct {
		name        string
		matcher     string
		wantErr     bool
		errContains string
	}{
		// Match-all tier.
		{name: "empty matcher is match-all", matcher: ""},
		{name: "bare star is match-all", matcher: "*"},

		// LIST tier: every entry fits the tool-entry charset.
		{name: "single exact name", matcher: "web_fetch"},
		{name: "exact name keeps inner dot", matcher: "web.search"},
		{name: "exact name with underscores dashes digits", matcher: "mcp__github__create_issue"},
		{name: "prefix family", matcher: "browser.*"},
		{name: "mcp server family prefix", matcher: "mcp__github.*"},
		{name: "comma-separated list", matcher: "shell,read_file"},
		{name: "pipe-separated list", matcher: "shell|read_file"},
		{name: "space-separated list", matcher: "shell read_file"},
		{name: "mixed separators", matcher: "shell, read_file|web.*"},
		{name: "exact and family entries mixed", matcher: "web.search, web.*"},
		{name: "repeated separators collapse", matcher: "shell,,read_file||  web.*"},
		{name: "separator-only matcher is an empty list", matcher: ",,|| "},
		{
			name:    "list tier has no length cap",
			matcher: strings.Repeat("tool_name.", 40),
		},

		// Regex tier: any non-charset character moves the WHOLE unsplit
		// string into the regex tier.
		{name: "anchored regex", matcher: "^web\\."},
		{name: "one bad entry moves the whole string to regex tier", matcher: "shell|^web\\."},
		{name: "mid-string wildcard reads as regex", matcher: "a.*b"},
		{name: "bare trailing star reads as regex", matcher: "mcp__github__*"},
		{name: "character class reads as regex", matcher: "^(shell|exec)$"},
		{name: "max length regex", matcher: strings.Repeat("a*", domain.MaxHookPatternLength/2)},
		{
			name:        "over-long regex",
			matcher:     "(" + strings.Repeat("a", domain.MaxHookPatternLength),
			wantErr:     true,
			errContains: "matcher: exceeds maximum length",
		},
		{
			name:        "uncompilable regex",
			matcher:     "(unclosed",
			wantErr:     true,
			errContains: "matcher: error parsing regexp",
		},
		{
			name:        "bad entry with uncompilable whole-string regex",
			matcher:     "shell, [unclosed",
			wantErr:     true,
			errContains: "matcher: error parsing regexp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.Matcher = tt.matcher
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookHTTPConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantErr     bool
		errContains string
	}{
		{name: "valid https url", config: `{"url":"https://hooks.example.com/onclaw"}`},
		{name: "valid http url", config: `{"url":"http://hooks.internal:8080/x"}`},
		{
			name:        "missing url",
			config:      `{}`,
			wantErr:     true,
			errContains: "config.url",
		},
		{
			name:        "empty config object",
			config:      ``,
			wantErr:     true,
			errContains: "config.url",
		},
		{
			name:        "non-string url",
			config:      `{"url":8080}`,
			wantErr:     true,
			errContains: "config.url",
		},
		{
			name:        "non-http scheme",
			config:      `{"url":"ftp://hooks.example.com/x"}`,
			wantErr:     true,
			errContains: "scheme must be http or https",
		},
		{
			name:        "url without host",
			config:      `{"url":"http:///path"}`,
			wantErr:     true,
			errContains: "config.url",
		},
		{
			name:   "valid headers",
			config: `{"url":"https://hooks.example.com","headers":[{"name":"Authorization","value":"Bearer tok"},{"name":"X-Hook-Delivery"}]}`,
		},
		{
			name:        "header name with underscore",
			config:      `{"url":"https://hooks.example.com","headers":[{"name":"bad_name","value":"x"}]}`,
			wantErr:     true,
			errContains: "config.headers",
		},
		{
			name:        "header name with space",
			config:      `{"url":"https://hooks.example.com","headers":[{"name":"bad name","value":"x"}]}`,
			wantErr:     true,
			errContains: "config.headers",
		},
		{
			name:        "header value not a string",
			config:      `{"url":"https://hooks.example.com","headers":[{"name":"X-Count","value":3}]}`,
			wantErr:     true,
			errContains: "config.headers",
		},
		{
			name:        "headers not an array",
			config:      `{"url":"https://hooks.example.com","headers":{"name":"X"}}`,
			wantErr:     true,
			errContains: "config.headers",
		},
		{
			name:        "config is an array",
			config:      `[]`,
			wantErr:     true,
			errContains: "config: must be a JSON object",
		},
		{
			name:        "config is a scalar",
			config:      `"webhook"`,
			wantErr:     true,
			errContains: "config: must be a JSON object",
		},
		{
			name:        "null config falls through to missing url",
			config:      `null`,
			wantErr:     true,
			errContains: "config.url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.Config = json.RawMessage(tt.config)
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookCommandConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantErr     bool
		errContains string
	}{
		{name: "valid command with args and env", config: `{"command":"/usr/local/bin/gate","args":["--strict"],"env":[{"name":"HOOK_TOKEN","value":"t"}]}`},
		{name: "valid command without args or env", config: `{"command":"gate"}`},
		{name: "empty args array", config: `{"command":"gate","args":[]}`},
		{name: "empty env array", config: `{"command":"gate","env":[]}`},
		{
			name:        "missing command",
			config:      `{"args":["--x"]}`,
			wantErr:     true,
			errContains: "config.command",
		},
		{
			name:        "blank command",
			config:      `{"command":"  "}`,
			wantErr:     true,
			errContains: "config.command",
		},
		{
			name:        "non-string command",
			config:      `{"command":true}`,
			wantErr:     true,
			errContains: "config.command",
		},
		{
			name:        "args not an array",
			config:      `{"command":"gate","args":"--strict"}`,
			wantErr:     true,
			errContains: "config.args",
		},
		{
			name:        "args with non-string element",
			config:      `{"command":"gate","args":["--strict",2]}`,
			wantErr:     true,
			errContains: "config.args",
		},
		{
			name:        "env name starting with digit",
			config:      `{"command":"gate","env":[{"name":"1BAD","value":"x"}]}`,
			wantErr:     true,
			errContains: "config.env",
		},
		{
			name:        "env name with dash",
			config:      `{"command":"gate","env":[{"name":"BAD-NAME","value":"x"}]}`,
			wantErr:     true,
			errContains: "config.env",
		},
		{
			name:        "env value not a string",
			config:      `{"command":"gate","env":[{"name":"OK_NAME","value":7}]}`,
			wantErr:     true,
			errContains: "config.env",
		},
		{
			name:        "env row not an object",
			config:      `{"command":"gate","env":["HOOK_TOKEN"]}`,
			wantErr:     true,
			errContains: "config.env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.HandlerType = domain.HookHandlerCommand
			base.Config = json.RawMessage(tt.config)
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookMCPConfig(t *testing.T) {
	validConfig := `{"server":"srv-1","tool":"notify"}`

	lookupTrue := func(workspaceID, serverID string) (bool, error) {
		if workspaceID != "ws-1" || serverID != "srv-1" {
			t.Errorf("lookup called with workspaceID=%q serverID=%q, want ws-1/srv-1", workspaceID, serverID)
		}
		return true, nil
	}
	lookupFalse := func(workspaceID, serverID string) (bool, error) { return false, nil }
	lookupFails := func(workspaceID, serverID string) (bool, error) { return false, errors.New("db down") }

	tests := []struct {
		name        string
		config      string
		lookup      domain.MCPServerExistsFunc
		wantErr     bool
		errContains string
	}{
		{name: "valid with existing server", config: validConfig, lookup: lookupTrue},
		{name: "nil lookup skips existence check", config: validConfig, lookup: nil},
		{
			name:        "server not found",
			config:      validConfig,
			lookup:      lookupFalse,
			wantErr:     true,
			errContains: "config.server",
		},
		{
			name:    "lookup error propagates",
			config:  validConfig,
			lookup:  lookupFails,
			wantErr: false, // not ErrInvalid; asserted separately below
		},
		{
			name:        "missing server",
			config:      `{"tool":"notify"}`,
			lookup:      lookupTrue,
			wantErr:     true,
			errContains: "config.server",
		},
		{
			name:        "missing tool",
			config:      `{"server":"srv-1"}`,
			lookup:      lookupTrue,
			wantErr:     true,
			errContains: "config.tool",
		},
		{
			name:        "non-string tool",
			config:      `{"server":"srv-1","tool":9}`,
			lookup:      lookupTrue,
			wantErr:     true,
			errContains: "config.tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.HandlerType = domain.HookHandlerMCPTool
			base.Config = json.RawMessage(tt.config)
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", tt.lookup)
			if tt.name == "lookup error propagates" {
				if err == nil || errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected propagated non-ErrInvalid error, got %v", err)
				}
				if !strings.Contains(err.Error(), "mcp server lookup") {
					t.Fatalf("expected lookup error context, got %q", err.Error())
				}
				return
			}
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookMCPConfigInstanceSkipsLookup(t *testing.T) {
	neverCalled := func(workspaceID, serverID string) (bool, error) {
		t.Fatal("instance-level validation must not invoke the mcp lookup")
		return false, nil
	}
	base := validHookBase()
	base.HandlerType = domain.HookHandlerMCPTool
	base.Config = json.RawMessage(`{"server":"srv-1","tool":"notify"}`)
	if err := domain.ValidateHook(domain.HookLevelInstance, &base, "", neverCalled); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateHookPromptConfig(t *testing.T) {
	tests := []struct {
		name        string
		matcher     string
		config      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid list-tier matcher with provider and model",
			matcher: "shell",
			config:  `{"provider":"openai","model":"gpt-4.1-mini","prompt":"Evaluate the call."}`,
		},
		{
			name:    "valid regex-tier matcher",
			matcher: "^shell|^exec",
			config:  `{"provider":"openai","model":"gpt-4.1-mini"}`,
		},
		{
			name:    "valid with invocation cap",
			matcher: "shell, exec",
			config:  `{"provider":"openai","model":"gpt-4.1-mini","max_invocations_per_run":3}`,
		},
		{
			name:        "empty matcher rejected",
			matcher:     "",
			config:      `{"provider":"openai","model":"gpt-4.1-mini"}`,
			wantErr:     true,
			errContains: "matcher: prompt hooks require a non-match-all matcher",
		},
		{
			name:        "star matcher rejected",
			matcher:     "*",
			config:      `{"provider":"openai","model":"gpt-4.1-mini"}`,
			wantErr:     true,
			errContains: `matcher: prompt hooks require a non-match-all matcher`,
		},
		{
			name:        "missing provider",
			matcher:     "shell",
			config:      `{"model":"gpt-4.1-mini"}`,
			wantErr:     true,
			errContains: "config.provider",
		},
		{
			name:        "blank model",
			matcher:     "shell",
			config:      `{"provider":"openai","model":" "}`,
			wantErr:     true,
			errContains: "config.model",
		},
		{
			name:        "non-string model",
			matcher:     "shell",
			config:      `{"provider":"openai","model":["gpt-4.1-mini"]}`,
			wantErr:     true,
			errContains: "config.model",
		},
		{
			name:        "zero invocation cap",
			matcher:     "shell",
			config:      `{"provider":"openai","model":"gpt-4.1-mini","max_invocations_per_run":0}`,
			wantErr:     true,
			errContains: "config.max_invocations_per_run",
		},
		{
			name:        "negative invocation cap",
			matcher:     "shell",
			config:      `{"provider":"openai","model":"gpt-4.1-mini","max_invocations_per_run":-1}`,
			wantErr:     true,
			errContains: "config.max_invocations_per_run",
		},
		{
			name:        "non-integer invocation cap",
			matcher:     "shell",
			config:      `{"provider":"openai","model":"gpt-4.1-mini","max_invocations_per_run":2.5}`,
			wantErr:     true,
			errContains: "config.max_invocations_per_run",
		},
		{
			name:        "string invocation cap",
			matcher:     "shell",
			config:      `{"provider":"openai","model":"gpt-4.1-mini","max_invocations_per_run":"5"}`,
			wantErr:     true,
			errContains: "config.max_invocations_per_run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.Matcher = tt.matcher
			base.HandlerType = domain.HookHandlerPrompt
			base.Config = json.RawMessage(tt.config)
			base.TimeoutMS = domain.DefaultPromptHookTimeoutMS
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestValidateHookScriptConfig(t *testing.T) {
	oversized := strings.Repeat("x", domain.MaxHookScriptBytes+1)
	tests := []struct {
		name        string
		matcher     string
		config      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid script with match-all matcher",
			matcher: "",
			config:  `{"script":"return {decision:'allow'}"}`,
		},
		{
			name:    "valid script with star matcher",
			matcher: "*",
			config:  `{"script":"return input.origin === 'cron'"}`,
		},
		{
			name:        "missing script",
			matcher:     "",
			config:      `{}`,
			wantErr:     true,
			errContains: "config.script: is required",
		},
		{
			name:        "blank script",
			matcher:     "",
			config:      `{"script":"  "}`,
			wantErr:     true,
			errContains: "config.script: is required",
		},
		{
			name:        "empty script",
			matcher:     "",
			config:      `{"script":""}`,
			wantErr:     true,
			errContains: "config.script: is required",
		},
		{
			name:        "non-string script",
			matcher:     "",
			config:      `{"script":["return true"]}`,
			wantErr:     true,
			errContains: "config.script: must be a string",
		},
		{
			name:        "oversized script",
			matcher:     "",
			config:      `{"script":"` + oversized + `"}`,
			wantErr:     true,
			errContains: fmt.Sprintf("config.script: %d bytes exceeds maximum of %d", domain.MaxHookScriptBytes+1, domain.MaxHookScriptBytes),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := validHookBase()
			base.Matcher = tt.matcher
			base.HandlerType = domain.HookHandlerScript
			base.Config = json.RawMessage(tt.config)
			err := domain.ValidateHook(domain.HookLevelWorkspace, &base, "ws-1", nil)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestInstanceHookValidate(t *testing.T) {
	var nilHook *domain.InstanceHook
	if err := nilHook.Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil receiver should be ErrInvalid, got %v", err)
	}

	valid := &domain.InstanceHook{
		HookBase: validHookBase(),
		Key:      "block-rm-rf",
		Source:   domain.HookSourceBuiltin,
		Version:  1,
	}
	if err := valid.Validate(nil); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	noKey := *valid
	noKey.Key = "  "
	if err := (&noKey).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank key should be ErrInvalid, got %v", err)
	}

	badSource := *valid
	badSource.Source = "uploaded"
	if err := (&badSource).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown source should be ErrInvalid, got %v", err)
	}

	zeroVersion := *valid
	zeroVersion.Version = 0
	if err := (&zeroVersion).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("zero version should be ErrInvalid, got %v", err)
	}

	invalidBase := *valid
	invalidBase.Event = "not-an-event"
	if err := (&invalidBase).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid base should be ErrInvalid, got %v", err)
	}
}

func TestWorkspaceHookValidate(t *testing.T) {
	var nilHook *domain.WorkspaceHook
	if err := nilHook.Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil receiver should be ErrInvalid, got %v", err)
	}

	valid := &domain.WorkspaceHook{
		HookBase:    validHookBase(),
		WorkspaceID: "ws-1",
	}
	if err := valid.Validate(nil); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	noWorkspace := *valid
	noWorkspace.WorkspaceID = ""
	if err := (&noWorkspace).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty workspace id should be ErrInvalid, got %v", err)
	}
}

func TestAgentHookValidate(t *testing.T) {
	var nilHook *domain.AgentHook
	if err := nilHook.Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil receiver should be ErrInvalid, got %v", err)
	}

	valid := &domain.AgentHook{
		HookBase:    validHookBase(),
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
	}
	if err := valid.Validate(nil); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	noAgent := *valid
	noAgent.AgentID = ""
	if err := (&noAgent).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty agent id should be ErrInvalid, got %v", err)
	}

	noWorkspace := *valid
	noWorkspace.WorkspaceID = ""
	if err := (&noWorkspace).Validate(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty workspace id should be ErrInvalid, got %v", err)
	}
}
