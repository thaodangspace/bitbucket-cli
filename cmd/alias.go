package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/config"
	"github.com/thaodangspace/bitbucket-cli/output"
	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
)

func aliasPath() string {
	path := config.DefaultConfigPath(envMap())
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "bitbucket-cli-aliases.yaml")
}

func loadAliases() (map[string]string, error) {
	aliases := map[string]string{}
	path := aliasPath()
	if path == "" {
		return aliases, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return aliases, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read aliases: %w", err)
	}
	if len(data) != 0 {
		if err := yaml.Unmarshal(data, &aliases); err != nil {
			return nil, fmt.Errorf("parse aliases: %w", err)
		}
	}
	return aliases, nil
}

func saveAliases(aliases map[string]string) error {
	path := aliasPath()
	if path == "" {
		return fmt.Errorf("could not resolve aliases path")
	}
	data, err := yaml.Marshal(aliases)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func expandAliasArgs(args []string) ([]string, error) {
	aliases, err := loadAliases()
	if err != nil {
		return nil, err
	}
	for depth := 0; depth < 10; depth++ {
		index := firstCommandIndex(args)
		if index < 0 || index >= len(args) {
			return args, nil
		}
		name := args[index]
		if isBuiltinCommand(name) {
			return args, nil
		}
		value, ok := aliases[name]
		if !ok {
			return args, nil
		}
		parts, err := splitAliasCommand(value)
		if err != nil {
			return nil, fmt.Errorf("parse alias %q: %w", name, err)
		}
		args = append(append(append([]string{}, args[:index]...), parts...), args[index+1:]...)
	}
	return nil, fmt.Errorf("alias expansion exceeded 10 levels (possible cycle)")
}

func firstCommandIndex(args []string) int {
	valueFlags := map[string]bool{"--workspace": true, "--repo": true, "--repository": true, "--json": true, "--jq": true, "--template": true, "--format": true, "--color": true, "--pager": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return i + 1
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && valueFlags[arg] {
				i++
			}
			continue
		}
		return i
	}
	return -1
}

func isBuiltinCommand(name string) bool {
	for _, command := range rootCmd.Commands() {
		if command.Name() == name || command.HasAlias(name) {
			return true
		}
	}
	return false
}

func splitAliasCommand(value string) ([]string, error) {
	var parts []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, r := range value {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("alias command is empty")
	}
	return parts, nil
}

func init() {
	aliasCmd := &cobra.Command{Use: "alias", Short: "Manage command aliases"}
	setCmd := &cobra.Command{
		Use: "set <name> <command>", Short: "Set a command alias",
		Long: "Set a command alias. This is a write operation to local configuration; run it only when explicitly requested.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			value := strings.TrimSpace(args[1])
			if name == "" || value == "" {
				return fail(fmt.Errorf("alias name and command are required"))
			}
			aliases, err := loadAliases()
			if err != nil {
				return fail(err)
			}
			aliases[name] = value
			if err := saveAliases(aliases); err != nil {
				return fail(err)
			}
			return output.RenderJSON(os.Stdout, map[string]string{"name": name, "command": value})
		},
	}
	deleteCmd := &cobra.Command{
		Use: "delete <name>", Short: "Delete a command alias",
		Long: "Delete a command alias from local configuration; this is a write operation.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			aliases, err := loadAliases()
			if err != nil {
				return fail(err)
			}
			if _, ok := aliases[args[0]]; !ok {
				return fail(fmt.Errorf("alias %q not found", args[0]))
			}
			delete(aliases, args[0])
			if err := saveAliases(aliases); err != nil {
				return fail(err)
			}
			return output.RenderJSON(os.Stdout, map[string]any{"deleted": args[0]})
		},
	}
	listCmd := &cobra.Command{
		Use: "list", Short: "List command aliases", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			aliases, err := loadAliases()
			if err != nil {
				return fail(err)
			}
			keys := make([]string, 0, len(aliases))
			for key := range aliases {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if flagPretty {
				for _, key := range keys {
					fmt.Fprintf(os.Stdout, "%s %s\n", key, aliases[key])
				}
				return nil
			}
			return output.RenderJSON(os.Stdout, aliases)
		},
	}
	aliasCmd.AddCommand(setCmd, deleteCmd, listCmd)
	rootCmd.AddCommand(aliasCmd)
}
