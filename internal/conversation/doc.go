// Package conversation implements the business-layer owner of conversation history
// and session management per Eino agent best practices (03.md).
//
// The SessionManager in this package owns history loading, history preprocessing,
// turn persistence, secret redaction, reasoning stripping, and token usage extraction.
package conversation
