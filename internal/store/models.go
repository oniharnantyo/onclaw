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
