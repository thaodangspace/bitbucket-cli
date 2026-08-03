---
title: Getting started
description: Install bitbucket-cli, configure credentials, and inspect a repository.
---

## Requirements

- Go, if installing or building from source
- An Atlassian API token and the email address associated with it
- Bitbucket Cloud access to the target workspace and repository

Create an API token under [Bitbucket account settings](https://support.atlassian.com/bitbucket-cloud/docs/api-tokens/).
The CLI validates credentials against `GET /2.0/user` before saving them.

## Install

Install the latest tagged release with Go:

```sh
go install github.com/thaodangspace/bitbucket-cli@latest
```

You can also download a release tarball or build from source:

```sh
go build -o bitbucket-cli .
```

Verify the binary:

```sh
bitbucket-cli --help
bitbucket-cli --version
```

## Configure credentials

The recommended workflow is `auth login`, which prompts for your Atlassian
account email and token, validates them against Bitbucket, and stores the token
in the OS credential store (macOS Keychain) instead of plaintext:

```sh
bitbucket-cli auth login
```

For scripting and CI, read the token from stdin without interactive prompts
(matching `gh auth login --with-token`):

```sh
echo "your-atlassian-api-token" | bitbucket-cli auth login --email you@example.com --with-token
```

`auth login` also supports `--token-type api|access|oauth` for Bitbucket access
tokens and OAuth bearer tokens. Verify state with `bitbucket-cli auth status` and
sign out with `bitbucket-cli auth logout`. The token is never echoed by these
commands; `bitbucket-cli auth token` prints it explicitly for scripts that need
to build their own headers.

### Environment variables (non-interactive override)

Environment variables always take precedence over stored credentials and are the
safest choice for automation:

```sh
export BITBUCKET_EMAIL="you@example.com"
export BITBUCKET_API_TOKEN="your-atlassian-api-token"
export BITBUCKET_DEFAULT_WORKSPACE="workspace-slug"
export BITBUCKET_DEFAULT_REPO="repository-slug"
```

### Config file

`~/.config/bitbucket-cli.yaml` holds non-secret profile data (email, token type,
defaults). A legacy plaintext `api_token` is still accepted but `auth login`
migrates it to the credential store and removes it from the file:

```yaml
email: you@example.com
default_workspace: workspace-slug
default_repo: repository-slug
```

Environment variables take precedence over the config file. When workspace and
repository defaults are absent, the CLI can auto-detect them from a local git
`origin` pointing at `bitbucket.org`.

## Make a read-only request

Check configuration without changing Bitbucket:

```sh
bitbucket-cli status
```

Then inspect pull requests using JSON output:

```sh
bitbucket-cli pr list --state OPEN
```

Use `--workspace` and `--repo` to override defaults for one command. See
[Commands](/commands/) and [Configuration and security](/configuration-security/)
for more detail.
