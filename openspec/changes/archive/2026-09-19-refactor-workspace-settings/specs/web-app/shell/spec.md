## MODIFIED Requirements

### Requirement: Responsive navigation
The icon rail SHALL remain visible at every viewport width except on the `/settings` routes, where settings renders as a full-screen surface without the rail (see the settings capability). The sidebar SHALL render as a static column at viewport widths ≥768px and as an off-canvas drawer below 768px, opened from a control in the rail and dismissible. The channel members panel SHALL render as a static column at widths ≥1280px and as a slide-over sheet below 1280px. At every width in the contract viewport matrix (360×800 through 1920×1080) the app MUST NOT scroll horizontally.

#### Scenario: Mobile navigation
- **WHEN** the viewport is 390px wide
- **THEN** the rail is visible, the sidebar is hidden until opened as a drawer, and no horizontal scrollbar appears

#### Scenario: Tablet width
- **WHEN** the viewport is 820px wide
- **THEN** the sidebar renders as a static column beside the chat

#### Scenario: Settings takeover
- **WHEN** the user navigates to any `/settings` route at any viewport width
- **THEN** the icon rail is not rendered and the settings surface occupies the full viewport
