package skill

import (
	"fmt"
	"log"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseAndNormalizeManifest parses the content of a SKILL.md file, normalizes its frontmatter,
// and returns the normalized full file content along with the parsed metadata.
func ParseAndNormalizeManifest(content string, fallbackName string) (normalizedContent string, meta map[string]interface{}, err error) {
	content = strings.TrimSpace(content)

	var frontmatterYAML string
	var body string

	if strings.HasPrefix(content, "---") {
		// Find the ending ---
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			frontmatterYAML = parts[1]
			body = parts[2]
		} else {
			// Malformed frontmatter
			body = content
		}
	} else {
		body = content
	}

	meta = make(map[string]interface{})
	if frontmatterYAML != "" {
		if err := yaml.Unmarshal([]byte(frontmatterYAML), &meta); err != nil {
			log.Printf("Warning: failed to parse YAML frontmatter: %v. Treating as raw text.", err)
		}
	}

	// Normalize name
	name, _ := meta["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallbackName
		meta["name"] = name
		log.Printf("Warning: missing skill name, synthesized from source/directory: %s", name)
	}

	// Normalize description
	desc, _ := meta["description"].(string)
	desc = strings.TrimSpace(desc)
	if desc == "" {
		// Synthesize description from first non-empty line of the body
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				// Strip markdown heading symbols if any
				trimmed = strings.TrimLeft(trimmed, "#* \t")
				if len(trimmed) > 200 {
					trimmed = trimmed[:200] + "..."
				}
				desc = trimmed
				break
			}
		}
		if desc == "" {
			desc = "Skill " + name
		}
		meta["description"] = desc
		log.Printf("Warning: missing skill description, synthesized: %s", desc)
	}

	// Strip context fork* keys
	if ctxVal, exists := meta["context"]; exists {
		if ctxStr, ok := ctxVal.(string); ok {
			if strings.HasPrefix(ctxStr, "fork") {
				delete(meta, "context")
			}
		}
	}

	// Re-marshal frontmatter
	fmBytes, err := yaml.Marshal(meta)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal normalized frontmatter: %w", err)
	}

	normalizedContent = fmt.Sprintf("---\n%s---\n%s", string(fmBytes), body)
	return normalizedContent, meta, nil
}
