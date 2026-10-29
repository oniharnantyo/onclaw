package promptgen

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
)

// Generation parameters
const (
	GenerationTemperature = float32(0.7)
	// GenerationMaxTokens caps the model output only for adapters that require
	// an explicit limit (Anthropic). OpenAI-family and Gemini adapters are left
	// uncapped so each model uses its own maximum output: three full markdown
	// documents inside a single JSON object exceed a small fixed cap, and a
	// mid-JSON truncation fails structured-output parsing.
	GenerationMaxTokens = 8192
)

// personaArchitectRole opens both generation framings.
const personaArchitectRole = `You are an expert AI persona architect for the buddies platform.`

// freshTaskFraming tells the model to write the three documents from scratch.
const freshTaskFraming = `Your task is to generate three foundational system prompt documents (IDENTITY.md, SOUL.md, and BOOTSTRAP.md) for an autonomous agent based on its name and brief.`

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

3. **BOOTSTRAP.md (Layer 3 - First Impressions)**:
   BOOTSTRAP.md scripts the agent's first conversation — the birth sequence. It composes into every turn, so keep it short, self-contained, and durable: a page the agent grows into gracefully, not a gate it performs once. Title it "BOOTSTRAP.md - Birth Sequence". This ritual is not a gate: the user's request always comes first — do the real work they asked for, and save the ritual for after, or for a quiet moment. When the moment is right, complete three short beats:
   1. **Introduce yourself.** Use the name on the IDENTITY.md card — never re-ask the user for a name, never choose or invent one.
   2. **Show your vibe.** One short line in the SOUL.md voice, so the user knows who they're talking to.
   3. **Invite the user's first real task.**
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
						Description: "Full markdown content of the BOOTSTRAP.md document.",
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

// generationOptions are the ChatModel options applied to a generation call:
// bind the prompts JSON schema for adapters that take it at call time (Gemini).
// An unstructured call — the retry path for providers that rejected the native
// structured-output request — binds no schema.
func generationOptions(structured bool) []model.Option {
	if !structured {
		return nil
	}
	return []model.Option{
		gemini.WithResponseJSONSchema(promptsJSONSchema()),
	}
}

// BuildGenerationPrompt builds the fresh-generation user prompt text from the
// agent's name and brief.
func BuildGenerationPrompt(agentName, brief string) string {
	var sb strings.Builder
	sb.WriteString("Please generate the IDENTITY.md, SOUL.md, and BOOTSTRAP.md documents for the following agent:\n\n")
	sb.WriteString(fmt.Sprintf("**Agent Name**: %s\n", strings.TrimSpace(agentName)))
	if b := strings.TrimSpace(brief); b != "" {
		sb.WriteString(fmt.Sprintf("**User Brief / Mission**: %s\n", b))
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
func BuildEnhancePrompt(p Params, current Documents, instruction string) string {
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
	sb.WriteString(BuildGenerationPrompt(p.AgentName, p.Brief))
	return sb.String()
}

// BuildGenerationMessages builds the schema.Message slice for ChatModel.Generate.
// A nil current generates the documents fresh; a non-nil current runs enhance
// mode, passing the existing documents inline for the model to strengthen. A
// non-empty instruction is carried as a Requested Changes section in both modes.
func BuildGenerationMessages(p Params, current *Documents) []*schema.Message {
	if current != nil {
		return []*schema.Message{
			schema.SystemMessage(buildSystemPrompt(true)),
			schema.UserMessage(BuildEnhancePrompt(p, *current, p.Instruction)),
		}
	}
	return []*schema.Message{
		schema.SystemMessage(buildSystemPrompt(false)),
		schema.UserMessage(BuildGenerationPrompt(p.AgentName, p.Brief) + "\n" + requestedChangesSection(p.Instruction)),
	}
}

// ParseGenerationOutput extracts the structured prompts from a model response:
// a tool call's arguments first (adapters that surface structured output as a
// call), then a JSON object in the message content, and finally — for models
// that ignored the JSON contract — the bare markdown documents split by their
// headings.
func ParseGenerationOutput(resp *schema.Message) (Documents, error) {
	if resp != nil {
		for i := range resp.ToolCalls {
			if p, ok := decodePrompts(resp.ToolCalls[i].Function.Arguments); ok {
				return p, nil
			}
		}
		if p, ok := decodePrompts(resp.Content); ok {
			return p, nil
		}
		if docs, ok := splitMarkdownDocuments(resp.Content); ok {
			return docs, nil
		}
	}
	return Documents{}, fmt.Errorf("model response did not contain the identity/soul/bootstrap JSON object")
}

// decodePrompts unmarshals a Documents JSON object, tolerating code fences and
// surrounding prose; all three documents must be non-empty.
func decodePrompts(raw string) (Documents, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return Documents{}, false
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

	var d Documents
	if err := json.Unmarshal([]byte(strings.TrimSpace(candidate)), &d); err != nil {
		return Documents{}, false
	}
	if strings.TrimSpace(d.Identity) == "" || strings.TrimSpace(d.Soul) == "" || strings.TrimSpace(d.Bootstrap) == "" {
		return Documents{}, false
	}
	return d, true
}

// docHeadingPattern matches a document heading line — "# IDENTITY.md ...",
// "## SOUL.md", "### BOOTSTRAP.md", etc. — case-insensitively, and captures
// which document it opens. Line-anchored, so headings inside JSON string
// escapes never match.
var docHeadingPattern = regexp.MustCompile(`(?im)^#{1,6}\s*(identity|soul|bootstrap)\.md\b`)

// splitMarkdownDocuments recovers the three documents when a model ignored the
// JSON contract and answered with bare markdown: the content is split on the
// documents' own headings. All three headings must appear exactly once and in
// order (identity, soul, bootstrap) for the split to be trusted.
func splitMarkdownDocuments(content string) (docs Documents, ok bool) {
	matches := docHeadingPattern.FindAllStringSubmatchIndex(content, 3)
	if len(matches) != 3 {
		return Documents{}, false
	}
	kind := func(m []int) string {
		return strings.ToLower(content[m[2]:m[3]])
	}
	if kind(matches[0]) != "identity" || kind(matches[1]) != "soul" || kind(matches[2]) != "bootstrap" {
		return Documents{}, false
	}
	docs.Identity = strings.TrimSpace(content[matches[0][0]:matches[1][0]])
	docs.Soul = strings.TrimSpace(content[matches[1][0]:matches[2][0]])
	docs.Bootstrap = strings.TrimSpace(content[matches[2][0]:])
	return docs, true
}
