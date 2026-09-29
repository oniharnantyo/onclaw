# Spec Delta

## MODIFIED Requirements

### Requirement: Fixed middleware order
When multiple capabilities attach in one composition, their behaviors SHALL be wired in the fixed order: patch-tool-calls, reduction, summarization, skill, filesystem, subagent, background-control — regardless of the order the configurations appear in. The subagent and background-control steps attach only when the subagent capability is wired. Later steps wrap earlier ones: the policy, gate, and tool-error-result wrappers stay outside the delegation and control tools.

#### Scenario: Full stack ordering
- **WHEN** a composition includes filesystem, skills, summarization, and the subagent capability
- **THEN** the wired behaviors execute in the order patch-tool-calls, reduction, summarization, skill, filesystem, subagent, background-control

#### Scenario: Subagent absent leaves order untouched
- **WHEN** a composition without the subagent capability includes filesystem, skills, and summarization
- **THEN** the wired behaviors execute in the pre-existing order with no subagent or background-control step

## ADDED Requirements

### Requirement: Subagent wiring inputs
Composition SHALL accept the subagent capability as data: declared subagent instances, a background configuration, and a suppress-general-purpose flag. Wiring the capability SHALL inject a general-purpose clone of the composed configuration — same name derivation, instruction, model, tool surface, iteration cap, and capability wiring, minus the delegation capability — described as a general-purpose research delegate. Composition SHALL remain pure: the subagent instances and background configuration arrive pre-built, and no store, database, or disk is consulted. Validation SHALL fail fast when the capability is wired with the general-purpose subagent suppressed and no subagent instances declared, or when declared instances have empty names, empty descriptions, or duplicate names.

#### Scenario: Clone mirrors the parent
- **WHEN** the capability is wired without suppression
- **THEN** the injected general-purpose subagent exposes the parent's tool surface and instruction and carries its own fresh-conversation execution

#### Scenario: Suppressed with nothing declared rejected
- **WHEN** the capability is wired with the general-purpose subagent suppressed and no declared subagent instances
- **THEN** a descriptive error is returned and no agent is constructed

#### Scenario: Purity holds with the capability wired
- **WHEN** a caller composes with declared subagent instances and a pre-built background configuration
- **THEN** composition succeeds without touching any store, database, or the filesystem
