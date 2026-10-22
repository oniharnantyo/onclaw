package promptgen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultTimeout is the default duration allocated to a synchronous prompt
// generation.
const DefaultTimeout = 3 * time.Minute

// Option configures the generation Service.
type Option func(*Service)

// WithTimeout sets the per-generation context timeout applied around the model
// call. Non-positive values are ignored.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if timeout > 0 {
			s.timeout = timeout
		}
	}
}

// Service orchestrates agent prompt generation: build the model, invoke it with
// the generation prompt, parse the three documents, and commit them through the
// DocStore. It performs no database access and no prompt-status transitions —
// the caller (services layer) owns the generating → ready | failed machine and
// persists the returned documents' status.
type Service struct {
	factory ModelFactory
	docs    DocStore
	timeout time.Duration
}

// NewService creates a new prompt generation service.
func NewService(factory ModelFactory, docs DocStore, opts ...Option) *Service {
	svc := &Service{
		factory: factory,
		docs:    docs,
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// GenerateForCreate runs a fresh generation for a not-yet-generated agent slug
// and commits the documents. A new agent owns no documents, so this is always
// fresh generation from the name and brief; a non-empty instruction still rides
// along as a requested-changes section. On any failure nothing is committed.
func (s *Service) GenerateForCreate(ctx context.Context, slug string, p Params) (Documents, error) {
	if err := validateRequest(slug, p); err != nil {
		return Documents{}, err
	}
	docs, err := s.generateDocuments(ctx, p, nil)
	if err != nil {
		return Documents{}, err
	}
	if err := s.commit(slug, docs); err != nil {
		return Documents{}, err
	}
	return docs, nil
}

// Regenerate regenerates an agent's documents. When the workspace holds
// generated documents the call runs in enhance mode: the current document
// contents are always sent to the model with instructions to strengthen them —
// preserving the established voice and structure and incorporating the current
// brief — with or without an instruction. With no documents it runs fresh and
// the instruction rides along as a requested-changes section. A failed
// generation leaves the existing documents untouched: CommitDocuments backs up
// each overwritten document and aborts the whole commit on any backup failure.
func (s *Service) Regenerate(ctx context.Context, slug string, p Params) (Documents, error) {
	if err := validateRequest(slug, p); err != nil {
		return Documents{}, err
	}

	current, exists, err := s.docs.ReadDocuments(slug)
	if err != nil {
		return Documents{}, fmt.Errorf("read current prompt documents for %q: %w", slug, err)
	}

	// A workspace holding no documents — or only blanks — has nothing worth
	// enhancing; regenerate fresh.
	var enhance *Documents
	if exists && !current.blank() {
		enhance = &current
	}

	docs, err := s.generateDocuments(ctx, p, enhance)
	if err != nil {
		return Documents{}, err
	}
	if err := s.commit(slug, docs); err != nil {
		return Documents{}, err
	}
	return docs, nil
}

// commit writes the documents through the DocStore. The DocStore's backup
// contract makes this atomic from the caller's perspective: a backup or write
// failure means no document changed, so the generation fails and the previous
// documents stay retryable.
func (s *Service) commit(slug string, docs Documents) error {
	if s.docs == nil {
		return errors.New("prompt generation has no document store configured")
	}
	if err := s.docs.CommitDocuments(slug, docs); err != nil {
		return fmt.Errorf("commit prompt documents for %q: %w", slug, err)
	}
	return nil
}

// validateRequest rejects requests the generation cannot serve: the slug keys
// the DocStore, the name fills the IDENTITY document's load-bearing name field,
// and the brief drives the whole generation.
func validateRequest(slug string, p Params) error {
	switch {
	case strings.TrimSpace(slug) == "":
		return fmt.Errorf("%w: agent slug is empty", ErrInvalid)
	case strings.TrimSpace(p.AgentName) == "":
		return fmt.Errorf("%w: agent name is empty", ErrInvalid)
	case strings.TrimSpace(p.Brief) == "":
		return fmt.Errorf("%w: agent brief is empty", ErrInvalid)
	}
	return nil
}

// generateDocuments builds the model, invokes it bounded by the service timeout
// (3 minutes by default) so a slow or hung provider surfaces a timeout reason
// instead of hanging the request, and parses the three documents. The current
// documents, when non-nil, switch the call to enhance mode. Model and adapter
// errors are sanitized before returning: the provider's API key is redacted and
// the raw provider error is replaced by a safe, single-line reason, never
// carried through %w.
func (s *Service) generateDocuments(ctx context.Context, p Params, current *Documents) (Documents, error) {
	if s.factory == nil {
		return Documents{}, errors.New("prompt generation has no model factory configured")
	}

	// sanitizeReason maps a provider-produced error to a user-safe reason with
	// the provider's own API key redacted: adapter and HTTP errors can embed
	// the credential from the request they failed.
	sanitizeReason := func(err error) string {
		return SanitizeErrorString(redactKey(err.Error(), p.Provider.APIKey))
	}

	chatModel, err := s.factory.Build(ctx, p.Provider, p.Model)
	if err != nil {
		return Documents{}, fmt.Errorf("failed to initialize model adapter: %s", sanitizeReason(err))
	}

	genCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := chatModel.Generate(genCtx, BuildGenerationMessages(p, current), generationOptions()...)
	if err != nil {
		return Documents{}, errors.New(sanitizeReason(err))
	}

	if resp == nil {
		return Documents{}, errors.New("model returned empty response")
	}

	docs, err := ParseGenerationOutput(resp)
	if err != nil {
		// Replace the raw parse failure with the canonical reason: the model
		// output is provider-controlled text and must not reach the user.
		return Documents{}, errors.New("failed to parse generated identity, soul, and bootstrap prompts from model output")
	}

	return docs, nil
}

// redactKey removes the provider's API key from an error message so the
// credential can never reach a user-facing reason. An empty key redacts
// nothing.
func redactKey(msg, key string) string {
	if key == "" {
		return msg
	}
	return strings.ReplaceAll(msg, key, "<redacted>")
}

// SanitizeError extracts and sanitizes an error for user-facing display.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeErrorString(err.Error())
}

// SanitizeErrorString ensures error messages are concise, safe to display, and
// actionable: provider errors are mapped to a stable reason that cannot carry
// the API key or other request secrets.
func SanitizeErrorString(msg string) string {
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "invalid api key") || strings.Contains(lower, "authentication"):
		return "provider authentication failed — check provider API key"
	case strings.Contains(lower, "403") || strings.Contains(lower, "permission_denied") || strings.Contains(lower, "forbidden"):
		return "provider permission denied — check API key permissions"
	case strings.Contains(lower, "model_not_found") || strings.Contains(lower, "model not found") || (strings.Contains(lower, "404") && strings.Contains(lower, "model")):
		return "configured model not found or inaccessible with provider credentials"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "quota") || strings.Contains(lower, "resource_exhausted"):
		return "provider rate limit or quota exceeded — retry later"
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout"):
		return "prompt generation timed out — retry"
	case strings.Contains(lower, "parse"):
		return "failed to parse generated identity, soul, and bootstrap prompts from model output"
	default:
		// Clean up any potential sensitive strings or excessive verbosity
		clean := strings.TrimSpace(msg)
		if idx := strings.Index(clean, "\n"); idx != -1 {
			clean = clean[:idx]
		}
		if len(clean) > 150 {
			clean = clean[:147] + "..."
		}
		return clean
	}
}
