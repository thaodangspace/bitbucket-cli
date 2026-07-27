---
title: Configuration and security
description: Manage Bitbucket credentials, repository defaults, and write boundaries.
---

## Credential precedence

Configuration resolves in this order:

1. `BITBUCKET_EMAIL`, `BITBUCKET_API_TOKEN`, `BITBUCKET_DEFAULT_WORKSPACE`, and `BITBUCKET_DEFAULT_REPO`
2. `~/.config/bitbucket-cli.yaml` (or `$XDG_CONFIG_HOME/bitbucket-cli.yaml`)
3. Workspace/repository auto-detection from a local `bitbucket.org` git remote

Set `BITBUCKET_CONFIG` to override the config-file path. The `config` command can
write and inspect values without printing the API token:

```sh
bitbucket-cli config set email you@example.com
bitbucket-cli config set api_token your-atlassian-api-token
bitbucket-cli config list
```

Restrict the config file to your user and never commit it:

```sh
chmod 600 ~/.config/bitbucket-cli.yaml
```

## API and authentication boundary

The CLI sends Basic authentication using the configured email and Atlassian API
token to `https://api.bitbucket.org/2.0`. The token is not logged or echoed.
Recommended scopes depend on the operation: repository and pull-request read
scopes for inspection, `write:pullrequest:bitbucket` for comments/PR writes, and
`write:repository:bitbucket` for `pr attach`.

`pr attach` stores uploaded files in repository Downloads and posts canonical
Download links in a PR comment. It does not use a native PR attachment API.

:::danger[Protect credentials and writes]
Keep tokens out of shell history, source control, and documentation. Treat
`pr comment`, `pr create`, `pr update`, and `pr attach` as remote mutations and
run them only after explicit authorization.
:::

The test suite injects an HTTP transport and does not require network access or
real credentials. The known `TestStatusMissingCredsFails` configuration-file
gotcha should be isolated with a temporary `BITBUCKET_CONFIG` when running tests
locally.
