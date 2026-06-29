package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/modelmeta"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// pickModel interactively guides the user to select a model and returns the model ID, its metadata, the chosen effort level, and the budget tokens.
func pickModel(ctx context.Context, mgr *llm.Service, providerName string, in io.Reader, out io.Writer) (string, store.ModelMetadata, string, int, error) {
	p, err := mgr.GetProfile(ctx, providerName)
	if err != nil {
		return "", store.ModelMetadata{}, "", 0, fmt.Errorf("provider %q not found: %w", providerName, err)
	}

	apiKey, _ := mgr.ResolveSecret(ctx, providerName)

	// 1. Get models.dev catalog
	catalog, _ := modelmeta.GetCatalog()

	// Setup context cache for models discovery to avoid N+1 requests
	ctx = context.WithValue(ctx, modelmeta.OpenaiModelsCacheKey, &modelmeta.ModelCache{})

	// 2. Enumerate models
	fmt.Fprintln(out, "Discovering models from provider...")
	models, err := modelmeta.Enumerate(ctx, p.ProviderType, p.APIBase, apiKey)
	if err != nil {
		fmt.Fprintf(out, "WARNING: failed to enumerate models: %v. Please enter model name manually.\n", err)
	}

	var modelID string
	var meta store.ModelMetadata

	if len(models) > 0 {
		var choices []string
		for _, m := range models {
			mMeta := modelmeta.Resolve(ctx, m, p.ProviderType, p.APIBase, apiKey, catalog)
			choices = append(choices, fmt.Sprintf("%s (context: %d, thinking: %t, modalities: %s)",
				m, mMeta.ContextWindow, mMeta.Thinking, strings.Join(mMeta.InputModalities, ",")))
		}
		choices = append(choices, "Enter custom model name manually...")

		idx, err := promptChoice("Select a model", choices, in, out)
		if err != nil {
			return "", store.ModelMetadata{}, "", 0, err
		}

		if idx == len(models) {
			// Manual entry
			modelID, err = promptString("Enter model name", "", in, out)
			if err != nil {
				return "", store.ModelMetadata{}, "", 0, err
			}
			meta = modelmeta.Resolve(ctx, modelID, p.ProviderType, p.APIBase, apiKey, catalog)
		} else {
			modelID = models[idx]
			meta = modelmeta.Resolve(ctx, modelID, p.ProviderType, p.APIBase, apiKey, catalog)
		}
	} else {
		modelID, err = promptString("Enter model name", "", in, out)
		if err != nil {
			return "", store.ModelMetadata{}, "", 0, err
		}
		meta = modelmeta.Resolve(ctx, modelID, p.ProviderType, p.APIBase, apiKey, catalog)
	}

	// 3. Optional context-window override
	override, err := promptConfirm(fmt.Sprintf("Override default context window (%d)?", meta.ContextWindow), false, in, out)
	if err != nil {
		return "", store.ModelMetadata{}, "", 0, err
	}
	if override {
		for {
			cwStr, err := promptString("Enter context window size in tokens", "", in, out)
			if err != nil {
				return "", store.ModelMetadata{}, "", 0, err
			}
			cw, err := strconv.Atoi(cwStr)
			if err == nil && cw > 0 {
				meta.ContextWindow = cw
				break
			}
			fmt.Fprintln(out, "Please enter a valid positive integer.")
		}
	}

	// 4. Optional interactive reasoning prompting on thinking models
	var chosenEffort string
	var chosenBudget int

	if len(meta.ReasoningOptions) > 0 {
		var primaryOpt *store.ReasoningOption
		// Find primary option based on precedence: effort -> budget_tokens -> toggle
		for i := range meta.ReasoningOptions {
			if meta.ReasoningOptions[i].Type == "effort" {
				primaryOpt = &meta.ReasoningOptions[i]
				break
			}
		}
		if primaryOpt == nil {
			for i := range meta.ReasoningOptions {
				if meta.ReasoningOptions[i].Type == "budget_tokens" {
					primaryOpt = &meta.ReasoningOptions[i]
					break
				}
			}
		}
		if primaryOpt == nil {
			for i := range meta.ReasoningOptions {
				if meta.ReasoningOptions[i].Type == "toggle" {
					primaryOpt = &meta.ReasoningOptions[i]
					break
				}
			}
		}

		if primaryOpt != nil {
			switch primaryOpt.Type {
			case "effort":
				if len(primaryOpt.Values) > 0 {
					idx, err := promptChoice("Select reasoning effort", primaryOpt.Values, in, out)
					if err != nil {
						return "", store.ModelMetadata{}, "", 0, err
					}
					chosenEffort = primaryOpt.Values[idx]
				}
			case "budget_tokens":
				for {
					promptMsg := fmt.Sprintf("Enter reasoning budget tokens (between %d and %d)", primaryOpt.Min, primaryOpt.Max)
					valStr, err := promptString(promptMsg, "", in, out)
					if err != nil {
						return "", store.ModelMetadata{}, "", 0, err
					}
					val, err := strconv.Atoi(valStr)
					if err == nil && val >= primaryOpt.Min && val <= primaryOpt.Max {
						chosenBudget = val
						break
					}
					fmt.Fprintf(out, "Please enter a valid integer between %d and %d.\n", primaryOpt.Min, primaryOpt.Max)
				}
			case "toggle":
				ok, err := promptConfirm("Enable reasoning thinking?", true, in, out)
				if err != nil {
					return "", store.ModelMetadata{}, "", 0, err
				}
				if ok {
					chosenEffort = "on"
				} else {
					chosenEffort = "off"
				}
			}
		}
	}

	return modelID, meta, chosenEffort, chosenBudget, nil
}
