package modelmeta

import "encoding/json"

// LimitObj represents model limits like context window size.
type LimitObj struct {
	Context int `json:"context"`
}

// ReasoningOptionObj represents a reasoning option capability in the catalog.
type ReasoningOptionObj struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    int      `json:"min,omitempty"`
	Max    int      `json:"max,omitempty"`
}

// ModalitiesObj represents the input/output modalities of the model.
type ModalitiesObj struct {
	Input []string `json:"input"`
}

// ModelObj represents a model's metadata in the models.dev catalog.
type ModelObj struct {
	Limit            LimitObj             `json:"limit"`
	Reasoning        bool                 `json:"reasoning"`
	ReasoningOptions []ReasoningOptionObj `json:"reasoning_options,omitempty"`
	Modalities       ModalitiesObj        `json:"modalities"`
}

// ProviderObj represents a provider's models in the models.dev catalog.
type ProviderObj struct {
	Models map[string]ModelObj `json:"models"`
}

// ApiJSON represents the root models.dev catalog structure.
type ApiJSON struct {
	Providers map[string]ProviderObj `json:"providers"`
}

// UnmarshalJSON unmarshals the flat JSON structure directly into the Providers map.
func (a *ApiJSON) UnmarshalJSON(data []byte) error {
	var m map[string]ProviderObj
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	a.Providers = m
	return nil
}
