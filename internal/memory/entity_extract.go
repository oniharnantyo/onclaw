package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ExtractEntities extracts entities and relations from episodic summary text.
// Uses LLM to identify entities (Person, Concept, Location, etc.) and relations between them.
func ExtractEntities(ctx context.Context, chatModel model.AgenticModel, text string) (*Extraction, error) {
	if chatModel == nil {
		return nil, fmt.Errorf("chatModel is required for entity extraction")
	}
	if strings.TrimSpace(text) == "" {
		return &Extraction{}, nil
	}

	prompt := fmt.Sprintf(`Analyze the following text and extract entities and relations.
Focus on: people, projects, concepts, locations, technologies, and meaningful relationships between them.

Return ONLY a JSON object with this exact structure:
{
  "entities": [
    {"type": "EntityType", "name": "EntityName"}
  ],
  "relations": [
    {"from": "EntityName1", "predicate": "relation_type", "to": "EntityName2"}
  ]
}

Entity types should be: Person, Project, Concept, Location, Technology, Organization, or Other.
Relations should be simple predicates like: works_on, uses, located_at, related_to, depends_on, etc.

Text to analyze:
%s`, text)

	resp, err := chatModel.Generate(ctx, []*schema.AgenticMessage{
		schema.UserAgenticMessage(prompt),
	})
	if err != nil {
		return nil, fmt.Errorf("LLM extraction failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response from model")
	}

	extractedText := getAgenticMessageText(resp)
	extractedText = strings.TrimSpace(extractedText)

	// Parse the JSON response
	var extraction Extraction
	if err := parseExtractionJSON(extractedText, &extraction); err != nil {
		return nil, fmt.Errorf("parse extraction JSON: %w", err)
	}

	return &extraction, nil
}

// ExtractEntitiesWithSecurity extracts entities and applies security scanning.
// Returns error if security threats are detected in the extracted content.
func ExtractEntitiesWithSecurity(ctx context.Context, chatModel model.AgenticModel, text string, agentName string, sourceID string, skipSecurityScan bool) (*Extraction, error) {
	ext, err := ExtractEntities(ctx, chatModel, text)
	if err != nil {
		return nil, err
	}

	if !skipSecurityScan {
		// Security scan: check all entity names and relation predicates
		for _, ent := range ext.Entities {
			if err := ScanContent(ent.Type); err != nil {
				return nil, fmt.Errorf("security threat in entity type %q: %w", ent.Type, err)
			}
			if err := ScanContent(ent.Name); err != nil {
				return nil, fmt.Errorf("security threat in entity name %q: %w", ent.Name, err)
			}
		}

		for _, rel := range ext.Relations {
			if err := ScanContent(rel.Predicate); err != nil {
				return nil, fmt.Errorf("security threat in relation predicate %q: %w", rel.Predicate, err)
			}
		}
	}

	// Populate agent and sourceID
	ext.Agent = agentName
	ext.SourceID = sourceID

	return ext, nil
}

// parseExtractionJSON parses the LLM response into an Extraction struct.
// Handles common JSON formatting issues from LLM responses.
// The LLM returns from/to as entity names (strings); we parse them into
// FromName/ToName so IngestExtraction can resolve them to entity IDs.
func parseExtractionJSON(text string, extraction *Extraction) error {
	// Clean up common LLM response issues
	text = strings.TrimSpace(text)

	// Remove markdown code blocks if present
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		var jsonLines []string
		inJson := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```json") || strings.HasPrefix(line, "```") {
				inJson = true
				continue
			}
			if strings.HasPrefix(line, "```") && inJson {
				break
			}
			if inJson || !strings.HasPrefix(line, "```") {
				jsonLines = append(jsonLines, line)
			}
		}
		text = strings.Join(jsonLines, "\n")
	}

	// LLM returns from/to as entity name strings, not integer IDs.
	// Parse into an intermediate shape, then map to Relation fields.
	var raw struct {
		Entities []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"entities"`
		Relations []struct {
			From      string `json:"from"`
			To        string `json:"to"`
			Predicate string `json:"predicate"`
		} `json:"relations"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return fmt.Errorf("parse extraction JSON: %w", err)
	}

	extraction.Entities = make([]Entity, 0, len(raw.Entities))
	for _, e := range raw.Entities {
		if e.Type == "" || e.Name == "" {
			continue
		}
		extraction.Entities = append(extraction.Entities, Entity{
			Type: e.Type,
			Name: normalizeEntityName(e.Name),
		})
	}

	extraction.Relations = make([]Relation, 0, len(raw.Relations))
	for _, r := range raw.Relations {
		if r.From == "" || r.To == "" || r.Predicate == "" {
			continue
		}
		extraction.Relations = append(extraction.Relations, Relation{
			FromName:  normalizeEntityName(r.From),
			ToName:    normalizeEntityName(r.To),
			Predicate: r.Predicate,
		})
	}

	return nil
}

// normalizeEntityName normalizes an entity name for deduplication.
func normalizeEntityName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.Join(strings.Fields(name), " ")
	return name
}
