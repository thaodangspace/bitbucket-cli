# bitbucket-cli

A standalone Go CLI for Bitbucket Cloud pull requests, branches, and repository
info. It is the command-line port of the Pi `bitbucket` agent extension, built
so that any agent or script can drive Bitbucket without the Pi runtime.

Output is **JSON by default** (easy for agents to parse); pass `--pretty` for
human-readable text. Errors are written as a JSON envelope to stderr with a
non-zero exit code.

## Install

### go install

```bash
go install github.com/thaodangspace/bitbucket-cli@latest
```

Installs the latest tagged release into `$GOBIN`. Requires Go 1.24+. Pin a
specific version with `@v0.1.0`.

### Prebuilt binary

Download a tarball for your OS/arch from the
[Releases page](https://github.com/thaodangspace/bitbucket-cli/releases), extract it,
and put `bitbucket-cli` on your `PATH`. No Go toolchain required.

### Build from source

```bash
cd ~/code/bitbucket-cli
go build -o bitbucket-cli .      # local binary
# or
go install .                     # into $GOBIN
```

The only third-party dependencies are `spf13/cobra` and `gopkg.in/yaml.v3`.

## Configuration

Credentials and defaults are resolved with the following precedence:
**environment variables → config file (+ OS credential store) → git remote
auto-detection**.

### Auth command (recommended)

The `gh`-style `auth` workflow validates your credential against Bitbucket and
stores the token in the OS credential store (macOS Keychain) instead of
plaintext:

```bash
bitbucket-cli auth login                 # interactive (TTY)
echo "$TOKEN" | bitbucket-cli auth login --email you@example.com --with-token   # scripting/CI
bitbucket-cli auth status                # account, source, token type, repo access
bitbucket-cli auth logout                # remove the stored profile (--yes when stdin is not a TTY)
bitbucket-cli auth token                 # opt-in: print the token for scripts
```

`--token-type api|access|oauth` selects API tokens (default), Bitbucket access
tokens, or OAuth bearer tokens. API tokens use HTTP Basic with the Atlassian
account email; access and OAuth tokens use `Authorization: Bearer`. Access
tokens are resource-scoped, so login validates the selected/default repository
and does not require an email. Tokens are never echoed by these commands; a
legacy plaintext `api_token` in YAML is migrated by API-token login and removed.

### Environment variables

```bash
export BITBUCKET_EMAIL="you@example.com"
export BITBUCKET_API_TOKEN="your-atlassian-api-token"
export BITBUCKET_DEFAULT_WORKSPACE="workspace-slug"   # optional
export BITBUCKET_DEFAULT_REPO="repository-slug"        # optional
export BITBUCKET_TOKEN_TYPE="api"                       # optional: api|access|oauth
export BITBUCKET_CLONE_PROTOCOL="https"                 # optional: https|ssh
```

### Config file

Any value not set in the environment is read from a YAML config file at
`~/.config/bitbucket-cli.yaml` (or `$XDG_CONFIG_HOME/bitbucket-cli.yaml`).
Override the path with `BITBUCKET_CONFIG`. It holds non-secret profile data:

```yaml
# ~/.config/bitbucket-cli.yaml
email: you@example.com
token_type: api
default_workspace: workspace-slug   # optional
default_repo: repository-slug       # optional
clone_protocol: https                # optional: https|ssh
```

All keys are optional; environment variables take precedence over file values.
The token itself resolves from the OS credential store. Bearer profiles also
persist a non-secret `credential_key` to namespace their store entry per config
file; API-token entries remain keyed by email for compatibility. Manage the file
with the `config` command instead of editing it by hand:

```bash
bitbucket-cli config set email you@example.com
bitbucket-cli config set token_type api
bitbucket-cli config set default_workspace workspace-slug
bitbucket-cli config get default_workspace
bitbucket-cli config list          # API token redacted
bitbucket-cli config path          # print the resolved file path
```

API tokens must be Atlassian API tokens with access to the target workspace.
Bitbucket access tokens are resource-scoped and use Bearer authentication; they
must be validated against a repository context with repository-read access.
OAuth tokens also use Bearer. Permission names depend on the credential type:
API tokens use names such as `read:repository:bitbucket` and
`write:pullrequest:bitbucket`; access tokens use names such as
`repository:read` and `pullrequest:write`; OAuth uses names such as
`repository` and `pullrequest:write`. On a `403`, the CLI selects the matching
credential-specific permission family. The result is documented remediation
guidance, not live scope introspection, and resource/event configuration may
require additional permissions.

If `BITBUCKET_DEFAULT_WORKSPACE`/`BITBUCKET_DEFAULT_REPO` are unset and you run
inside a git repository whose `origin` points at `bitbucket.org`, the workspace
and repo are auto-detected. A local `bitbucket-cli.repository` git config set by
`repo set-default` takes precedence over the remote. Override per command with
`--workspace`/`--repo`.

## Commands

| Command | Description |
| --- | --- |
| `auth login [--email <e>] [--with-token] [--token-type api\|access\|oauth]` | Validate and store credentials in the OS credential store |
| `auth status [--json]` | Report account, credential source, token type, repo access |
| `auth logout [--yes]` | Remove the stored profile |
| `auth token` | Print the active token (opt-in, for scripting) |
| `status` | Report config validity and default repo |
| `config set <key> <value>` | Write a value to the config file |
| `config get <key>` | Print a stored config value |
| `config list` | Show stored config (API token redacted) |
| `config path` | Print the config file path |
| `repo list [<workspace>]` | Discover repositories with filters |
| `repo view [<workspace/repo>] [--readme] [--web]` | Repository details and README |
| `repo create <name>` | Create a repository (write) |
| `repo edit [<workspace/repo>]` | Update selected repository fields (write) |
| `repo delete [<workspace/repo>] --yes` | Delete a repository (write) |
| `repo fork [<workspace/repo>]` | Fork a repository (write) |
| `repo clone <workspace/repo|url>` | Clone a repository |
| `repo browse [<workspace/repo>] [<path>]` | Open a repository path in a browser |
| `repo set-default [<workspace/repo>]` | Set a local repository default |
| `workspace list|view|members` | List workspaces, details, and members |
| `workspace member view <user-selector>` | View a workspace membership |
| `workspace invite/remove-member` | Discoverable capability checks; unsupported by the current REST API |
| `project list|view|create|edit|delete` | Manage Bitbucket projects (writes require care) |
| `permission repos|users|groups` | Inspect effective and explicit repository permissions |
| `permission grant|revoke` | Change one explicit repository permission (write) |
| `webhook events|list|view|create|edit|delete` | Manage repository/workspace webhooks and validate event keys |
| `webhook export|apply` | Export or safely reconcile declarative webhook configuration |
| `ssh-key list|view|add|edit|delete` | Manage account SSH keys (public keys only) |
| `deploy-key list|add|delete` | Manage repository deploy keys (read-only Git access) |
| `repo get [--web]` | Compatibility alias for `repo view` |
| `pr list [--state OPEN\|MERGED\|DECLINED\|SUPERSEDED] [--limit N]` | List pull requests |
| `pr get [<id-or-url>] [--web]` | One pull request |
| `pr comments <id> [--limit N]` | Pull request comments |
| `pr commits <id> [--limit N]` | Pull request commits |
| `pr comment <id> --body <markdown> [--reply-to <comment-id>]` | Post a markdown comment or reply (write) |
| `pr attach <id> --file <path> [--message <markdown>]` | Upload files to Downloads and link them from a PR comment (write) |
| `pr create --source <branch> --title <t> [...]` | Create a pull request (write) |
| `pr update <id> [--title <t>] [--description ...]` | Update a PR's title/description (write) |
| `branch list [--query <q>] [--limit N]` | List branches |
| `pipeline list [--state <state>] [--limit N]` | List pipeline runs |
| `pipeline get <uuid|build-number|url> [--web]` | Pipeline details and steps |
| `pipeline run --branch <name>\|--tag <name>\|--commit <hash>` | Start a pipeline (write) |
| `pipeline stop <pipeline> --yes` | Stop a pipeline (write) |
| `pipeline watch <pipeline> [--interval D] [--exit-status]` | Watch a pipeline |
| `pipeline steps <pipeline> [--limit N]` | List pipeline steps |
| `pipeline log <pipeline> [<step>] [--follow]` | Read raw step logs |
| `pipeline test-report <pipeline> [<step>] [--cases]` | Read test reports |
| `pipeline schedule list\|create\|edit\|delete\|runs` | Manage schedules (writes require care) |
| `pipeline variable list\|set\|delete` | Manage repository/workspace/deployment variables |
| `pipeline cache list\|delete` | Manage pipeline caches |
| `pipeline runner list\|view\|create\|edit\|delete` | Manage self-hosted runners |
| `pipeline config view\|enable\|disable` | Manage Pipelines configuration |
| `browse [<path>]` | Open the repository in a browser |
| `completion bash\|zsh\|fish\|powershell` | Generate shell completion |
| `alias set\|delete\|list` | Manage local command aliases |

Workspace/project selectors accept slugs, UUIDs, and Bitbucket URLs. User selectors accept account UUIDs, account IDs, nicknames, or uniquely resolved display names. Permission mutations return auditable before/after results and treat no-op changes as unchanged. The current REST API has no supported workspace invitation or member-removal endpoint; those commands return a targeted capability error rather than suggesting deprecated app passwords.

Webhook and key writes are intentionally explicit. Webhooks require HTTPS (private destinations need `--allow-private`; `http://localhost` additionally needs `--allow-insecure-localhost`), and secrets are read with `--secret-stdin`, `--secret-prompt`, or `--secret-env NAME`, never as an argument. SSH and deploy-key commands accept only OpenSSH public keys. Deploy keys are repository-scoped and read-only for Git access; they are not account SSH keys. Webhook API-token scopes are `read:webhook:bitbucket` plus `write:webhook:bitbucket` for create/update, and `delete:webhook:bitbucket` for deletion; subscribed event types may add requirements. Account SSH-key reads use `read:ssh-key:bitbucket`; add/edit uses both `read:ssh-key:bitbucket` and `write:ssh-key:bitbucket`; delete uses `delete:ssh-key:bitbucket`. When the authenticated user is resolved by default, `read:user:bitbucket` is also required. Deploy-key reads require `admin:repository:bitbucket`; writes additionally require the documented SSH-key write/delete scope as applicable. The webhook event catalog is public. SSH-key edit changes labels only; expiry is set at creation because Bitbucket does not document expiry updates via PUT.

Global flags: `--workspace`, `--repo`, `-R/--repository`, `--pretty`, `--json`,
`--jq`, `--template`, `--format json|table|yaml|raw`, `--color`, and `--pager`.
`--jq` uses the maintained gojq implementation. Default `--limit` is 20.

## Examples

```bash
# JSON (default) — for agents
bitbucket-cli pr list --state OPEN
bitbucket-cli pr get 123
bitbucket-cli branch list --query 'name ~ "feature/"'

# Human-readable
bitbucket-cli pr list --pretty
# -> #123 Fix login bug [OPEN]

# Post a comment or reply (only run when explicitly asked to post)
bitbucket-cli pr comment 123 --body "Thanks, I will take a look."
bitbucket-cli pr comment 123 --body "Fixed now." --reply-to 456

# Create a pull request (write — only run when explicitly asked)
bitbucket-cli pr create --source feature/login --title "Add login" \
  --description "Implements the login flow."
# destination defaults to the repo main branch; override with --destination
# long descriptions: pipe markdown via stdin
generate-summary | bitbucket-cli pr create --source feature/login \
  --title "Add login" --description-file -

# Attach files to a PR (write — uploads to repository Downloads, then comments links)
bitbucket-cli pr attach 123 --file test-report.html --message "Attached test report"
bitbucket-cli pr attach 123 --file screenshot.png --file debug.log
# Bitbucket Cloud has no native PR attachment API; existing Downloads artifacts
# with the same filename are replaced by Bitbucket.

# Update a PR's description (write); reviewers are preserved
bitbucket-cli pr update 123 --description-file release-notes.md
bitbucket-cli pr update 123 --title "Add login (v2)"

# Override the target repo
bitbucket-cli --workspace acme --repo web pr list
```

## Output contract

- **Default**: full JSON on stdout. List commands emit an array; single-entity
  commands emit an object.
- **`--json id,title,...`**: select documented stable fields. `--jq` and
  `--template` transform the projection (or the full response without it).
- **`--format`** supports `json`, `table`, `yaml`, and `raw`; `--pretty` is an
  alias for the deterministic table view.
- **Errors**: structured JSON on stderr and never transformed by `--jq` or
  templates. Config/usage errors carry only `message`.

## Security

Credentials resolve from environment variables, the config file, or the OS
credential store. Tokens are never placed in command arguments, logs, error
excerpts, or pretty output; error responses redact the active token. `auth
logout` and `auth status` never mutate environment variables, and tests run
against a temporary config and an in-memory credential store. Do not commit
tokens, `.env` files, or a config file containing a real API token (`chmod 600`
it and keep it out of version control).

## Documentation

The documentation site is an Astro/Starlight app under `docs/`. Run it locally
with `make docs-dev`, or build the static site with `make docs-build`. Cloudflare
Pages settings are documented in [`docs/README.md`](docs/README.md).

## Testing

```bash
go test ./...
```
