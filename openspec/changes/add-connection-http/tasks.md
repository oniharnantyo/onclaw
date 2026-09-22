## 1. Domain & Recipes

- [ ] 1.1 Add connection kind values (`mcp` default, `http`) to recipe descriptors with the kind-specific fields: base URL, auth header name, probe call definition, and declared verb tools (name, method, path template, typed parameter schema, description)
- [ ] 1.2 Figma reference recipe: declared http kind, curated verb list (drafted from the OpenAPI spec, pruned to the workflows that matter — file/file-tree reads, comments, project listings), guided token steps, access-level scopes, probe call — values marked for live-verification pinning (tier annotations join when the authority-gate change lands)

## 2. Connection Tool Source

- [ ] 2.1 Runner seam: `ConnectionToolSource` consulted at tool resolution after built-ins alongside MCP tools, yielding one tool per declared verb per attached HTTP-kind connection
- [ ] 2.2 Verb tool generation: parameter schema from the declaration, pre-bound method/path, bind-time parameter validation (encoding, traversal and absolute-URL rejection), post-join pin re-validation, off-host redirect refusal; provider errors surfaced as tool errors without credential material
- [ ] 2.3 Server-side credential injection: resolver-function handshake with the connections service, token decrypted at call time, never present in schema/description/errors
- [ ] 2.4 Response discipline: size cap, content-type gating, truncation markers matching the web fetch tool's rules
- [ ] 2.5 Degradation: unresolvable credential skips and marks the connection's tools, run proceeds (matching MCP skip-and-mark)

## 3. Connections Service & API

- [ ] 3.1 Kind-aware create: HTTP kind skips materialization; probe executes the recipe's call with the injected header; store-nothing-on-failure hygiene
- [ ] 3.2 Kind-aware probe/disconnect: probe re-runs the recipe call; disconnect cascades connection + attachments (no server row to remove)
- [ ] 3.3 API surface: existing connection endpoints carry kind through; no new routes

## 4. Web

- [ ] 4.1 Kind-aware gallery copy: connect dialog guided steps for HTTP recipes (API token + scopes), card copy naming the declared verb surface, agent-attach rows unchanged
- [ ] 4.2 Design-contract conformance for the dialog variant

## 5. Verification

- [ ] 5.1 Unit tests: verb generation (schema, pre-binding), bind-time validation (traversal/absolute-URL rejection, post-join pin re-validation), off-host redirect refusal, credential-injection hygiene (no token in errors), degradation, kind-aware create/disconnect, undeclared-operation impossibility
- [ ] 5.2 HTTP tests: connect/probe/disconnect lifecycle for an HTTP-kind recipe against a stub API
- [ ] 5.3 Recipe live-verification (Figma): confirm base URL, auth header, probe call with a real PAT; pin the values
- [ ] 5.4 `go build ./...`, `go vet ./...`, `go test ./...` green; web typecheck + tests green
- [ ] 5.5 Smoke coverage: Figma-like stub connection lifecycle end-to-end
- [ ] 5.6 Manual pass: connect Figma, attach an agent, run a file-listing request, verify transcript shows the tool call without token leakage
