// Package webhooks is the connection webhook ingress and management surface
// (add-connection-webhooks): recipes declare webhook support as data, and
// this package turns verified provider deliveries into agent runs for the
// connection's bound target.
//
// The package mirrors the gateway pipeline's posture (design.md D1 — the
// gateway SHAPE, none of its chat code):
//
//		verify → dedupe → queue → render → route → run
//
//	  - Ingress (ingress.go): the public delivery path. Signature verification
//	    runs constant-time (verify.go) against the connection's decrypted
//	    secret before any work; unknown/disabled/mis-signed deliveries all get
//	    one generic non-enumerating outcome. Dedupe rides the store's
//	    ack-after-persist primitive (design.md D3). Verified deliveries queue
//	    (queue.go, design.md D6) behind a fast provider-friendly ack and
//	    process asynchronously.
//	  - Rendering (render.go): the recipe's per-event template interpolates a
//	    whitelisted set of payload fields, fails closed on a missing field,
//	    and wraps the result in labeled-data markers — event content is DATA,
//	    never instructions (design.md D4).
//	  - Routing: the rendered turn goes to the bound agent in the bound
//	    thread/channel through RunnerPort with service-authority attribution
//	    (design.md D5). The port is narrow and runner-owned in spirit; the
//	    composition root adapts *agents.Runner to it (the gateways
//	    RunSubmitter precedent), keeping this package an ordinary ingress
//	    client that never imports the runner.
//	  - Service (service.go): the management surface — enable (secret
//	    generation, display-once), disable, rotate, target and event updates
//	    (design.md D2).
//
// Dependencies are granular and positional (AGENTS.md): two store sub-
// interfaces, the secret cipher, the queue, and the runner port. Injected
// dependencies are never nil.
package webhooks
