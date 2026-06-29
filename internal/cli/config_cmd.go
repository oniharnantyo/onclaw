package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v3"
)

func configCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Inspect onclaw configuration",
		Commands: []*cli.Command{
			{
				Name:  "show",
				Usage: "Print the resolved configuration (defaults < .env < env < flags)",
				Action: func(ctx context.Context, c *cli.Command) error {
					if err := st.ensure(c); err != nil {
						return err
					}

					// Marshal Config to preserve JSON formatting of fields
					b, err := json.Marshal(st.cfg)
					if err != nil {
						return err
					}
					var m map[string]interface{}
					if err := json.Unmarshal(b, &m); err != nil {
						return err
					}

					// Redact langfuse secret key if present
					if lf, ok := m["Langfuse"].(map[string]interface{}); ok {
						if val, exists := lf["SecretKey"]; exists && val != "" {
							lf["SecretKey"] = "***"
						}
					}
					if lf, ok := m["langfuse"].(map[string]interface{}); ok {
						if val, exists := lf["secret_key"]; exists && val != "" {
							lf["secret_key"] = "***"
						}
					}

					// Load provider profiles to append providers section
					mgr, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					profiles, err := mgr.ListProfiles(ctx)
					if err != nil {
						return err
					}

					providerList := []map[string]interface{}{}
					for _, p := range profiles {
						sec, err := mgr.GetSecret(ctx, p.Name)
						if err != nil {
							return err
						}
						apiKeyVal := ""
						if sec != "" {
							apiKeyVal = "***"
						}
						providerList = append(providerList, map[string]interface{}{
							"name":     p.Name,
							"kind":     p.ProviderType,
							"base_url": p.APIBase,
							"api_key":  apiKeyVal,
						})
					}
					m["providers"] = providerList

					res, err := json.MarshalIndent(m, "", "  ")
					if err != nil {
						return err
					}
					fmt.Println(string(res))
					return nil
				},
			},
			{
				Name:  "path",
				Usage: "Print the .env file in use and all searched paths",
				Action: func(ctx context.Context, c *cli.Command) error {
					if err := st.ensure(c); err != nil {
						return err
					}
					if st.cfg.LoadedFrom == "" {
						fmt.Println("No .env file found; using defaults + env.")
					} else {
						fmt.Println("Config file:", st.cfg.LoadedFrom)
					}
					fmt.Println("Searched paths:")
					for _, p := range st.cfg.SearchPaths {
						fmt.Println("  -", p)
					}
					return nil
				},
			},
		},
	}
}
