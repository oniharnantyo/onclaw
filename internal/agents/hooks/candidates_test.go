package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeToolSource is a registry-ish value source: 24 workspace-visible tools
// including the 4-tool browser family and 2 GitHub MCP tools.
type fakeToolSource struct {
	tools    []string
	err      error
	lastWSID string
}

func (f *fakeToolSource) VisibleToolNames(_ context.Context, workspaceID string) ([]string, error) {
	f.lastWSID = workspaceID
	return f.tools, f.err
}

func newFakeToolSource() *fakeToolSource {
	return &fakeToolSource{tools: []string{
		"browser.snapshot", "browser.click", "browser.navigate", "browser.screenshot",
		"web.fetch", "web.search",
		"shell.run", "shell.exec",
		"files.write", "files.read",
		"memory.write",
		"grafana.query",
		"mcp__github__create_issue", "mcp__github__list_prs",
		"mcp__gitlab__create_issue",
		"slack.post",
		"calendar.create",
		"db.query",
		"http.request",
		"cron.trigger",
		"search.index",
		"translate.text",
		"summarize.text",
		"vector.embed",
	}}
}

func TestMatchedValue(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		want string
	}{
		{
			name: "pre_tool_use uses the tool name",
			ev:   Event{Event: "pre_tool_use", Origin: "user", Tool: &EventTool{Name: "shell.run"}},
			want: "shell.run",
		},
		{
			name: "post_tool_use uses the tool name",
			ev:   Event{Event: "post_tool_use", Tool: &EventTool{Name: "web.fetch"}},
			want: "web.fetch",
		},
		{
			name: "pre_tool_use without tool detail",
			ev:   Event{Event: "pre_tool_use"},
			want: "",
		},
		{
			name: "run_started uses the origin",
			ev:   Event{Event: "run_started", Origin: "scheduler"},
			want: "scheduler",
		},
		{
			name: "user_prompt_submit uses the origin",
			ev:   Event{Event: "user_prompt_submit", Origin: "channel"},
			want: "channel",
		},
		{
			name: "run_finished uses the status",
			ev:   Event{Event: "run_finished", Status: "failed"},
			want: "failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchedValue(tt.ev); got != tt.want {
				t.Errorf("MatchedValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOriginValuesAndStatusValues(t *testing.T) {
	if got, want := OriginValues(), []string{"user", "scheduler", "channel", "telegram", "heartbeat"}; len(got) != len(want) {
		t.Errorf("OriginValues() = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("OriginValues()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	}
	if got, want := StatusValues(), []string{"completed", "failed", "cancelled"}; len(got) != len(want) {
		t.Errorf("StatusValues() = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("StatusValues()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	}
}

func TestCountMatches(t *testing.T) {
	src := newFakeToolSource()
	tests := []struct {
		name        string
		event       domain.HookEvent
		matcher     string
		src         ToolValueSource
		wantMatched int
		wantTotal   int
		wantErr     bool
	}{
		{
			name:        "browser family selects 4 of 24 tools",
			event:       domain.HookEventPreToolUse,
			matcher:     "browser.*",
			src:         src,
			wantMatched: 4,
			wantTotal:   24,
		},
		{
			name:        "match-all selects the whole toolset",
			event:       domain.HookEventPostToolUse,
			matcher:     "",
			src:         src,
			wantMatched: 24,
			wantTotal:   24,
		},
		{
			name:        "mcp family selects its server's tools",
			event:       domain.HookEventPreToolUse,
			matcher:     "mcp__github.*",
			src:         src,
			wantMatched: 2,
			wantTotal:   24,
		},
		{
			name:        "comma-separated list selects the named tools",
			event:       domain.HookEventPreToolUse,
			matcher:     "web.fetch, shell.run, files.write",
			src:         src,
			wantMatched: 3,
			wantTotal:   24,
		},
		{
			name:        "unanchored regex over the toolset",
			event:       domain.HookEventPreToolUse,
			matcher:     "^web",
			src:         src,
			wantMatched: 2,
			wantTotal:   24,
		},
		{
			name:        "run_started counts against the origin enum",
			event:       domain.HookEventRunStarted,
			matcher:     "scheduler",
			src:         nil,
			wantMatched: 1,
			wantTotal:   5,
		},
		{
			name:        "user_prompt_submit list tier counts against the origin enum",
			event:       domain.HookEventUserPromptSubmit,
			matcher:     "user|channel",
			src:         nil,
			wantMatched: 2,
			wantTotal:   5,
		},
		{
			name:        "telegram origin counts against the origin enum (integrate-telegram-gateway 6.3)",
			event:       domain.HookEventRunStarted,
			matcher:     "telegram",
			src:         nil,
			wantMatched: 1,
			wantTotal:   5,
		},
		{
			name:        "heartbeat origin counts against the origin enum (add-agent-heartbeat D11)",
			event:       domain.HookEventUserPromptSubmit,
			matcher:     "heartbeat",
			src:         nil,
			wantMatched: 1,
			wantTotal:   5,
		},
		{
			name:        "run_finished counts against the status enum",
			event:       domain.HookEventRunFinished,
			matcher:     "failed",
			src:         nil,
			wantMatched: 1,
			wantTotal:   3,
		},
		{
			name:        "tool event without a source errors",
			event:       domain.HookEventPreToolUse,
			matcher:     "",
			src:         nil,
			wantMatched: 0,
			wantTotal:   0,
			wantErr:     true,
		},
		{
			name:        "uncompilable matcher errors",
			event:       domain.HookEventPreToolUse,
			matcher:     "(",
			src:         src,
			wantMatched: 0,
			wantTotal:   0,
			wantErr:     true,
		},
		{
			name:        "unknown event errors",
			event:       domain.HookEvent("run_paused"),
			matcher:     "",
			src:         src,
			wantMatched: 0,
			wantTotal:   0,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, total, err := CountMatches(context.Background(), tt.event, tt.src, "ws-1", tt.matcher)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CountMatches() err = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("CountMatches(): %v", err)
			}
			if matched != tt.wantMatched || total != tt.wantTotal {
				t.Errorf("CountMatches() = %d of %d, want %d of %d", matched, total, tt.wantMatched, tt.wantTotal)
			}
		})
	}
}

func TestCountMatches_PassesWorkspaceScopeAndSurfacesSourceError(t *testing.T) {
	src := &fakeToolSource{err: errors.New("registry unavailable")}
	if _, _, err := CountMatches(context.Background(), domain.HookEventPreToolUse, src, "ws-42", ""); err == nil {
		t.Fatal("source error must surface")
	}
	if src.lastWSID != "ws-42" {
		t.Errorf("source called with workspace %q, want ws-42", src.lastWSID)
	}
}
