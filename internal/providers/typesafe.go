package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TypeSafe /v1/systemone verify-probe constants: the minimal authenticated
// decision request carries one noul question over trivial state under the
// documented flagship model alias. The probe validates the endpoint and key;
// it makes no claim about the workspace's real decision model.
const (
	typesafeVerifyQuestion = "needs_memory"
	typesafeVerifyState    = "onclaw verify probe"
	typesafeVerifyModel    = "jev-latest"
)

// TypeSafeProvider implements connection verification for TypeSafe
// /v1/systemone decision endpoints. It is a decision-class provider (see
// IsDecisionType): it backs typed-decision calls, never chat or agent model
// resolution, and surfaces no models.
type TypeSafeProvider struct {
	client *http.Client
}

// NewTypeSafeProvider creates a new TypeSafe provider instance.
func NewTypeSafeProvider(client *http.Client) *TypeSafeProvider {
	return &TypeSafeProvider{client: httpClient(client)}
}

func (p *TypeSafeProvider) Type() string {
	return TypeTypeSafe
}

func (p *TypeSafeProvider) RequiresBaseURL() bool {
	return false
}

func (p *TypeSafeProvider) RequiresAPIKey() bool {
	return true
}

func (p *TypeSafeProvider) CanonicalOrigin() string {
	return TypesafeDefaultEndpoint
}

func (p *TypeSafeProvider) RequiresMaxTokens() bool {
	return false
}

func (p *TypeSafeProvider) ValidEfforts() []string {
	return nil
}

func (p *TypeSafeProvider) SupportsTemperature() bool {
	return false
}

// Verify probes the systemone endpoint (the stored base_url — the full
// endpoint including the resource path — or the canonical origin) with a
// minimal authenticated decision request: one noul question over trivial
// state. A 2xx with a parseable body carrying an answers key succeeds;
// any non-2xx (401/403 included) is reported through parseProviderError.
func (p *TypeSafeProvider) Verify(ctx context.Context, cred Credential) error {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return fmt.Errorf("%w: API key is required for typesafe provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, TypesafeDefaultEndpoint, "", false)
	if err != nil {
		return err
	}

	probe := struct {
		State     string                      `json:"state"`
		Model     string                      `json:"model"`
		Questions map[string]typesafeQuestion `json:"questions"`
	}{
		State: typesafeVerifyState,
		Model: typesafeVerifyModel,
		Questions: map[string]typesafeQuestion{
			// The map key IS the question id (docs: questions is
			// map<string, Question>); criteria is optional for noul and the
			// minimal probe omits it.
			typesafeVerifyQuestion: {
				Type:         "noul",
				Instructions: "report the classification probability of the probe state",
			},
		},
	}
	payload, err := json.Marshal(probe)
	if err != nil {
		return fmt.Errorf("marshal probe request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, probeURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "onclaw")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseProviderError(resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	// A success body must be parseable JSON carrying an answers key — the
	// shape every decision response rides (values may be {"noul": x} or a
	// bare number; the probe does not read them).
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("%w: typesafe verify response is not valid JSON", domain.ErrInvalid)
	}
	if _, ok := parsed["answers"]; !ok {
		return fmt.Errorf("%w: typesafe verify response does not contain an answers body", domain.ErrInvalid)
	}
	return nil
}

// ListModels returns no models: decision providers never surface model
// choices. The model-catalog resolution maps an empty live list to
// domain.ModelSourceNone, so a typesafe config never appears in a model
// picker.
func (p *TypeSafeProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	return []domain.Model{}, nil
}

// typesafeQuestion is one noul question of a systemone request. The map key
// it rides under is the question id — answers return under the same key; a
// noul criteria ({true, false} object) is optional and the probe omits it.
// Shared by the verify probe; the memory decision client builds its three
// questions over the same shape.
type typesafeQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}
