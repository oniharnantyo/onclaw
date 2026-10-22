# Spec Delta

## Purpose

Provide the single application shell that runs buddies in two hosts — a desktop host (window + system tray) and a headless server host (the same frontend served as a web app) — so the gateway and all services keep running regardless of window visibility or host.

## ADDED Requirements

### Requirement: Desktop background lifecycle

The desktop host SHALL keep the application running when the main window is closed; closing the window MUST hide it, not terminate the process. Background services (gateway, schedulers) MUST continue running while the window is hidden.

#### Scenario: User closes the window

- **WHEN** the user closes the main window on the desktop host
- **THEN** the window disappears, the process keeps running, and background services continue operating

#### Scenario: Reopening after close

- **WHEN** the user activates the tray menu item "Open" while the window is hidden
- **THEN** the main window becomes visible again with its previous state

### Requirement: System tray presence

The desktop host SHALL display a system tray icon whenever the app is running. The tray menu SHALL offer, at minimum: open the main window, show gateway running state, pause/resume the gateway, and quit the application. Quitting from the tray MUST be the only way to fully exit the app after the window has been closed.

#### Scenario: Tray menu contents

- **WHEN** the user opens the system tray menu
- **THEN** the menu shows an item to open the window, the current gateway state with a pause/resume toggle, and a quit item

#### Scenario: Quit from tray terminates the app

- **WHEN** the user selects Quit in the tray menu
- **THEN** the process terminates cleanly, stopping background services first

### Requirement: Server host serves the same app as web

The application SHALL build in a headless server mode (separate binary) that serves the same frontend and exposes the same bound services over HTTP/WebSocket as the desktop host. Server mode MUST NOT require native GUI libraries. The server SHALL bind to a loopback address by default and SHALL NOT accept remote connections unless explicitly configured.

#### Scenario: Web access to the same app

- **WHEN** the server-mode binary is running and a browser opens the configured address
- **THEN** the same frontend loads and the user can chat with agents with the same data as the desktop host

#### Scenario: Default binding is loopback

- **WHEN** the server-mode binary starts without an explicit bind address
- **THEN** it listens only on a loopback interface

### Requirement: Optional server bearer token

Server mode SHALL support an optional bearer token; when configured, requests and WebSocket connections without the token MUST be rejected. When not configured, no authentication is required (local single-user assumption).

#### Scenario: Token required when configured

- **WHEN** a token is configured and a client connects without presenting it
- **THEN** the request or connection is rejected

#### Scenario: Token absent from config

- **WHEN** no token is configured
- **THEN** clients connect without authentication

### Requirement: Service parity across hosts

All application services (agents, sessions, configuration, settings) SHALL be callable from both hosts through the same service API surface. Behavior MUST NOT differ between desktop and server hosts except for host-specific concerns (window, tray).

#### Scenario: Same operations in both hosts

- **WHEN** the user creates an agent and sends a chat message on the desktop host, then opens the server host's web UI against the same data directory
- **THEN** the agent exists and the session history is identical
