# web-app/chat — Delta

## ADDED Requirements

### Requirement: Degraded fence caption
When a fence fails validation, the transcript SHALL render the fence's source inside the ordinary styled code block, followed by a one-line caption stating the card could not be rendered. The caption SHALL carry the validator's rejection reason as its hover title. A fence whose body rode the info string (blank body, non-empty meta) SHALL render that meta as the source. The caption SHALL NOT appear on successfully mounted cards, ordinary code blocks, or streaming (unclosed) fences.

#### Scenario: A failed fence shows its source with a caption
- **WHEN** a ` ```ui ` fence's body violates the composition vocabulary
- **THEN** the transcript renders the fence source as a code block with a caption stating the card could not be rendered

#### Scenario: The rejection reason is available on hover
- **WHEN** a viewer hovers the caption of a degraded fence
- **THEN** the tooltip shows the validator's rejection reason

#### Scenario: Successful cards carry no caption
- **WHEN** any fence mounts its card element
- **THEN** no degraded-fence caption renders for that fence
