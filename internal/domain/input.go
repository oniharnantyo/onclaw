package domain

// InputKind names a non-text user-input modality an agent's model may accept.
type InputKind string

const (
	InputKindImage InputKind = "image"
	InputKindPDF   InputKind = "pdf"
)

// InputSupport is the tri-state input-modality capability of a (provider, model) pair:
// Supported/Unsupported come from a catalog entry (Unsupported is affirmative —
// the entry exists and lacks the modality); Unknown means no catalog evidence
// (unmapped provider, absent entry, failed fetch) and fails open downstream.
type InputSupport string

const (
	InputSupported   InputSupport = "supported"
	InputUnsupported InputSupport = "unsupported"
	InputUnknown     InputSupport = "unknown"
)

// AgentInputModalities is the per-agent input-modality capability payload
// served alongside the agent (computed from the catalog at agent read time).
type AgentInputModalities struct {
	Image InputSupport `json:"image"`
	PDF   InputSupport `json:"pdf"`
}
