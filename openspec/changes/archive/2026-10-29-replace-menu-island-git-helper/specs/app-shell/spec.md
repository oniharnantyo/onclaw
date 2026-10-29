# Spec Delta

## ADDED Requirements

### Requirement: Frontend navigation menu

The shell's persistent navigation island (desktop rail) SHALL present the app's
destinations — chat, agents, schedules, knowledge, and settings — as a single
flat menu: compact full-width rows, each with a leading icon and a truncated
label, with no group headings between rows, in both light and dark themes. The
active destination SHALL be marked by a tinted selection pill on its row while
the row's label keeps the default foreground styling (the pill, not text color
or weight, carries the active state). The island SHALL render as a flat column
on the window canvas — with no bordered card, surface background, or rounded
chrome of its own — so the menu reads as part of the window edge, while the
content island remains the carded surface. The island's bottom cluster (theme
control, settings) SHALL use the same compact row geometry so the island reads
as one menu. When the rail is collapsed to the narrow icon rail, rows SHALL
become icon-only while each control keeps an accessible name matching its
destination. The below-desktop-breakpoint horizontal top-bar navigation is
unaffected by this requirement.

#### Scenario: Flat icon rows in the expanded island

- **WHEN** the navigation island is expanded
- **THEN** each destination renders as one compact full-width row with its
  leading icon and label, and no group heading text appears between rows

#### Scenario: Selection pill marks the active destination

- **WHEN** a destination is the active menu
- **THEN** its row is highlighted with the accent-tint pill while its label
  keeps the default foreground styling, and inactive rows show a tint only on
  hover

#### Scenario: Bottom cluster shares the menu geometry

- **WHEN** the theme control and the settings destination are shown in the
  island's bottom cluster
- **THEN** they render at the same compact row height as the menu rows, and
  activating the settings row displays the settings menu

#### Scenario: Collapsed rail stays operable

- **WHEN** the rail is collapsed to the narrow icon rail
- **THEN** the destination rows become icon-only and each control remains
  operable with an accessible name matching its destination label

#### Scenario: Island is a flat canvas column

- **WHEN** the desktop navigation island is shown at the desktop breakpoint
- **THEN** it renders without its own bordered card or surface background
  while its rows, selection pill, and bottom cluster keep the flat-menu
  grammar, and the content island remains the carded surface

#### Scenario: Narrow layout unaffected

- **WHEN** the window is below the desktop breakpoint
- **THEN** the horizontal top-bar navigation renders as before, without the
  island's flat-menu row grammar
