# Spec Delta

## Purpose

Add the two presentation behaviors validated in the shell preview on top of
the settings navigation and providers port: a pane-title action row and
brand-identified provider rows.

## ADDED Requirements

### Requirement: Pane header action row

Each settings pane's content SHALL present its title with a top-right action
slot on the same row. Providers SHALL place its primary action ("Add
provider") there; panes without a primary action SHALL render the title
alone. The action row SHALL stay on one line at desktop widths in both
themes.

#### Scenario: Add provider sits beside the pane title

- **WHEN** the user opens the Providers pane
- **THEN** the pane title row shows the title on the left and the "Add
  provider" action on the right

### Requirement: Provider row identity

Each provider row SHALL lead with a brand logo tile sized consistently with
the row (known providers use their brand mark; unknown providers fall back to
an initial glyph on the same tile), followed by the provider name, the
model/credential line in the mono face (key-state hint only, per credential
secrecy), and the verification status. Logo tiles SHALL render legibly in
both themes.

#### Scenario: Provider row shows its brand

- **WHEN** the Providers pane lists a provider with a known brand mark
- **THEN** the row leads with that provider's logo tile before its name

#### Scenario: Unknown provider falls back

- **WHEN** the Providers pane lists a provider type without a known brand
  mark
- **THEN** the row shows an initial-glyph tile in the same size and position
