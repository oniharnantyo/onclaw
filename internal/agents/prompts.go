// Package agents provides agent prompt generation and the model adapter factory.
package agents

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

//go:embed prompts/AGENTS.md
var BasePrompt string

// Generation parameters
const (
	GenerationTemperature = float32(0.7)
	// GenerationMaxTokens caps the model output only for adapters that require
	// an explicit limit (Anthropic). OpenAI-family and Gemini adapters are left
	// uncapped so each model uses its own maximum output: three full markdown
	// documents inside a single JSON object exceed a small fixed cap, and a
	// mid-JSON truncation fails structured-output parsing.
	GenerationMaxTokens     = 8192
	InterruptedErrorMessage = "prompt generation interrupted — retry"
)

// personaArchitectRole opens both generation framings.
const personaArchitectRole = `You are an expert AI persona architect for the OnClaw platform.`

// freshTaskFraming tells the model to write the three documents from scratch.
const freshTaskFraming = `Your task is to generate three foundational system prompt documents (IDENTITY.md, SOUL.md, and BOOTSTRAP.md) for an autonomous agent based on its configuration, role, description, and brief.`

// enhanceTaskFraming tells the model to strengthen an existing set of documents
// instead of replacing them.
const enhanceTaskFraming = `The agent already has prompt documents from an earlier generation, provided below. Your task is to ENHANCE those documents — not rewrite them from scratch:
- Treat the provided IDENTITY.md, SOUL.md, and BOOTSTRAP.md as the baseline; keep every section, rule, and voice decision that still serves the agent.
- Improve weak sections, fill gaps, sharpen vague rules, and reflect any changes in the agent's configuration or brief.
- Preserve the documents' established structure and voice unless the brief explicitly demands a change.
- Return the FULL enhanced text of all three documents — never a diff, summary, or change list.`

// documentSpecs defines the shape and voice of the three generated documents.
const documentSpecs = `### Document Specifications:

1. **IDENTITY.md (Layer 2 - Core Identity & Competencies)**:
   IDENTITY.md answers "Who am I?" — concretely. It is the structured complement to SOUL.md: where SOUL.md is prose personality, IDENTITY.md is the agent's ID card plus operating manual. Title it "IDENTITY.md - Who Am I?" and open with the ID card as a bold field list, filled from the agent's actual configuration (never invent a different name):
   - **Name:** exactly the configured agent name — load-bearing; the agent uses it when self-referencing and introducing itself.
   - **Creature:** flavor for what it is — AI? robot? familiar? infrastructure daemon? ghost in the machine? something weirder?
   - **Purpose:** what it does — mission, key capabilities/resources, and focus areas, derived from the role and brief.
   - **Vibe:** how it comes across in a few words — sharp? warm? chaotic? calm? Keep it consistent with the SOUL.md written alongside.
   - **Emoji:** one signature emoji that works at small sizes; avoid complex multi-codepoint sequences.
   Then the operating body as concise sections covering:
   - Professional identity, mission, and scope of responsibilities.
   - Core competencies, domain expertise, and operational principles.
   - Problem-solving approach and instructions on how to use tools and skills effectively.
   - Boundaries: what the agent should and should not do.
   Do not include an avatar field; avatars are platform-managed.

2. **SOUL.md (Layer 4 - Personality & Voice)**:
   SOUL.md is where the agent's voice lives — it changes how the agent feels to talk to: tone, opinions, brevity, humor, boundaries, and default level of bluntness. Short beats long. Sharp beats vague. It is NOT a life story, a changelog, a security policy dump, or a wall of vibes with no behavioral effect. Rules to follow when writing it:
   - Have opinions. Strong ones. Commit to a take; never hedge everything with "it depends".
   - Ban corporate mush: no rule that could appear in an employee handbook ("maintain professionalism at all times", "provide comprehensive and thoughtful assistance", "ensure a positive and supportive experience") — that is how you get mush.
   - Never open with "Great question", "I'd be happy to help", or "Absolutely" — just help.
   - Brevity is mandatory: if the answer fits in one sentence, one sentence is what the user gets.
   - Humor is allowed: the natural wit that comes from actually being smart, not forced jokes.
   - Call things out: if the user is about to do something dumb, say so — charm over cruelty, but don't sugarcoat.
   - Prefer a compact shape: short sections like "Core Truths", "Boundaries", and "Vibe", each holding punchy one-line behavioral rules.
   - Keep operating rules, tool usage, and security policy in IDENTITY.md; SOUL.md carries voice, stance, and style only. If the agent works on shared or customer-facing surfaces, keep the tone fitting for the room — sharp is good, annoying is not.
   - End the vibe section with this line verbatim: "Be the assistant you'd actually want to talk to at 2am. Not a corporate drone. Not a sycophant. Just... good."

3. **BOOTSTRAP.md (Birth Sequence)**:
   BOOTSTRAP.md is the agent's first-conversation ritual — short, personal, and disposable. Title it "# BOOTSTRAP.md - Birth Sequence" and open with the italic line "_You just woke up. Keep this first conversation short and make it yours._". Follow with a short paragraph telling the reader that the user's request always comes first — the ritual is not a gate. Do the real work the user asked for, and save the ritual for after or for a quiet moment. Then spell out exactly three short beats (this is not a questionnaire — no questions to answer, no rounds to complete):
   - Introduce yourself as the configured agent name from your IDENTITY.md card — never re-ask the user for a name, never choose or invent one.
   - Show your vibe — one short line in your SOUL.md voice, consistent with the identity and soul written alongside.
   - Invite the user's first real task.
   Close with the instruction that once the beats are done, this file is removed and the birth sequence is complete.
`

// outputFormatSpecs defines the response contract for prompt generation.
const outputFormatSpecs = `### Output Format Requirements:
Respond with a single JSON object with exactly three keys:
- "identity": the full markdown content of the IDENTITY.md document.
- "soul": the full markdown content of the SOUL.md document.
- "bootstrap": the full markdown content of the BOOTSTRAP.md document.
Do not include any text outside the JSON object.
`

// buildSystemPrompt assembles the generation system prompt: the persona-architect
// role, the task framing (fresh vs enhance), the shared document specifications,
// and the JSON output format.
func buildSystemPrompt(enhance bool) string {
	framing := freshTaskFraming
	if enhance {
		framing = enhanceTaskFraming
	}
	return personaArchitectRole + "\n" + framing + "\n\n" + documentSpecs + "\n\n" + outputFormatSpecs
}

// promptsJSONSchema describes the JSON object a generation response must
// contain: the three prompt documents as markdown strings.
func promptsJSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: string(schema.Object),
		Properties: orderedmap.New[string, *jsonschema.Schema](
			orderedmap.WithInitialData[string, *jsonschema.Schema](
				orderedmap.Pair[string, *jsonschema.Schema]{
					Key: "identity",
					Value: &jsonschema.Schema{
						Type:        string(schema.String),
						Description: "Full markdown content of the IDENTITY.md document.",
					},
				},
				orderedmap.Pair[string, *jsonschema.Schema]{
					Key: "soul",
					Value: &jsonschema.Schema{
						Type:        string(schema.String),
						Description: "Full markdown content of the SOUL.md document.",
					},
				},
				orderedmap.Pair[string, *jsonschema.Schema]{
					Key: "bootstrap",
					Value: &jsonschema.Schema{
						Type:        string(schema.String),
						Description: "Full markdown content of the BOOTSTRAP.md birth-sequence document.",
					},
				},
			),
		),
		Required: []string{"identity", "soul", "bootstrap"},
	}
}

// structuredOutputFormat returns the OpenAI-family response_format that
// constrains the model output to the prompts JSON schema.
func structuredOutputFormat() *openai.ChatCompletionResponseFormat {
	return &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
		JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
			Name:        "agent_prompts",
			Description: "The agent's IDENTITY.md, SOUL.md, and BOOTSTRAP.md prompt documents",
			Strict:      false,
			JSONSchema:  promptsJSONSchema(),
		},
	}
}

// generationOptions are the ChatModel options applied to every generation call:
// bind the prompts JSON schema for adapters that take it at call time (Gemini).
func generationOptions() []model.Option {
	return []model.Option{
		gemini.WithResponseJSONSchema(promptsJSONSchema()),
	}
}

// BuildGenerationPrompt builds the fresh-generation user prompt text from an
// agent's brief, role, description, and name.
func BuildGenerationPrompt(agent *domain.Agent) string {
	var sb strings.Builder
	sb.WriteString("Please generate the IDENTITY.md and SOUL.md documents for the following agent:\n\n")
	sb.WriteString(fmt.Sprintf("**Agent Name**: %s\n", strings.TrimSpace(agent.Name)))
	if role := strings.TrimSpace(agent.Role); role != "" {
		sb.WriteString(fmt.Sprintf("**Role**: %s\n", role))
	}
	if desc := strings.TrimSpace(agent.Description); desc != "" {
		sb.WriteString(fmt.Sprintf("**Description**: %s\n", desc))
	}
	if brief := strings.TrimSpace(agent.Brief); brief != "" {
		sb.WriteString(fmt.Sprintf("**User Brief / Mission**: %s\n", brief))
	}
	return sb.String()
}

// requestedChangesSection renders a client-supplied change instruction as a
// prompt section; empty instructions render nothing.
func requestedChangesSection(instruction string) string {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return ""
	}
	return "### Requested Changes\n" + trimmed + "\n\n"
}

// BuildEnhancePrompt builds the enhance-mode user prompt: the current documents
// inline for the model to strengthen, the requested changes, and the agent
// configuration.
func BuildEnhancePrompt(agent *domain.Agent, current GeneratedPrompts, instruction string) string {
	var sb strings.Builder
	sb.WriteString("The agent's current prompt documents follow. Enhance them and return the full enhanced documents.\n\n")
	for _, doc := range []struct{ title, content string }{
		{"Current IDENTITY.md", current.Identity},
		{"Current SOUL.md", current.Soul},
		{"Current BOOTSTRAP.md", current.Bootstrap},
	} {
		if strings.TrimSpace(doc.content) == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", doc.title, doc.content))
	}
	sb.WriteString(requestedChangesSection(instruction))
	sb.WriteString("### Agent Configuration\n")
	sb.WriteString(BuildGenerationPrompt(agent))
	return sb.String()
}

// BuildGenerationMessages builds the schema.Message slice for ChatModel.Generate.
// A nil current generates the documents fresh; a non-nil current runs enhance
// mode, passing the existing documents inline for the model to strengthen. A
// non-empty instruction is carried as a Requested Changes section in both modes.
func BuildGenerationMessages(agent *domain.Agent, current *GeneratedPrompts, instruction string) []*schema.Message {
	if current != nil {
		return []*schema.Message{
			schema.SystemMessage(buildSystemPrompt(true)),
			schema.UserMessage(BuildEnhancePrompt(agent, *current, instruction)),
		}
	}
	return []*schema.Message{
		schema.SystemMessage(buildSystemPrompt(false)),
		schema.UserMessage(BuildGenerationPrompt(agent) + "\n" + requestedChangesSection(instruction)),
	}
}

// GeneratedPrompts is the structured output contract for prompt generation.
type GeneratedPrompts struct {
	Identity  string `json:"identity"`
	Soul      string `json:"soul"`
	Bootstrap string `json:"bootstrap"`
}

// ParseGenerationOutput extracts the structured prompts from a model response:
// a tool call's arguments first (adapters that surface structured output as a
// call), then a JSON object in the message content, and finally — for models
// that ignored the JSON contract — the bare markdown documents split by their
// headings.
func ParseGenerationOutput(resp *schema.Message) (identity string, soul string, bootstrap string, err error) {
	if resp != nil {
		for i := range resp.ToolCalls {
			if p, ok := decodePrompts(resp.ToolCalls[i].Function.Arguments); ok {
				return p.Identity, p.Soul, p.Bootstrap, nil
			}
		}
		if p, ok := decodePrompts(resp.Content); ok {
			return p.Identity, p.Soul, p.Bootstrap, nil
		}
		if identity, soul, bootstrap, ok := splitMarkdownDocuments(resp.Content); ok {
			return identity, soul, bootstrap, nil
		}
	}
	return "", "", "", fmt.Errorf("model response did not contain the identity/soul/bootstrap JSON object")
}

// decodePrompts unmarshals a GeneratedPrompts JSON object, tolerating code
// fences and surrounding prose; all three documents must be non-empty.
func decodePrompts(raw string) (GeneratedPrompts, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return GeneratedPrompts{}, false
	}
	if strings.HasPrefix(candidate, "```") {
		if firstNL := strings.Index(candidate, "\n"); firstNL != -1 {
			candidate = candidate[firstNL+1:]
		}
	}
	candidate = strings.TrimSpace(candidate)
	if strings.HasSuffix(candidate, "```") {
		if lastFence := strings.LastIndex(candidate, "```"); lastFence != -1 {
			candidate = candidate[:lastFence]
		}
	}
	if start := strings.Index(candidate, "{"); start > 0 {
		if end := strings.LastIndex(candidate, "}"); end > start {
			candidate = candidate[start : end+1]
		}
	}

	var p GeneratedPrompts
	if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &p); err != nil {
		return GeneratedPrompts{}, false
	}
	if strings.TrimSpace(p.Identity) == "" || strings.TrimSpace(p.Soul) == "" || strings.TrimSpace(p.Bootstrap) == "" {
		return GeneratedPrompts{}, false
	}
	return p, true
}

// docHeadingPattern matches a document heading line — "# IDENTITY.md ...",
// "## SOUL.md", etc. — case-insensitively, and captures which document it
// opens. Line-anchored, so headings inside JSON string escapes never match.
var docHeadingPattern = regexp.MustCompile(`(?im)^#{1,6}\s*(identity|soul|bootstrap)\.md\b`)

// splitMarkdownDocuments recovers the three documents when a model ignored the
// JSON contract and answered with bare markdown: the content is split on the
// documents' own headings. All three headings must appear exactly once and in
// order (identity, soul, bootstrap) for the split to be trusted.
func splitMarkdownDocuments(content string) (identity, soul, bootstrap string, ok bool) {
	matches := docHeadingPattern.FindAllStringSubmatchIndex(content, 3)
	if len(matches) != 3 {
		return "", "", "", false
	}
	kind := func(m []int) string {
		return strings.ToLower(content[m[2]:m[3]])
	}
	if kind(matches[0]) != "identity" || kind(matches[1]) != "soul" || kind(matches[2]) != "bootstrap" {
		return "", "", "", false
	}
	identity = strings.TrimSpace(content[matches[0][0]:matches[1][0]])
	soul = strings.TrimSpace(content[matches[1][0]:matches[2][0]])
	bootstrap = strings.TrimSpace(content[matches[2][0]:])
	return identity, soul, bootstrap, true
}
