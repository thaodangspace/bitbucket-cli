package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/bitbucket-cli/bitbucket"
	"github.com/thaodangspace/bitbucket-cli/output"
)

var modelKinds = []string{"feature", "bugfix", "release", "hotfix"}

func init() {
	modelCmd := &cobra.Command{Use: "branching-model", Short: "Branching model commands"}
	viewCmd := &cobra.Command{Use: "view", Short: "View the repository branching model", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), base+"/branching-model", bitbucket.RequestOptions{}, &raw); err != nil {
			return fail(err)
		}
		return emitObjectFields(raw, output.BranchingModelFields, output.BranchingModelSummary)
	}}

	var production, development, feature, bugfix, release, hotfix string
	var enableProduction, disableProduction, developmentUseMain bool
	var enableKinds, disableKinds []string
	editCmd := &cobra.Command{Use: "edit", Short: "Edit the repository branching model", Long: "Edit the branching model. This is a write operation; run it only when explicitly requested.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if enableProduction && disableProduction {
			return fail(fmt.Errorf("--enable-production and --disable-production are mutually exclusive"))
		}
		enableKinds = cleanModelKinds(enableKinds)
		disableKinds = cleanModelKinds(disableKinds)
		if err := validateModelKinds(enableKinds); err != nil {
			return fail(err)
		}
		if err := validateModelKinds(disableKinds); err != nil {
			return fail(err)
		}
		if overlapStrings(enableKinds, disableKinds) {
			return fail(fmt.Errorf("a branch type cannot be both enabled and disabled"))
		}
		cfg, client, err := newClient()
		if err != nil {
			return fail(err)
		}
		_, base, err := resolveRepo(cfg)
		if err != nil {
			return fail(err)
		}
		var current map[string]any
		if err := client.Request(ctx(cmd), base+"/branching-model/settings", bitbucket.RequestOptions{}, &current); err != nil {
			return fail(err)
		}
		body := map[string]any{}
		if cmd.Flags().Changed("production-branch") || enableProduction || disableProduction {
			branch := mapFrom(current["production"])
			if production != "" {
				branch["name"] = production
				branch["use_mainbranch"] = false
			}
			if enableProduction {
				branch["enabled"] = true
			}
			if disableProduction {
				branch["enabled"] = false
			}
			body["production"] = branch
		}
		if cmd.Flags().Changed("development-branch") || developmentUseMain {
			branch := mapFrom(current["development"])
			if development != "" {
				branch["name"] = development
				branch["use_mainbranch"] = false
			}
			if developmentUseMain {
				branch["use_mainbranch"] = true
				branch["name"] = "null"
			}
			body["development"] = branch
		}
		if cmd.Flags().Changed("feature-prefix") || cmd.Flags().Changed("bugfix-prefix") || cmd.Flags().Changed("release-prefix") || cmd.Flags().Changed("hotfix-prefix") || cmd.Flags().Changed("enable") || cmd.Flags().Changed("disable") {
			body["branch_types"] = cloneModelList(current["branch_types"])
		}
		if cmd.Flags().Changed("feature-prefix") {
			setModelPrefix(body, "feature", feature)
		}
		if cmd.Flags().Changed("bugfix-prefix") {
			setModelPrefix(body, "bugfix", bugfix)
		}
		if cmd.Flags().Changed("release-prefix") {
			setModelPrefix(body, "release", release)
		}
		if cmd.Flags().Changed("hotfix-prefix") {
			setModelPrefix(body, "hotfix", hotfix)
		}
		for _, kind := range enableKinds {
			setModelEnabled(body, kind, true)
		}
		for _, kind := range disableKinds {
			setModelEnabled(body, kind, false)
		}
		if !cmd.Flags().Changed("production-branch") && !cmd.Flags().Changed("development-branch") && !cmd.Flags().Changed("enable-production") && !cmd.Flags().Changed("disable-production") && !cmd.Flags().Changed("development-use-main") && !cmd.Flags().Changed("feature-prefix") && !cmd.Flags().Changed("bugfix-prefix") && !cmd.Flags().Changed("release-prefix") && !cmd.Flags().Changed("hotfix-prefix") && !cmd.Flags().Changed("enable") && !cmd.Flags().Changed("disable") {
			return fail(fmt.Errorf("at least one branching-model field must be supplied"))
		}
		if err := validateModelPrefixes(body); err != nil {
			return fail(err)
		}
		var raw json.RawMessage
		if err := client.Request(ctx(cmd), base+"/branching-model/settings", bitbucket.RequestOptions{Method: http.MethodPut, Body: body}, &raw); err != nil {
			return fail(err)
		}
		// The settings endpoint returns raw configuration. Fetch the active
		// model so the result reflects actual branch targets and prefixes.
		var effective json.RawMessage
		if err := client.Request(ctx(cmd), base+"/branching-model", bitbucket.RequestOptions{}, &effective); err != nil {
			return fail(err)
		}
		return emitObjectFields(effective, output.BranchingModelFields, output.BranchingModelSummary)
	}}
	editCmd.Flags().StringVar(&production, "production-branch", "", "Production branch name")
	editCmd.Flags().StringVar(&development, "development-branch", "", "Development branch name")
	editCmd.Flags().BoolVar(&enableProduction, "enable-production", false, "Enable the production branch")
	editCmd.Flags().BoolVar(&disableProduction, "disable-production", false, "Disable the production branch")
	editCmd.Flags().BoolVar(&developmentUseMain, "development-use-main", false, "Make development track the main branch")
	editCmd.Flags().StringVar(&feature, "feature-prefix", "", "Feature branch prefix")
	editCmd.Flags().StringVar(&bugfix, "bugfix-prefix", "", "Bugfix branch prefix")
	editCmd.Flags().StringVar(&release, "release-prefix", "", "Release branch prefix")
	editCmd.Flags().StringVar(&hotfix, "hotfix-prefix", "", "Hotfix branch prefix")
	editCmd.Flags().StringSliceVar(&enableKinds, "enable", nil, "Enable a branch type (repeatable: feature, bugfix, release, hotfix)")
	editCmd.Flags().StringSliceVar(&disableKinds, "disable", nil, "Disable a branch type (repeatable: feature, bugfix, release, hotfix)")
	modelCmd.AddCommand(viewCmd, editCmd)
	rootCmd.AddCommand(modelCmd)
}

func cleanModelKinds(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" && value != "[]" {
			out = append(out, value)
		}
	}
	return out
}
func validateModelKinds(values []string) error {
	for _, value := range values {
		if !contains(modelKinds, strings.ToLower(value)) {
			return fmt.Errorf("invalid branch type %q (use feature, bugfix, release, or hotfix)", value)
		}
	}
	return nil
}
func overlapStrings(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}
func cloneMap(value any) map[string]any {
	out := map[string]any{}
	if m, ok := value.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
func mapFrom(value any) map[string]any { return cloneMap(value) }
func cloneModelList(value any) []any {
	list, _ := value.([]any)
	out := make([]any, len(list))
	for i, item := range list {
		out[i] = cloneMap(item)
	}
	return out
}
func setModelPrefix(body map[string]any, kind, prefix string) {
	if strings.TrimSpace(prefix) == "" {
		return
	}
	list, _ := body["branch_types"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && strings.EqualFold(fmt.Sprint(m["kind"]), kind) {
			m["prefix"] = prefix
			return
		}
	}
	body["branch_types"] = append(list, map[string]any{"kind": kind, "enabled": true, "prefix": prefix})
}
func setModelEnabled(body map[string]any, kind string, enabled bool) {
	list, _ := body["branch_types"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && strings.EqualFold(fmt.Sprint(m["kind"]), kind) {
			m["enabled"] = enabled
			return
		}
	}
	body["branch_types"] = append(list, map[string]any{"kind": kind, "enabled": enabled, "prefix": kind + "/"})
}
func validateModelPrefixes(body map[string]any) error {
	seen := map[string]string{}
	list, _ := body["branch_types"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prefix := strings.TrimSpace(fmt.Sprint(m["prefix"]))
		if prefix == "" {
			return fmt.Errorf("branch type %q must have a non-empty prefix", m["kind"])
		}
		for previousPrefix, previous := range seen {
			if previous != fmt.Sprint(m["kind"]) && (prefix == previousPrefix || strings.HasPrefix(prefix, previousPrefix) || strings.HasPrefix(previousPrefix, prefix)) {
				return fmt.Errorf("branch type prefixes %q and %q overlap (%s and %s)", prefix, previousPrefix, previous, m["kind"])
			}
		}
		seen[prefix] = fmt.Sprint(m["kind"])
	}
	return nil
}
