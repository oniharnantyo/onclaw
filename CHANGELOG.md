# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Agent lifecycle hooks now enforce on matching events.** Hooks were previously configurable, validatable, and auditable but silently never executed: the serving runner's hook chain resolved empty, so no hook ever fired. This deploy fixes that dispatch wiring.
- **Breaking-by-fix:** workspaces with stored workspace-level or agent-level hooks, and instance builtin/managed hooks, will see them fire on the next matching event. Review configured hooks before deploying — blocks now end turns with a `prompt_blocked` notice or return the canonical block JSON as the tool result.
- **The command-handler kill switch (`hooks.command_enabled`) now actually gates production hook execution** (default enabled). Operators who disable it get failing-closed policy absence exactly as documented: command hooks are treated as absent, not as a run failure.
- The hooks dry-run endpoint reports the current kill-switch state, so operators can confirm which handler classes are active before relying on them.
