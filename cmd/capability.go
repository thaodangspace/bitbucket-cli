package cmd

import (
	"fmt"
	"strings"

	"github.com/thaodangspace/bitbucket-cli/auth"
	"github.com/thaodangspace/bitbucket-cli/config"
)

// Capability describes a Bitbucket operation's authentication and scope
// requirements. Keeping this registry separate from command code makes
// limitations visible in help and lets commands fail before making a request.
type Capability struct {
	Operation string   `json:"operation"`
	AuthKinds []string `json:"authKinds"`
	Scopes    []string `json:"scopes"`
	Available bool     `json:"available"`
	Note      string   `json:"note"`
}

var capabilityRegistry = map[string]Capability{
	"workspace.read":          {Operation: "workspace.read", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"read:workspace:bitbucket"}, Available: true, Note: "Workspace listing and membership reads are supported by the current REST API."},
	"workspace.invite":        {Operation: "workspace.invite", AuthKinds: []string{"oauth"}, Scopes: []string{"admin:workspace:bitbucket"}, Available: false, Note: "The current Bitbucket Cloud REST API does not expose a supported workspace invitation endpoint. Use the Bitbucket workspace administration UI; deprecated app passwords are not a workaround."},
	"workspace.remove-member": {Operation: "workspace.remove-member", AuthKinds: []string{"oauth"}, Scopes: []string{"admin:workspace:bitbucket"}, Available: false, Note: "The current Bitbucket Cloud REST API exposes membership reads but no supported member-removal operation. Use the Bitbucket workspace administration UI; deprecated app passwords are not a workaround."},
	"project.read":            {Operation: "project.read", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"read:project:bitbucket"}, Available: true, Note: "Project reads are supported by the current REST API."},
	"project.write":           {Operation: "project.write", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"admin:project:bitbucket"}, Available: true, Note: "Project CRUD is supported by the current REST API for credentials with project administration access."},
	"permission.read":         {Operation: "permission.read", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"read:repository:bitbucket"}, Available: true, Note: "Repository permission reads are supported by the current REST API."},
	"permission.grant":        {Operation: "permission.grant", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"admin:repository:bitbucket", "write:permission:bitbucket"}, Available: true, Note: "Explicit repository permission updates require repository administration and the permission-write scope where applicable."},
	"permission.revoke":       {Operation: "permission.revoke", AuthKinds: []string{"api", "access", "oauth"}, Scopes: []string{"admin:repository:bitbucket", "delete:permission:bitbucket"}, Available: true, Note: "Explicit repository permission deletion requires repository administration and the permission-delete scope where applicable."},
}

type capabilityError struct {
	cap      Capability
	authKind string
}

func (e *capabilityError) Error() string {
	if !e.cap.Available {
		return fmt.Sprintf("capability %q is unavailable: %s", e.cap.Operation, e.cap.Note)
	}
	return fmt.Sprintf("capability %q is unavailable for %s credentials: supported credential types: %s; required scopes: %s. %s", e.cap.Operation, e.authKind, strings.Join(e.cap.AuthKinds, ", "), strings.Join(e.cap.Scopes, ", "), e.cap.Note)
}

func (e *capabilityError) Details() map[string]any {
	return map[string]any{
		"capability": e.cap,
		"authKind":   e.authKind,
	}
}

func requireCapability(cfg config.Config, operation string) error {
	cap, ok := capabilityRegistry[operation]
	if !ok {
		return fmt.Errorf("capability %q is not registered", operation)
	}
	kind := auth.ProviderKind(cfg.Auth)
	if !cap.Available || !contains(cap.AuthKinds, kind) {
		return &capabilityError{cap: cap, authKind: kind}
	}
	return nil
}

func capabilityHelp(operation string) string {
	cap, ok := capabilityRegistry[operation]
	if !ok {
		return "Capability requirements are documented by the Bitbucket REST API."
	}
	if !cap.Available {
		return "Unavailable with the current Bitbucket Cloud REST API. " + cap.Note
	}
	return fmt.Sprintf("Supported credential types: %s. Required scopes: %s. %s", strings.Join(cap.AuthKinds, ", "), strings.Join(cap.Scopes, ", "), cap.Note)
}
