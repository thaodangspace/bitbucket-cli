package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenerateCommandMarkdown returns the command reference generated from the
// Cobra tree. Keeping this in the command package prevents the docs from
// silently drifting when a command or flag is added.
func GenerateCommandMarkdown() string {
	var b strings.Builder
	b.WriteString("---\ntitle: Generated command reference\ndescription: Generated from the Cobra command tree.\n---\n\n")
	var walk func(*cobra.Command, string)
	walk = func(cmd *cobra.Command, prefix string) {
		children := append([]*cobra.Command(nil), cmd.Commands()...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if !child.IsAvailableCommand() {
				continue
			}
			use := strings.TrimSpace(child.Use)
			if use == "" {
				use = child.Name()
			}
			b.WriteString(fmt.Sprintf("## `%s%s`\n\n%s\n\n", prefix, use, strings.TrimSpace(child.Short)))
			flags := child.LocalNonPersistentFlags()
			var names []string
			flags.VisitAll(func(f *pflag.Flag) { names = append(names, fmt.Sprintf("`--%s` — %s", f.Name, f.Usage)) })
			sort.Strings(names)
			if len(names) > 0 {
				b.WriteString("Flags: " + strings.Join(names, "; ") + "\n\n")
			}
			walk(child, prefix+child.Name()+" ")
		}
	}
	walk(rootCmd, "")
	return strings.TrimRight(b.String(), "\n") + "\n"
}
