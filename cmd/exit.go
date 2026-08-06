package cmd

import (
	"errors"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/bitbucket"
)

// exitCode centralizes the documented process status categories.
func exitCode(err error) int {
	var httpErr *bitbucket.HTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.Status == 401 || httpErr.Status == 403:
			return 3
		case httpErr.Status == 404:
			return 4
		default:
			return 1
		}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "authentication") || strings.Contains(message, "credential") || strings.Contains(message, "token") {
		return 3
	}
	for _, word := range []string{"invalid ", "required", "mutually exclusive", "unknown flag", "usage:"} {
		if strings.Contains(message, word) {
			return 2
		}
	}
	return 1
}
