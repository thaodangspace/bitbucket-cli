package main

import (
	"os"

	"github.com/thaodangspace/bitbucket-cli/cmd"
)

func main() {
	const path = "docs/src/content/docs/reference.md"
	if err := os.WriteFile(path, []byte(cmd.GenerateCommandMarkdown()), 0o644); err != nil {
		panic(err)
	}
}
