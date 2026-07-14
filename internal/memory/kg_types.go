package memory

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID         int64   `json:"id"`
	Type       string  `json:"type"`  // e.g. "Person", "Concept", "Location"
	Name       string  `json:"name"`  // normalized name
	Agent      string  `json:"agent"` // agent that created this entity
	ValidFrom  string  `json:"valid_from"`
	ValidUntil *string `json:"valid_until,omitempty"` // null if current
}

// Relation represents a directed edge between entities.
type Relation struct {
	ID         int64   `json:"id"`
	FromEntity int64   `json:"from_entity"`
	ToEntity   int64   `json:"to_entity"`
	FromName   string  `json:"from_name,omitempty"` // entity name (used before ID resolution)
	ToName     string  `json:"to_name,omitempty"`   // entity name (used before ID resolution)
	Predicate  string  `json:"predicate"`
	Agent      string  `json:"agent"`
	ValidFrom  string  `json:"valid_from"`
	ValidUntil *string `json:"valid_until,omitempty"`
}

// Extraction contains entities and relations extracted from an episodic summary.
type Extraction struct {
	Agent     string     `json:"agent"`
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
	SourceID  string     `json:"source_id"` // episodic summary ID
}

// KGQuery represents a knowledge graph search query.
type KGQuery struct {
	SeedEntity     int64  `json:"seed_entity"`                // starting entity ID (direct)
	SeedEntityName string `json:"seed_entity_name,omitempty"` // starting entity name (resolved if SeedEntity=0)
	Agent          string `json:"agent"`                      // scope to this agent
	MaxDepth       int    `json:"max_depth"`                  // maximum hops from seed
	Limit          int    `json:"limit"`                      // maximum results to return
}

// KGHit represents a knowledge graph search result.
type KGHit struct {
	Entity   *Entity    `json:"entity"`
	Path     []Relation `json:"path"`     // relations traversed from seed to this entity
	Distance int        `json:"distance"` // number of hops from seed
}
