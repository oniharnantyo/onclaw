package agent_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/render"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	sysweb "github.com/oniharnantyo/onclaw/internal/web"
)

// failingFetcher is a web fetcher that always errors, for deterministic
// terminal-failure injection in the agent-level regression test.
type failingFetcher struct{}

func (failingFetcher) Fetch(ctx context.Context, url string, headers map[string]string) (sysweb.FetchResult, error) {
	return sysweb.FetchResult{}, errors.New("injected fetch failure")
}

// testToolGroupCfg supplies a per-category config string (e.g. to force a
// failing web provider) without touching real configuration.
type testToolGroupCfg struct {
	web string
}

func (c *testToolGroupCfg) GetConfig(ctx context.Context, category string) (string, error) {
	if category == "Web" {
		return c.web, nil
	}
	return "", nil
}

// TestAgent_ExpectedToolFailuresAcrossFamilies is the cross-family regression
// gate (openspec task 6.7): an expected failure in the filesystem, web, and
// browser tools must be converted to a tool-result observation and must NOT
// surface as a fatal stream error. The agent turn continues and the model
// receives the observation in each family.
func TestAgent_ExpectedToolFailuresAcrossFamilies(t *testing.T) {
	// Force web_fetch/web_search terminal failures deterministically by
	// failing both the preferred provider and the http fallback.
	origBoom, _ := sysweb.LookupFetcher("boom")
	origHTTP, _ := sysweb.LookupFetcher("http")
	sysweb.RegisterFetcher("boom", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Fetcher, error) {
		return failingFetcher{}, nil
	})
	sysweb.RegisterFetcher("http", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Fetcher, error) {
		return failingFetcher{}, nil
	})
	defer func() {
		if origBoom != nil {
			sysweb.RegisterFetcher("boom", origBoom)
		}
		if origHTTP != nil {
			sysweb.RegisterFetcher("http", origHTTP)
		}
	}()

	type scenario struct {
		name      string
		tool      string
		args      string
		toolGroup *testToolGroupCfg
		// wantSubstr is required to appear in the rendered output (empty means
		// only the no-fatal-error contract is asserted, e.g. browser where the
		// engine path is environment-dependent).
		wantSubstr string
	}
	scenarios := []scenario{
		{"fs", "read_file", `{"file_path":"/tmp/secret"}`, nil, "outside workspace"},
		{"web", "web_fetch", `{"url":"http://unreachable.invalid"}`, &testToolGroupCfg{web: `{"fetch_provider":"boom"}`}, "web_fetch failed"},
		{"browser", "browser_navigate", `{"url":"http://unreachable.invalid"}`, nil, ""},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "onclaw-errsig-*")
			if err != nil {
				t.Fatalf("tempdir: %v", err)
			}
			defer os.RemoveAll(tmpDir)
			workspace := filepath.Join(tmpDir, "workspace")
			userConfigDir := filepath.Join(tmpDir, "config")
			for _, d := range []string{workspace, userConfigDir} {
				if err := os.MkdirAll(d, 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}

			modelCalls := 0
			respondMock := func(input []*schema.AgenticMessage) (*schema.AgenticMessage, error) {
				modelCalls++
				if modelCalls == 1 {
					return &schema.AgenticMessage{
						Role: schema.AgenticRoleTypeAssistant,
						ContentBlocks: []*schema.ContentBlock{{
							Type:             schema.ContentBlockTypeFunctionToolCall,
							FunctionToolCall: &schema.FunctionToolCall{CallID: "c1", Name: sc.tool, Arguments: sc.args},
						}},
					}, nil
				}
				return &schema.AgenticMessage{
					Role: schema.AgenticRoleTypeAssistant,
					ContentBlocks: []*schema.ContentBlock{
						schema.NewContentBlock(&schema.AssistantGenText{Text: "Continued after the observation."}),
					},
				}, nil
			}
			fm := &fakeChatModel{
				generateFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
					return respondMock(input)
				},
				streamFunc: func(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
					msg, err := respondMock(input)
					if err != nil {
						return nil, err
					}
					sr, sw := schema.Pipe[*schema.AgenticMessage](1)
					sw.Send(msg, nil)
					sw.Close()
					return sr, nil
				},
			}

			agentConf := &store.Agent{Name: "errsig-" + sc.name, Tools: sc.tool, MaxIterations: 5}
			agentVal, err := agent.AssembleAgent(context.Background(), agentConf, fm, fm, workspace, userConfigDir, "deny", nil, nil, 64000, dummyConvStore{}, 1, nil, nil, nil, "test", nil, sc.toolGroup, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, 0, nil, 3)
			if err != nil {
				t.Fatalf("assemble: %v", err)
			}

			var stdout bytes.Buffer
			it := agentVal.Run(context.Background(), "do it")
			tr := render.Text(&stdout)
			for {
				msg, ok := it.Next()
				if !ok {
					break
				}
				if err := tr.Render(msg); err != nil {
					t.Fatalf("render: %v", err)
				}
			}
			if err := tr.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}

			// The turn must NOT terminate with a fatal stream error.
			if err := it.Err(); err != nil {
				t.Fatalf("%s: expected turn to continue (no fatal error), got %v", sc.name, err)
			}
			// The model must have recovered and produced a second response.
			if modelCalls < 2 {
				t.Fatalf("%s: expected the model to continue after the observation, got %d calls", sc.name, modelCalls)
			}
			output := stdout.String()
			if sc.wantSubstr != "" && !strings.Contains(output, sc.wantSubstr) {
				t.Errorf("%s: expected observation %q in output, got %q", sc.name, sc.wantSubstr, output)
			}
			// Message-discipline / no-leak invariant: the absolute workspace
			// root must never appear in an observation — only the requested
			// input (e.g. "/tmp/secret") may be quoted. Guards against the
			// sandbox path leaking into the model context.
			if strings.Contains(output, workspace) {
				t.Errorf("%s: observation leaked absolute workspace root %q (output=%q)", sc.name, workspace, output)
			}
		})
	}
}
