---
title: Configuration and security
description: Manage Bitbucket credentials, authentication, repository defaults, and write boundaries.
---

## Authentication

`bitbucket-cli auth` implements a `gh`-style workflow backed by the OS credential
store:

```sh
bitbucket-cli auth login                 # interactive (TTY only)
bitbucket-cli auth login --email you@example.com --with-token   # read token from stdin
bitbucket-cli auth status                # account, source, token type, repo access
bitbucket-cli auth logout                # removes the stored profile (use --yes when stdin is not a TTY)
bitbucket-cli auth token                 # opt-in token output for scripting
```

Login validates credentials with `GET /2.0/user` before persisting anything.
Tokens are stored in the OS credential store (macOS Keychain); the YAML config
file keeps only non-secret profile data (`email`, `token_type`, defaults).
`--with-token` is required when stdin is not a TTY; the CLI refuses to read an
interactive token from a pipe.

Supported `--token-type` values:

- `api` (default): HTTP Basic with the Atlassian account email and API token.
- `access`: Bitbucket repository/project/workspace access-token Basic format.
- `oauth`: `Authorization: Bearer <token>`.

`auth status` never prints the token and works without a repository default.
Legacy plaintext `api_token` entries in YAML are detected and migrated by
`auth login`; after migration the secret is removed from the file.

## Credential precedence

Configuration resolves in this order:

1. `BITBUCKET_EMAIL`, `BITBUCKET_API_TOKEN` (plus optional
   `BITBUCKET_TOKEN_TYPE`, `BITBUCKET_DEFAULT_WORKSPACE`, and
   `BITBUCKET_DEFAULT_REPO`)
2. The YAML config file (`~/.config/bitbucket-cli.yaml` or
   `$XDG_CONFIG_HOME/bitbucket-cli.yaml`): `email`, `token_type`, defaults; the
   token itself resolves from the OS credential store
3. Workspace/repository auto-detection from a local `bitbucket.org` git remote

Set `BITBUCKET_CONFIG` to override the config-file path. The `config` command can
write and inspect values without printing the API token:

```sh
bitbucket-cli config set email you@example.com
bitbucket-cli config set token_type api
bitbucket-cli config list
```

Restrict the config file to your user and never commit it:

```sh
chmod 600 ~/.config/bitbucket-cli.yaml
```

## API and authentication boundary

The CLI authenticates outbound requests through a pluggable auth provider and
sends credentials to `https://api.bitbucket.org/2.0`. Tokens never appear in
process arguments, logs, error excerpts, URLs, or pretty output; errors redact
the active token before they are rendered.

### Minimum scopes by command family

Bitbucket API tokens grant repository-level scopes. A conservative mapping:

- Read inspection (`pr list/get/comments/commits`, `branch list`,
  `repo get`, `pipeline list/get`): `repository:read`, `pullrequest:read`
- PR comments and writes (`pr comment`, `pr create`, `pr update`):
  `write:pullrequest:bitbucket`
- Downloads uploads (`pr attach`): `write:repository:bitbucket`
- Pipelines reads (`pipeline list/get/steps/log/test-report/watch`): `pipeline:read`
- Pipeline execution and administration (`pipeline run/stop`, schedules, caches,
  and configuration): `pipeline:write`
- Pipeline variables: the pipeline variable read/write scopes for the selected
  repository, workspace, or deployment environment
- Self-hosted runners: the pipeline runner read/write scopes for the selected
  repository or workspace

On a `403`, the CLI includes the endpoint's documented required scopes in the
error so the token can be re-created with the right grants.

## CI and automation

Environment variables remain the highest-precedence, non-interactive override
and require no keychain access:

```sh
BITBUCKET_EMAIL=you@example.com \
BITBUCKET_API_TOKEN=ci-token \
BITBUCKET_DEFAULT_WORKSPACE=team \
BITBUCKET_DEFAULT_REPO=repo \
bitbucket-cli pr list --state OPEN
```

`pr attach` stores uploaded files in repository Downloads and posts canonical
Download links in a PR comment. It does not use a native PR attachment API.

:::danger[Protect credentials and writes]
Keep tokens out of shell history, source control, and documentation. Treat
`pr comment`, `pr create`, `pr update`, and `pr attach` as remote mutations and
run them only after explicit authorization.
:::

The test suite injects an HTTP transport and a memory credential store and does
not require network access, a real keychain, or real credentials. Tests always
run against a temporary `BITBUCKET_CONFIG` so the developer's real config file
is never read.
