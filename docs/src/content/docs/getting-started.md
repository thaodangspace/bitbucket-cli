---
title: Getting started
description: Install bitbucket-cli, configure credentials, and inspect a repository.
---

## Requirements

- Go, if installing or building from source
- An Atlassian API token and the email address associated with it
- Bitbucket Cloud access to the target workspace and repository

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

For a shell session, set the credentials and optional defaults:

```sh
export BITBUCKET_EMAIL="you@example.com"
export BITBUCKET_API_TOKEN="your-atlassian-api-token"
export BITBUCKET_DEFAULT_WORKSPACE="workspace-slug"
export BITBUCKET_DEFAULT_REPO="repository-slug"
```

Or use `~/.config/bitbucket-cli.yaml`:

```yaml
email: you@example.com
api_token: your-atlassian-api-token
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
