package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"

	"github.com/spf13/cobra"
)

// configPath resolves the config file location from the environment.
func configPath() string {
	return config.DefaultConfigPath(envMap())
}

// maskedFileConfig returns the stored config as a map with the API token
// redacted, for safe display.
func maskedFileConfig(fc config.FileConfig) map[string]any {
	m := map[string]any{}
	for _, key := range config.FileKeys {
		v, _ := fc.Get(key)
		if v == "" {
			continue
		}
		if key == "api_token" {
			v = "***redacted***"
		}
		m[key] = v
	}
	return m
}

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write the bitbucket-cli config file",
		Long: "Manage the YAML config file (default ~/.config/bitbucket-cli.yaml, " +
			"override with BITBUCKET_CONFIG). Valid keys: " + strings.Join(config.FileKeys, ", ") + ".",
	}

	configCmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(os.Stdout, configPath())
			return err
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value, writing it to the config file",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			path := configPath()
			if path == "" {
				return fail(fmt.Errorf("could not resolve a config file path"))
			}
			if err := config.SetFileValue(path, key, value); err != nil {
				return fail(err)
			}
			if flagPretty {
				_, err := fmt.Fprintf(os.Stdout, "Set %s in %s\n", key, path)
				return err
			}
			// Never echo the written value (it may be a token).
			return output.RenderJSON(os.Stdout, map[string]any{"set": key, "path": path})
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Print a stored config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			fc, err := config.LoadFileConfig(configPath())
			if err != nil {
				return fail(err)
			}
			value, err := fc.Get(args[0])
			if err != nil {
				return fail(err)
			}
			_, err = fmt.Fprintln(os.Stdout, value)
			return err
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show stored config values (API token redacted)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fc, err := config.LoadFileConfig(configPath())
			if err != nil {
				return fail(err)
			}
			masked := maskedFileConfig(fc)
			if flagPretty {
				if len(masked) == 0 {
					_, err := fmt.Fprintln(os.Stdout, "No config file values set.")
					return err
				}
				for _, key := range config.FileKeys {
					if v, ok := masked[key]; ok {
						if _, err := fmt.Fprintf(os.Stdout, "%s: %v\n", key, v); err != nil {
							return err
						}
					}
				}
				return nil
			}
			return output.RenderJSON(os.Stdout, masked)
		},
	})

	rootCmd.AddCommand(configCmd)
}
