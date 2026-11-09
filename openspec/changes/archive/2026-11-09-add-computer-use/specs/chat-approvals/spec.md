## ADDED Requirements

### Requirement: Computer-use destructive actions require approval

A call to a fixed destructive subset of computer-use tools — killing an application, writing the clipboard, and typing into credential fields — SHALL park the turn on the approval seam exactly like a shell safety-pattern match: the pending request is persisted, the card names the requested action and its target, and the turn has no terminal outcome while pending. Approving lets the call proceed to the desktop; denying feeds the model a fixed refusal and the turn continues without executing. The remainder of the computer-use surface (observation, listing, zoom, verification) SHALL NOT require per-call approval — the permission warm-up gate is the consent boundary for those.

#### Scenario: Kill app parks on an approval card

- **WHEN** the model calls the computer-use tool that kills an application
- **THEN** the turn parks on a persisted approval card naming the action and target app, and no desktop action runs yet

#### Scenario: Deny continues the turn

- **WHEN** the user denies a pending computer-use approval
- **THEN** the model receives a fixed refusal as the tool result, the turn continues, and the targeted application is untouched
