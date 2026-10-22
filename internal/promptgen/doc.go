// Package promptgen generates an agent's three prompt documents — IDENTITY,
// SOUL, and BOOTSTRAP — from its brief, on the agent's own provider and model,
// with hardcoded generation prompts and parameters.
//
// The package owns one generation: building the eino chat model, invoking it
// with the generation prompt, parsing the structured documents, and committing
// them through a DocStore (enhance mode with .bak-before-commit safety is a
// DocStore contract). It performs no database access and no prompt-status
// transitions — the generating → ready | failed status machine lives in the
// services layer that wires this package.
package promptgen
