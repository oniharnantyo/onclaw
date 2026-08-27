# web-app/workspaces Specification

## Purpose

Workspace (tenant) lifecycle in the UI: switching between workspaces, creating one with an optional starter agent, deleting one, and the zero-agent onboarding screen.

## Requirements

### Requirement: Workspace switching
A switcher SHALL list all known workspaces with plan badges and a create entry. Switching SHALL replace the active workspace, navigate to its first agent's chat, clear channel unread counts for the newly opened target, and show a confirmation toast.

#### Scenario: Switch workspace
- **WHEN** the user picks "Globex" from the switcher while in an Acme chat
- **THEN** the app navigates to Globex's first agent chat and toasts "Switched to Globex"

### Requirement: Workspace creation
Creating a workspace SHALL require a name (URL slug auto-derived and conflict-checked against existing slugs), a timezone, and an explicit choice of whether to deploy a starter agent. With the starter agent, the new workspace SHALL contain that agent, a `#general` channel bound to it, and a welcome message thread; without it, the workspace starts empty and lands on onboarding. The creator becomes its Owner.

#### Scenario: Slug conflict
- **WHEN** the user types a name whose derived slug is already taken
- **THEN** creation is blocked with a visible conflict message until the name changes

#### Scenario: Create with starter agent
- **WHEN** the user creates "Initech" with the starter agent enabled
- **THEN** the workspace opens on the starter agent's chat containing its welcome message

### Requirement: Onboarding screen
A workspace with zero agents SHALL present the onboarding screen: workspace-ready headline, explanation that a workspace is a container until it has agents, a primary "Deploy your first agent" action opening the agent configuration modal, a secondary path to workspace settings, and a plan/timezone footer.

#### Scenario: Deploy first agent from onboarding
- **WHEN** the user clicks "Deploy your first agent" and completes the modal
- **THEN** the new agent's chat opens and onboarding no longer appears for this workspace

### Requirement: Workspace deletion
Workspace deletion (from the settings danger zone) SHALL remove the workspace from the switcher, switch the app to another workspace's first agent chat, and be refused when it is the last remaining workspace.

#### Scenario: Delete and land elsewhere
- **WHEN** the user deletes the active workspace while two exist
- **THEN** the app switches to the remaining workspace and toasts the switch
