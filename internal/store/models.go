package store

import "encoding/json"

// MarshalModelMetadata marshals ModelMetadata to JSON string.
func MarshalModelMetadata(meta *ModelMetadata) (string, error) {
	if meta == nil {
		return "{}", nil
	}
	bytes, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// UnmarshalModelMetadata unmarshals JSON string to ModelMetadata.
func UnmarshalModelMetadata(metaJSON string) (*ModelMetadata, error) {
	if metaJSON == "" {
		return &ModelMetadata{InputModalities: []string{"text"}}, nil
	}
	var meta ModelMetadata
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return nil, err
	}
	if meta.InputModalities == nil {
		meta.InputModalities = []string{"text"}
	}
	return &meta, nil
}

// ResolveContextWindow determines the effective context window limit using the
// precedence: agent-level MaxContextTokens, then the global default, then the
// model metadata's reported context window, falling back to 64000. The CLI run
// path and the web API must agree on this so the context meter reflects the
// agent's configured limit rather than the hard-coded default.
func ResolveContextWindow(maxContextTokens, globalMaxContextTokens int, modelMetadata string) int {
	var contextWindow int
	if maxContextTokens > 0 {
		contextWindow = maxContextTokens
	} else if globalMaxContextTokens > 0 {
		contextWindow = globalMaxContextTokens
	} else {
		if modelMetadata != "" {
			if meta, err := UnmarshalModelMetadata(modelMetadata); err == nil && meta != nil {
				contextWindow = meta.ContextWindow
			}
		}
		if contextWindow <= 0 {
			contextWindow = 64000
		}
	}
	return contextWindow
}
