package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/modelmeta"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListProviders retrieves all provider profiles and flags the default one.
func (s *Service) ListProviders(ctx context.Context) ([]ProviderView, error) {
	profiles, err := s.mgr.ListProfiles(ctx)
	if err != nil {
		return nil, classify(err)
	}

	defaultProvider, err := s.kv.Get(ctx, "default_provider")
	if err != nil {
		s.log.Debug("Failed to get default provider preference", "error", err)
	}

	resp := make([]ProviderView, 0, len(profiles))
	for _, p := range profiles {
		secret, err := s.mgr.GetSecret(ctx, p.Name)
		secretSet := (err == nil && secret != "")

		resp = append(resp, ProviderView{
			Name:         p.Name,
			ProviderType: p.ProviderType,
			APIBase:      p.APIBase,
			Settings:     p.Settings,
			Enabled:      p.Enabled != 0,
			IsDefault:    p.Name == defaultProvider,
			SecretSet:    secretSet,
		})
	}

	return resp, nil
}

// CreateProvider adds a new provider profile.
func (s *Service) CreateProvider(ctx context.Context, input ProfileInput) (*store.Profile, error) {
	enabledInt := 0
	if input.Enabled {
		enabledInt = 1
	}

	p := &store.Profile{
		Name:         input.Name,
		ProviderType: input.ProviderType,
		APIBase:      input.APIBase,
		Settings:     input.Settings,
		Enabled:      enabledInt,
	}

	if err := s.mgr.AddProfile(ctx, p); err != nil {
		return nil, classify(err)
	}
	return p, nil
}

// GetProvider retrieves a single provider by name.
func (s *Service) GetProvider(ctx context.Context, name string) (ProviderView, error) {
	p, err := s.mgr.GetProfile(ctx, name)
	if err != nil {
		return ProviderView{}, classify(err)
	}

	defaultProvider, _ := s.kv.Get(ctx, "default_provider")

	secret, err := s.mgr.GetSecret(ctx, p.Name)
	secretSet := (err == nil && secret != "")

	return ProviderView{
		Name:         p.Name,
		ProviderType: p.ProviderType,
		APIBase:      p.APIBase,
		Settings:     p.Settings,
		Enabled:      p.Enabled != 0,
		IsDefault:    p.Name == defaultProvider,
		SecretSet:    secretSet,
	}, nil
}

// UpdateProvider updates provider profile by recreation, preserving secrets.
func (s *Service) UpdateProvider(ctx context.Context, name string, input ProfileInput) (*store.Profile, error) {
	if _, err := s.mgr.GetProfile(ctx, name); err != nil {
		return nil, classify(err)
	}

	enabledInt := 0
	if input.Enabled {
		enabledInt = 1
	}

	// Read API key before removal to preserve it
	secret, err := s.mgr.GetSecret(ctx, name)
	hasSecret := (err == nil && secret != "")

	if err := s.mgr.RemoveProfile(ctx, name); err != nil {
		return nil, classify(err)
	}

	p := &store.Profile{
		Name:         name,
		ProviderType: input.ProviderType,
		APIBase:      input.APIBase,
		Settings:     input.Settings,
		Enabled:      enabledInt,
	}

	if err := s.mgr.AddProfile(ctx, p); err != nil {
		return nil, classify(err)
	}

	if hasSecret {
		if err := s.mgr.SetSecret(ctx, name, secret); err != nil {
			s.log.Error("failed to restore secret during provider update", "error", err)
		}
	}

	return p, nil
}

// DeleteProvider deletes a provider profile, its secret and clears preference if it was default.
func (s *Service) DeleteProvider(ctx context.Context, name string) error {
	if _, err := s.mgr.GetProfile(ctx, name); err != nil {
		return classify(err)
	}

	if err := s.mgr.RemoveProfile(ctx, name); err != nil {
		return classify(err)
	}

	defaultProvider, _ := s.kv.Get(ctx, "default_provider")
	if defaultProvider == name {
		_ = s.kv.Delete(ctx, "default_provider")
	}

	return nil
}

// SetDefaultProvider sets a provider as the default preference.
func (s *Service) SetDefaultProvider(ctx context.Context, name string) error {
	if _, err := s.mgr.GetProfile(ctx, name); err != nil {
		return classify(err)
	}

	return classify(s.kv.Set(ctx, "default_provider", name))
}

// GetSecretStatus returns status and partial hint of provider key.
func (s *Service) GetSecretStatus(ctx context.Context, name string) (SecretStatus, error) {
	if _, err := s.mgr.GetProfile(ctx, name); err != nil {
		return SecretStatus{}, classify(err)
	}

	secret, err := s.mgr.GetSecret(ctx, name)
	if err != nil {
		return SecretStatus{}, classify(err)
	}

	var status SecretStatus
	if secret != "" {
		status.Set = true
		if len(secret) > 4 {
			status.Hint = "..." + secret[len(secret)-4:]
		} else {
			status.Hint = "..."
		}
	} else {
		status.Set = false
	}

	return status, nil
}

// SetSecret sets the secret key for a provider.
func (s *Service) SetSecret(ctx context.Context, name string, apiKey string) error {
	if _, err := s.mgr.GetProfile(ctx, name); err != nil {
		return classify(err)
	}

	return classify(s.mgr.SetSecret(ctx, name, apiKey))
}

// ListProviderModels retrieves discovered models for the provider and resolves their metadata.
func (s *Service) ListProviderModels(ctx context.Context, providerName string) (*ProviderModelsResponse, error) {
	p, err := s.mgr.GetProfile(ctx, providerName)
	if err != nil {
		return nil, classify(err)
	}

	apiKey, _ := s.mgr.ResolveSecret(ctx, providerName)

	catalog, _ := modelmeta.GetCatalog()

	// Setup context cache for models discovery to avoid N+1 requests
	ctx = context.WithValue(ctx, modelmeta.OpenaiModelsCacheKey, &modelmeta.ModelCache{})

	models, err := modelmeta.Enumerate(ctx, p.ProviderType, p.APIBase, apiKey)
	if err != nil || len(models) == 0 {
		s.log.Warn("Failed to enumerate models for provider, falling back to catalog", "provider", providerName, "error", err)

		var warning string
		if err != nil {
			warning = fmt.Sprintf("Failed to enumerate models: %v. Showing catalog defaults.", err)
		} else {
			warning = "No models returned from provider. Showing catalog defaults."
		}

		var modelViews []ProviderModelView
		if catalog != nil {
			if provObj, ok := catalog.Providers[strings.ToLower(p.ProviderType)]; ok {
				for modelID, mObj := range provObj.Models {
					var opts []ProviderModelReasoningOption
					for _, opt := range mObj.ReasoningOptions {
						opts = append(opts, ProviderModelReasoningOption{
							Type:   opt.Type,
							Values: opt.Values,
							Min:    opt.Min,
							Max:    opt.Max,
						})
					}
					modelViews = append(modelViews, ProviderModelView{
						ID:               modelID,
						ContextWindow:    mObj.Limit.Context,
						Thinking:         mObj.Reasoning,
						InputModalities:  mObj.Modalities.Input,
						ReasoningOptions: opts,
					})
				}
			}
		}

		sort.Slice(modelViews, func(i, j int) bool {
			return modelViews[i].ID < modelViews[j].ID
		})

		return &ProviderModelsResponse{
			Models:  modelViews,
			Warning: warning,
		}, nil
	}

	var modelViews []ProviderModelView
	for _, m := range models {
		mMeta := modelmeta.Resolve(ctx, m, p.ProviderType, p.APIBase, apiKey, catalog)
		var opts []ProviderModelReasoningOption
		for _, opt := range mMeta.ReasoningOptions {
			opts = append(opts, ProviderModelReasoningOption{
				Type:   opt.Type,
				Values: opt.Values,
				Min:    opt.Min,
				Max:    opt.Max,
			})
		}
		modelViews = append(modelViews, ProviderModelView{
			ID:               m,
			ContextWindow:    mMeta.ContextWindow,
			Thinking:         mMeta.Thinking,
			InputModalities:  mMeta.InputModalities,
			ReasoningOptions: opts,
		})
	}

	sort.Slice(modelViews, func(i, j int) bool {
		return modelViews[i].ID < modelViews[j].ID
	})

	return &ProviderModelsResponse{
		Models: modelViews,
	}, nil
}

// TestConnectionResponse holds the response from testing provider connection.
type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TestConnection tests the provider connection by attempting to enumerate models.
// It uses the stored credentials for the named provider.
func (s *Service) TestConnection(ctx context.Context, providerName string) (*TestConnectionResponse, error) {
	p, err := s.mgr.GetProfile(ctx, providerName)
	if err != nil {
		return &TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Provider not found: %s", providerName),
		}, classify(err)
	}

	if p.Enabled == 0 {
		return &TestConnectionResponse{
			Success: false,
			Message: "Provider is disabled",
		}, nil
	}

	apiKey, err := s.mgr.ResolveSecret(ctx, providerName)
	if err != nil {
		return &TestConnectionResponse{
			Success: false,
			Message: "API key not configured for this provider",
		}, nil
	}

	// Test connection by attempting to enumerate models
	catalog, _ := modelmeta.GetCatalog()

	// Setup context cache for models discovery
	ctx = context.WithValue(ctx, modelmeta.OpenaiModelsCacheKey, &modelmeta.ModelCache{})

	models, err := modelmeta.Enumerate(ctx, p.ProviderType, p.APIBase, apiKey)
	if err != nil {
		return &TestConnectionResponse{
			Success: false,
			Message: fmt.Sprintf("Connection failed: %v", err),
		}, nil
	}

	if len(models) == 0 {
		// Try to get models from catalog as fallback
		if catalog != nil {
			if provObj, ok := catalog.Providers[strings.ToLower(p.ProviderType)]; ok {
				if len(provObj.Models) > 0 {
					return &TestConnectionResponse{
						Success: true,
						Message: "Connection successful (using catalog fallback)",
					}, nil
				}
			}
		}
		return &TestConnectionResponse{
			Success: false,
			Message: "Connection succeeded but no models available",
		}, nil
	}

	return &TestConnectionResponse{
		Success: true,
		Message: "Connection successful",
	}, nil
}
