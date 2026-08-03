---
title: Commands
description: Inspect Bitbucket Cloud resources and perform explicit pull request writes.
---

## Global options

- `--workspace SLUG` overrides the workspace default.
- `--repo SLUG` overrides the repository default.
- `--pretty` prints one-line summaries instead of JSON.
- List commands default to a limit of 20; pagination is bounded internally.

## Authentication

```sh
bitbucket-cli auth login [--email EMAIL] [--with-token] [--token-type api|access|oauth]
bitbucket-cli auth status [--json]
bitbucket-cli auth logout [--yes]
bitbucket-cli auth token
```

`auth login` validates credentials against Bitbucket before storing the token in
the OS credential store. `--with-token` reads the token from stdin (required when
stdin is not a TTY). `auth status` reports the account, credential source, token
type, and whether the token can read the resolved repository — it never prints
the token. `auth logout` removes only the stored profile and never mutates
environment variables.

## Configuration and repository commands

```sh
bitbucket-cli status
bitbucket-cli config path
bitbucket-cli config get <key>
bitbucket-cli config set <key> <value>
bitbucket-cli config list
bitbucket-cli repo get
```

`config list` redacts the API token. `repo get` resolves the target using
explicit flags, configured defaults, or the local Bitbucket git remote.

## Pull requests

```sh
bitbucket-cli pr list [--state OPEN|MERGED|DECLINED|SUPERSEDED] [--limit N]
bitbucket-cli pr list [--author ACCOUNT_ID|--mine]
bitbucket-cli pr get <id>
bitbucket-cli pr comments <id> [--limit N]
bitbucket-cli pr commits <id> [--limit N]
```

Use `--author` and `--mine` as alternative filters; the CLI rejects using both
on the same request.

## Pull request writes

These commands modify Bitbucket and should run only when explicitly requested:

```sh
bitbucket-cli pr comment <id> --body "Markdown comment" [--reply-to <comment-id>]
bitbucket-cli pr create --source BRANCH --title "Title" [options]
bitbucket-cli pr update <id> [--title TITLE] [--description TEXT|--description-file FILE]
bitbucket-cli pr attach <id> --file PATH [--file PATH ...] [--message TEXT]
```

`pr create` also accepts `--destination`, `--description-file` (`-` means
stdin), and `--close-source-branch`. `pr update` reads the existing pull request
first so its required title and reviewers are preserved. Bitbucket Cloud has no
native pull-request attachment API: `pr attach` uploads files to repository
Downloads, then posts links in a pull request comment. Duplicate filenames in
one invocation are rejected.

:::danger[Write safety]
Do not run `pr comment`, `pr create`, `pr update`, or `pr attach` unless the user
explicitly asked for the remote change.
:::

## Branches and pipelines

```sh
bitbucket-cli branch list [--query QUERY] [--limit N]
bitbucket-cli pipeline list [--state STATE] [--limit N]
bitbucket-cli pipeline get <uuid>
```

Pipeline states include `PENDING`, `IN_PROGRESS`, `COMPLETED`, `PAUSED`,
`HALTED`, and `ERROR`.
