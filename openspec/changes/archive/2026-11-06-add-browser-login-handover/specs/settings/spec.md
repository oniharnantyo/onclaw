# settings Specification (delta)

> Note: this delta MODIFIES the `### Requirement: Browser Use pane` introduced
> by `rebrand-browser-use-system-mcp` (still un-archived). Archive order:
> rebrand first, then this change. Until then the base text lives only in the
> rebrand change's delta.

## MODIFIED Requirements

### Requirement: Browser Use pane

The Browser Use pane SHALL present a profiles-first configuration surface: a server status line fed by the system server's live connection state, a profile registry with create, delete, default-badge, and make-default controls, a Saved logins section (list, add, remove, reveal), and a detected-Chrome import. The pane SHALL NOT expose engine controls (headless, allow-private, idle timeout, action timeout); the Browser Use configuration SHALL persist under the browser tool settings key exactly as before, apply at the server's next spawn boundary, and be edited only through the make-default pass-through (all non-default fields re-saved unchanged). The detector SHALL list this machine's Google Chrome profiles (GOOS-aware user-data directory, display names from Chrome's `Local State` profile cache, `Default` first) and each candidate SHALL import with one click through the existing copy engine under a registry name derived from the display name; import collisions SHALL resolve to a suffixed name. The import disclosures (close the source browser first, copy size expectation, best-effort cookie decryption) SHALL render before any import starts. The pane SHALL NOT offer a manual source-path input. Vault logins SHALL list redacted views only (id, name, username, origin, addedBy) — the pane SHALL reveal a stored password only through an explicit reveal affordance. Creating a profile SHALL create its directory; deleting a profile SHALL remove it after its browser closes; the `default` profile SHALL not be deletable; deleting the configured default profile SHALL reset the default to `default`.

#### Scenario: Engine controls are absent

- **WHEN** the user opens the Browser Use pane
- **THEN** no headless, allow-private, idle-timeout, action-timeout, or engine save control appears — the pane shows only the status line, the profile registry, the Saved logins section, and the import action

#### Scenario: Make default passes other fields through

- **WHEN** the user presses Make default on profile `work` while the stored config carries non-default engine values
- **THEN** the saved config names `work` as the default and every other stored field is unchanged, applying at the server's next spawn boundary

#### Scenario: Detected Chrome profiles import with one click

- **WHEN** the machine has Google Chrome profiles `Personal` (Default) and `Work` (Profile 1) and the user presses Import on `Work`
- **THEN** the candidate lists with its Chrome display name, the import copies the profile directory under a registry name derived from `work` (suffixed on collision), the result names the final registry name, and the source directory is untouched

#### Scenario: Chrome absent

- **WHEN** Google Chrome has no user-data directory on this machine
- **THEN** the import section explains no Chrome profiles were found instead of erroring

#### Scenario: Import disclosures precede import

- **WHEN** the user opens Import from Chrome
- **THEN** closing the source browser, the copy size expectation, and best-effort cookie decryption are visible before any Import button takes effect

#### Scenario: Saved logins stay redacted until revealed

- **WHEN** the user opens the Saved logins section
- **THEN** rows show name, username, and origin but no password, and a stored password is shown only after the explicit reveal affordance

#### Scenario: Server status line reflects the system server

- **WHEN** the Browser Use server process is in a failed supervision state
- **THEN** the pane's status line shows the failed state with the reason

#### Scenario: Profile create and delete

- **WHEN** the user creates profile `test` and later deletes it
- **THEN** `~/.buddies/browser/profiles/test/` exists after creation, and after deletion it is gone and no longer listed
