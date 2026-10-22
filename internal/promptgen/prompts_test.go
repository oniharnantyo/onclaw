package promptgen

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestBuildGenerationPrompt(t *testing.T) {
	prompt := BuildGenerationPrompt("Data Analyst", "Focus on precision and actionable summaries.")
	require.Contains(t, prompt, "Data Analyst")
	require.Contains(t, prompt, "Focus on precision and actionable summaries.")
	require.Contains(t, prompt, "IDENTITY.md")
	require.Contains(t, prompt, "SOUL.md")
	require.Contains(t, prompt, "BOOTSTRAP.md")
}

func TestBuildGenerationMessages_Fresh(t *testing.T) {
	msgs := BuildGenerationMessages(testParams(), nil)
	require.Len(t, msgs, 2)
	require.Equal(t, schema.System, msgs[0].Role)
	require.Contains(t, msgs[0].Content, "IDENTITY.md")
	require.Contains(t, msgs[0].Content, "SOUL.md")
	require.Contains(t, msgs[0].Content, "BOOTSTRAP.md")
	require.NotContains(t, msgs[0].Content, "ENHANCE")

	require.Equal(t, schema.User, msgs[1].Role)
	require.Contains(t, msgs[1].Content, "Scout")
	require.Contains(t, msgs[1].Content, "Scout the perimeter.")
}

func TestBuildGenerationMessages_EnhanceMode(t *testing.T) {
	current := &Documents{
		Identity:  "# old identity body",
		Soul:      "# old soul body",
		Bootstrap: "# old bootstrap body",
	}

	msgs := BuildGenerationMessages(testParams(), current)
	require.Len(t, msgs, 2)
	require.Equal(t, schema.System, msgs[0].Role)
	require.Contains(t, msgs[0].Content, "ENHANCE")

	user := msgs[1].Content
	for _, want := range []string{
		"Current IDENTITY.md",
		"# old identity body",
		"Current SOUL.md",
		"# old soul body",
		"Current BOOTSTRAP.md",
		"# old bootstrap body",
		"Scout",
		"Scout the perimeter.",
	} {
		require.Contains(t, user, want)
	}
}

func TestBuildGenerationMessages_InstructionInFreshMode(t *testing.T) {
	p := testParams()
	p.Instruction = "keep it terse"
	msgs := BuildGenerationMessages(p, nil)
	require.Len(t, msgs, 2)
	require.Contains(t, msgs[1].Content, "Requested Changes")
	require.Contains(t, msgs[1].Content, "keep it terse")
}

func TestRequestedChangesSection_EmptyInstructionRendersNothing(t *testing.T) {
	require.Equal(t, "", requestedChangesSection(""))
	require.Equal(t, "", requestedChangesSection("   \n\t"))
	require.Contains(t, requestedChangesSection("sharper"), "### Requested Changes")
}

func TestParseGenerationOutput(t *testing.T) {
	tests := []struct {
		name        string
		resp        *schema.Message
		wantDocs    Documents
		wantErr     bool
		errContains string
	}{
		{
			name:     "forced tool call arguments",
			resp:     promptsToolCallMessage("# Identity\nCode reviewer.", "# Soul\nConcise and friendly.", "# Bootstrap\nSay hello."),
			wantDocs: Documents{Identity: "# Identity\nCode reviewer.", Soul: "# Soul\nConcise and friendly.", Bootstrap: "# Bootstrap\nSay hello."},
		},
		{
			name:     "json object in content",
			resp:     &schema.Message{Content: `{"identity":"# Identity\nExpert architect.","soul":"# Soul\nThoughtful mentor.","bootstrap":"# Bootstrap\nWelcome."}`},
			wantDocs: Documents{Identity: "# Identity\nExpert architect.", Soul: "# Soul\nThoughtful mentor.", Bootstrap: "# Bootstrap\nWelcome."},
		},
		{
			name:     "fenced json in content",
			resp:     &schema.Message{Content: "```json\n{\"identity\":\"# Identity\\nRelentless debugger.\",\"soul\":\"# Soul\\nCalm under pressure.\",\"bootstrap\":\"# Bootstrap\\nHi.\"}\n```"},
			wantDocs: Documents{Identity: "# Identity\nRelentless debugger.", Soul: "# Soul\nCalm under pressure.", Bootstrap: "# Bootstrap\nHi."},
		},
		{
			name:     "prose around json in content",
			resp:     &schema.Message{Content: "Here is the result:\n{\"identity\":\"I analyze incidents.\",\"soul\":\"I answer with calm.\",\"bootstrap\":\"I start gently.\"}\nHope that helps."},
			wantDocs: Documents{Identity: "I analyze incidents.", Soul: "I answer with calm.", Bootstrap: "I start gently."},
		},
		{
			name:     "bare markdown documents with prose",
			resp:     &schema.Message{Content: "Sure, here are the documents:\n\n# IDENTITY.md - Who Am I?\n**Name:** Radar\n\n## Competencies\nIncident analysis.\n\n# SOUL.md\nShort beats long.\n\n# BOOTSTRAP.md - Birth Sequence\nSay hello.\n"},
			wantDocs: Documents{Identity: "# IDENTITY.md - Who Am I?\n**Name:** Radar\n\n## Competencies\nIncident analysis.", Soul: "# SOUL.md\nShort beats long.", Bootstrap: "# BOOTSTRAP.md - Birth Sequence\nSay hello."},
		},
		{
			name:     "h2 markdown headings",
			resp:     &schema.Message{Content: "## IDENTITY.md\nWho am I?\n\n## SOUL.md\nVoice.\n\n## BOOTSTRAP.md\nFirst hello."},
			wantDocs: Documents{Identity: "## IDENTITY.md\nWho am I?", Soul: "## SOUL.md\nVoice.", Bootstrap: "## BOOTSTRAP.md\nFirst hello."},
		},
		{
			name:    "markdown headings out of order",
			resp:    &schema.Message{Content: "# SOUL.md\nVoice.\n\n# IDENTITY.md\nWho.\n\n# BOOTSTRAP.md\nHi."},
			wantErr: true,
		},
		{
			name:    "markdown missing bootstrap heading",
			resp:    &schema.Message{Content: "# IDENTITY.md\nWho.\n\n# SOUL.md\nVoice."},
			wantErr: true,
		},
		{
			name:    "empty identity rejected",
			resp:    promptsToolCallMessage("", "# Soul\nSomeone.", "# Bootstrap\nHi."),
			wantErr: true,
		},
		{
			name:    "empty soul rejected",
			resp:    promptsToolCallMessage("# Identity\nSomeone.", "   ", "# Bootstrap\nHi."),
			wantErr: true,
		},
		{
			name:    "empty bootstrap rejected",
			resp:    promptsToolCallMessage("# Identity\nSomeone.", "# Soul\nSomeone.", ""),
			wantErr: true,
		},
		{
			name:    "invalid json arguments",
			resp:    &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "submit_prompts", Arguments: "not json"}}}},
			wantErr: true,
		},
		{
			name:    "missing soul key",
			resp:    &schema.Message{Content: `{"identity":"only identity","bootstrap":"hi"}`},
			wantErr: true,
		},
		{
			name:    "missing bootstrap key",
			resp:    &schema.Message{Content: `{"identity":"id","soul":"soul"}`},
			wantErr: true,
		},
		{
			name:    "nil response",
			resp:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGenerationOutput(tt.resp)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDocs, got)
		})
	}
}

func TestDocuments_Blank(t *testing.T) {
	require.True(t, Documents{}.blank())
	require.True(t, Documents{Identity: " \n\t"}.blank())
	require.False(t, Documents{Identity: "x"}.blank())
	require.False(t, Documents{Soul: "x"}.blank())
	require.False(t, Documents{Bootstrap: "x"}.blank())
}
