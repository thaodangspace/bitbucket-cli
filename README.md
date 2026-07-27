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
**environment variables → config file → git remote auto-detection**.

### Environment variables

```bash
export BITBUCKET_EMAIL="you@example.com"
export BITBUCKET_API_TOKEN="your-atlassian-api-token"
export BITBUCKET_DEFAULT_WORKSPACE="workspace-slug"   # optional
export BITBUCKET_DEFAULT_REPO="repository-slug"        # optional
```

### Config file

Any value not set in the environment is read from a YAML config file at
`~/.config/bitbucket-cli.yaml` (or `$XDG_CONFIG_HOME/bitbucket-cli.yaml`).
Override the path with `BITBUCKET_CONFIG`.

```yaml
# ~/.config/bitbucket-cli.yaml
email: you@example.com
api_token: your-atlassian-api-token
default_workspace: workspace-slug   # optional
default_repo: repository-slug       # optional
```

All keys are optional; environment variables take precedence over file values.
Manage the file with the `config` command instead of editing it by hand:

```bash
bitbucket-cli config set email you@example.com
bitbucket-cli config set api_token your-atlassian-api-token
bitbucket-cli config set default_workspace workspace-slug
bitbucket-cli config get default_workspace
bitbucket-cli config list          # API token redacted
bitbucket-cli config path          # print the resolved file path
```

The token must be an Atlassian API token with access to the target workspace.
Recommended scopes: `read:repository:bitbucket`, `read:pullrequest:bitbucket`,
and `write:pullrequest:bitbucket` to create/update PRs and post comments. Add
`write:repository:bitbucket` when using `pr attach`, which uploads files to
repository Downloads before commenting links on the PR.

If `BITBUCKET_DEFAULT_WORKSPACE`/`BITBUCKET_DEFAULT_REPO` are unset and you run
inside a git repository whose `origin` points at `bitbucket.org`, the workspace
and repo are auto-detected. Override per command with `--workspace`/`--repo`.

## Commands

| Command | Description |
| --- | --- |
| `status` | Report config validity and default repo |
| `config set <key> <value>` | Write a value to the config file |
| `config get <key>` | Print a stored config value |
| `config list` | Show stored config (API token redacted) |
| `config path` | Print the config file path |
| `repo get` | Repository details |
| `pr list [--state OPEN\|MERGED\|DECLINED\|SUPERSEDED] [--limit N]` | List pull requests |
| `pr get <id>` | One pull request |
| `pr comments <id> [--limit N]` | Pull request comments |
| `pr commits <id> [--limit N]` | Pull request commits |
| `pr comment <id> --body <markdown> [--reply-to <comment-id>]` | Post a markdown comment or reply (write) |
| `pr attach <id> --file <path> [--message <markdown>]` | Upload files to Downloads and link them from a PR comment (write) |
| `pr create --source <branch> --title <t> [...]` | Create a pull request (write) |
| `pr update <id> [--title <t>] [--description ...]` | Update a PR's title/description (write) |
| `branch list [--query <q>] [--limit N]` | List branches |

Global flags: `--workspace`, `--repo`, `--pretty`. Default `--limit` is 20.

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

- **Default**: JSON on stdout. List commands emit a JSON array of the full
  Bitbucket objects; single-entity commands emit the entity object.
- **`--pretty`**: one-line text summaries (matching the original extension).
- **Errors**: JSON `{"error":{"message":...,"status":...,"method":...,"url":...,"excerpt":...}}`
  on stderr, exit code 1. Config/usage errors carry only `message`.

## Security

Credentials are read from environment variables or the config file and are
never logged or echoed. Do not commit tokens, `.env` files, or a config file
containing a real API token (`chmod 600` it and keep it out of version control).

## Documentation

The documentation site is an Astro/Starlight app under `docs/`. Run it locally
with `make docs-dev`, or build the static site with `make docs-build`. Cloudflare
Pages settings are documented in [`docs/README.md`](docs/README.md).

## Testing

```bash
go test ./...
```
