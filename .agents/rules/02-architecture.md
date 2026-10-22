---
description: "Hexagonal architecture: layer boundaries and dependency direction"
---
# Rule: Architecture (hexagonal boundaries)

## The model
A **port** is the core's socket to the outside world - nothing more. The
core (application) owns its boundary; adapters are the only "outside"
things that plug into it.

```
        OUTSIDE WORLD - adapters
  ┌───────────────────────────────────────────────────────────┐
  │ driver adapters (http, consumer)         - plug in        │
  │ driven adapters (repositories, publishers)- plug in       │
  └───────────────────────────────────────────────────────────┘
                │                 ▲
       Driver-Port          Driven-Port
       (ports/driver)       (ports/driven)
                ▼                 │
  ┌───────────────────────────────────────────────────────────┐
  │ CORE - application/usecases (do stuff)                    │
  │         domain underneath                                 │
  └───────────────────────────────────────────────────────────┘
```

## Hard architectural rules (enforced by `make arch` / go-arch-lint - never bypass them)
1. `internal/domain/**` imports nothing outside the standard library.
2. `internal/application/ports/**` imports `internal/domain` only - ports are pure interfaces (+ port-level input structs).
3. `internal/application/usecases/**` imports ports/domain/pkg - never adapters.
4. Driver adapters (`internal/adapters/http/**`, `internal/adapters/consumer/**`) call the core ONLY through **driver ports** (`internal/application/ports/driver/**`) - never a concrete use case, and never repository or driven adapters (not even in tests).
5. Driven adapters (`internal/adapters/repositories/**`, `internal/adapters/publishers/**`) implement **driven ports** (`internal/application/ports/driven/**`) - never import use cases or driver adapters directly; compose in `main.go` wiring instead.
6. Constructors (`NewXxx`) for use cases and driven adapters MUST always return their port interface (e.g. `NewCreateNoteUsecase(...) driver.NoteCreator`, `NewStore() repositories.NoteRepository`, `NewPublisher(...) events.NoteEventPublisher`) - callers wire interfaces, never concrete structs.
7. `config/` and `pkg/` import nothing project-internal except where shown in `.go-arch-lint.yml`.
8. All dependencies are constructor-injected in the composition root (`internal/bootstrap/server.go`). No globals, no service locators, no init()-based wiring.

## How to add a feature slice
1. `internal/domain/<aggregate>.go` - entity, invariants, errors, domain events (pure Go, stdlib only). One file per aggregate.
2. `internal/application/ports/driver/usecases/<capability>.go` - one **driver port** interface per capability, behavior-named (e.g. `NoteCreator`, `NoteLister`).
3. `internal/application/ports/driven/repositories/<repository>.go` - the **driven port** interface the core needs (e.g. `note_repository.go`). Add `events/publisher.go`, `cache.go`, ... in subfolders as new needs appear.
4. `internal/application/usecases/<domain>/` - one standalone struct per use case (grouped per domain: `note/`) with its own `NewXxx` constructor returning the driver port interface. The composition root (`internal/bootstrap/server.go`) inits them directly and passes each to the adapters that need it - no facade/service aggregator in between.
5. `internal/adapters/repositories/<backend>/<table>.go` - implementation(s) of driven repository ports (e.g. `postgres/notes.go` returning `repositories.NoteRepository`).
6. `internal/adapters/publishers/<backend>/<publisher>.go` - implementation(s) of driven event ports (e.g. `memory/publisher.go` returning `events.NoteEventPublisher`).
7. `internal/adapters/http/` - HTTP driver adapter: bootstrap in `server.go`, error envelope in `errors.go`, subpackages per concern (`health/`, `middleware/`, and one per resource).
8. `internal/adapters/consumer/` - event consumer driver adapter: consumes inbound messages/events and calls driver ports.
9. `internal/bootstrap/server.go` - wire: driven adapters -> use cases -> driver ports -> driver adapters.
10. Tests at every layer (see 04-testing.md); update `docs/openapi.yaml` + `docs/architecture.md`.

## Conventions
- Each package has a doc comment stating its single responsibility; keep it true.
- Ports are owned by the core (application), not by the implementer (adapters).
- Driver adapters only call driver ports; driven adapters only implement driven ports.
- Interfaces stay small (per capability); composite ports are assembled from granular ones.
- Neither side of an adapter pair may bypass ports to reach the other side.
- Features stay vertically coherent: all layers present or none (no orphan domain types).
- Errors carry domain meaning; adapters map them (see the HTTP error envelope in `docs/architecture.md`).
