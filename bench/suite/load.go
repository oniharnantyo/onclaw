package suite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Load reads and parses a suite file from disk. Suite files live at
// bench/suites/*.json (one per lane). Load performs JSON-shape validation
// only; semantic validation and the ABC authoring lint live in Lint, which
// the runner runs right after Load.
func Load(path string) (TaskSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskSuite{}, fmt.Errorf("load task suite: %w", err)
	}
	s, err := Parse(data)
	if err != nil {
		return TaskSuite{}, fmt.Errorf("load task suite %s: %w", path, err)
	}
	return s, nil
}

// Parse decodes a suite file from raw JSON. Decoding is strict: unknown
// fields are rejected so a typo'd field name in an authored lane file
// ("anyOf" instead of "any_of", a renamed probe) surfaces as a load error
// instead of silently degrading to an empty check.
func Parse(data []byte) (TaskSuite, error) {
	var s TaskSuite
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return TaskSuite{}, fmt.Errorf("parse task suite: %w", err)
	}
	if dec.More() {
		return TaskSuite{}, fmt.Errorf("parse task suite: unexpected trailing data after the JSON object")
	}
	return s, nil
}
