# agent-memory-graph Delta

## Purpose
The associative memory store — the source paper's entity–event graph: tenant-scoped entity labels connected by bipartite edges to extracted notes and events, resolved during extraction at zero extra model cost, and traversable at read time through deterministic, identity-bound hops so associative queries ("this project, its incidents, one incident's detail") retrieve without lexical luck.

## ADDED Requirements

### Requirement: Tenant-scoped entity store
Entities SHALL be labels scoped to their workspace partition (workspace_id on every row, the hard tenant boundary) with a provenance stamp (origin, learned_at, first source event id). Entity labels themselves SHALL carry no visibility tier — they are pointers, not content — and all access control SHALL live on the edges and linked rows. Entity creation SHALL be idempotent per workspace: resolving the same normalized label yields the same entity row.

#### Scenario: Same label resolves to one entity
- **WHEN** extraction proposes an entity whose normalized label already exists in the workspace
- **THEN** the existing entity row is linked, not duplicated

#### Scenario: Entities never cross tenants
- **WHEN** two workspaces both hold an entity with the same label
- **THEN** they are distinct rows and no read in one workspace can observe the other's

### Requirement: Narrowest-visibility edges
Edges SHALL connect entities to notes and events as a bipartite graph, and each edge SHALL inherit the narrowest visibility of what it links: an edge to a user-visibility row is traversable only by callers who could read that row directly. Edge creation SHALL be stamped with origin and source event id; edges are never widened after creation — widening happens only through the existing human promotion path on the linked row.

#### Scenario: Edge inherits the narrowest endpoint
- **WHEN** an event in a single-human direct chat mentions a shared project entity
- **THEN** the edge to that event is user-visibility (the event's tier), and only that event's owner reaches it through traversal

#### Scenario: Promotion widens through the row, not the edge
- **WHEN** a human promotes a user-visibility note to shared
- **THEN** traversal through the note's edges reaches it exactly as far as the direct read path does — no separate edge widening exists

### Requirement: Identity-bound traversal
Traversal SHALL be a deterministic sequence of database hops — seed entity match, expand edges, fetch linked rows — with zero model calls, computed entirely within the caller's visible set (workspace-shared rows, the caller's own user rows, the serving agent's rows). Traversal SHALL be reachable through the search tool's entity filter and through associative prefetch routing, and SHALL respect the same filters as direct search (time window, limit). Traversal SHALL be depth-bounded (entity → linked rows; v1 does not walk entity-to-entity chains).

#### Scenario: Traversal is identity-bound
- **WHEN** two different members traverse the same entity with the same query
- **THEN** each receives only the linked rows their own visible set allows, computed in the query layer

#### Scenario: Traversal adds no model cost
- **WHEN** an associative query resolves through entity traversal
- **THEN** the traversal performs database hops only — no side-call is issued for the read path

### Requirement: Nightly entity hygiene
The nightly consolidator SHALL merge duplicate entity rows whose normalized labels match (preserving every edge and evidence stamp), and SHALL report entity-merge counts in the morning report. Merging SHALL be copy-out-only with supersede semantics: no edge or provenance row is ever deleted.

#### Scenario: Duplicates fold overnight
- **WHEN** the consolidator finds two entity rows whose normalized labels are equal
- **THEN** edges and evidence are re-pointed to one surviving row and the morning report counts the merge
