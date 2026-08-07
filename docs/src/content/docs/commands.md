---
title: Commands
description: Inspect Bitbucket Cloud resources and perform explicit pull request writes.
---

## Global options

- `--workspace SLUG` overrides the workspace default.
- `--repo SLUG` overrides the repository default.
- `--pretty` prints deterministic table summaries (the table format alias).
- `--json FIELD,...` selects documented stable fields; `--jq EXPR` and
  `--template TEMPLATE` transform the selected object.
- `--format json|table|yaml|raw` chooses the output encoding.
- `-R, --repository` accepts `workspace/repo` or a Bitbucket URL; the legacy
  `--workspace` and `--repo` flags remain supported.
- `--color auto|always|never`, `--pager auto|always|never`, and `--no-pager`
  control shell integration.
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
bitbucket-cli repo list [<workspace>] [--role owner|member|contributor] [--private|--public]
bitbucket-cli repo view [<workspace/repo>] [--readme] [--branch REF] [--web]
bitbucket-cli repo create <name> --workspace WORKSPACE [--private] [--clone|--source PATH]
bitbucket-cli repo edit [<workspace/repo>] [--name NAME] [--description TEXT] [--private=<bool>]
bitbucket-cli repo delete [<workspace/repo>] --yes
bitbucket-cli repo fork [<workspace/repo>] [--workspace TARGET] [--clone] [--remote]
bitbucket-cli repo clone <workspace/repo|url> [<directory>] [-- <git-flags>...]
bitbucket-cli repo browse [<workspace/repo>] [<path>] [--branch REF]
bitbucket-cli repo set-default [<workspace/repo>]
```

`repo get` remains a compatibility alias for `repo view`. `config list` redacts
the API token. Repository resolution gives explicit selectors precedence, then
the local `bitbucket-cli.repository` git config, then matching Bitbucket
remotes, then configured defaults. Repository writes require explicit user
intent; `repo delete` always requires `--yes`.

## Pull requests

```sh
bitbucket-cli pr list [--state OPEN|MERGED|DECLINED|SUPERSEDED] [--limit N]
bitbucket-cli pr list [--author ACCOUNT_ID|--mine]
bitbucket-cli pr view [<selector>] [--comments] [--activity] [--web]
bitbucket-cli pr get [<id-or-url>] [--web] # compatibility alias behavior
bitbucket-cli pr diff [<selector>] [--patch|--stat|--name-only]
bitbucket-cli pr checks [<selector>] [--watch]
bitbucket-cli pr conflicts [<selector>]
bitbucket-cli pr status [--mine|--review-requested]
bitbucket-cli pr comments <id> [--limit N]
bitbucket-cli pr commits <id> [--limit N]
```

Use `--author` and `--mine` as alternative filters; the CLI rejects using both
on the same request.

## Pull request writes

These commands modify Bitbucket and should run only when explicitly requested:

```sh
bitbucket-cli pr comment [<id>] --body "Markdown comment" [--reply-to <comment-id>]
bitbucket-cli pr comment [<id>] --body "Fix this line" --path path/to/file.go --to 42
bitbucket-cli pr review [<id>] --approve [--body "Looks good"]
bitbucket-cli pr review [<id>] --request-changes --body-file review.md
bitbucket-cli pr review [<id>] --comment --body "Please clarify"
bitbucket-cli pr unapprove [<id>]
bitbucket-cli pr remove-change-request [<id>]
bitbucket-cli pr thread resolve <id> <comment-id>
bitbucket-cli pr thread reopen <id> <comment-id>
bitbucket-cli pr task list <id> [--state OPEN|RESOLVED]
bitbucket-cli pr task create <id> --body "Add a test"
bitbucket-cli pr task update <id> <task-id> [--body TEXT] [--state OPEN|RESOLVED]
bitbucket-cli pr task delete <id> <task-id> --yes
bitbucket-cli pr create --source BRANCH --title "Title" [options]
bitbucket-cli pr update <id> [--title TITLE] [--description TEXT|--description-file FILE]
bitbucket-cli pr attach <id> --file PATH [--file PATH ...] [--message TEXT]
bitbucket-cli pr merge [<selector>] [--strategy merge_commit|squash|fast_forward]
bitbucket-cli pr decline [<selector>] [--message TEXT] [--yes]
bitbucket-cli pr reopen [<selector>]
```

`pr create` also accepts `--destination`, `--description-file` (`-` means
stdin), and `--close-source-branch`. `pr update` reads the existing pull request
first so its required title and reviewers are preserved. Bitbucket Cloud has no
native pull-request attachment API: `pr attach` uploads files to repository
Downloads, then posts links in a pull request comment. Duplicate filenames in
one invocation are rejected.

Review bodies are ordinary PR comments posted before the participant action;
Bitbucket does not make that sequence atomic. If approval or a change request
fails after the comment succeeds, the error includes the created comment ID.
Inline comments use Bitbucket's old/new file line numbers (`--from` and
`--to`); unified-diff positions are not translated. Comment and task deletes
require `--yes`, and task updates preserve fields that were not specified.

:::danger[Write safety]
Do not run `pr comment`, `pr review`, `pr unapprove`,
`pr remove-change-request`, `pr thread`, `pr task create/update/delete`,
`pr create`, `pr update`, `pr attach`, `pr merge`, `pr decline`, or `pr reopen`
unless the user explicitly asked for the remote change.
:::

## Branches, tags, policies, and pipelines

```sh
bitbucket-cli branch list [--query QUERY] [--sort FIELD] [--limit N]
bitbucket-cli branch view <name>
bitbucket-cli branch create <name> --target <commit|branch|tag>
bitbucket-cli branch delete <name> --yes
bitbucket-cli tag list [--query QUERY] [--sort FIELD] [--limit N]
bitbucket-cli tag view <name>
bitbucket-cli tag create <name> --target <commit|branch> [--message TEXT]
bitbucket-cli tag delete <name> --yes
bitbucket-cli branching-model view
bitbucket-cli branching-model edit [--production-branch BRANCH] [--development-branch BRANCH] [--feature-prefix PREFIX]
bitbucket-cli branch-restriction list [--kind KIND] [--pattern GLOB|--branch-type TYPE]
bitbucket-cli branch-restriction view <id>
bitbucket-cli branch-restriction create --kind KIND (--pattern GLOB|--branch-type TYPE) [--value N]
bitbucket-cli branch-restriction edit <id> [--from-file POLICY.yaml]
bitbucket-cli branch-restriction delete <id> --yes
bitbucket-cli default-reviewer list
bitbucket-cli default-reviewer add <user-selector>
bitbucket-cli default-reviewer remove <user-selector> --yes
bitbucket-cli pipeline list [--state STATE] [--limit N]
bitbucket-cli pipeline get <uuid> [--web]
bitbucket-cli browse [path] [--branch BRANCH] [--no-browser]
bitbucket-cli completion bash|zsh|fish|powershell
bitbucket-cli alias set <name> <command>
bitbucket-cli alias delete <name>
bitbucket-cli alias list
```

Branch and tag mutations resolve branch/tag targets to an immutable commit hash,
reject existing refs, and URL-encode names as a single path segment. Branch
creation reports both `requested_target` and `resolved_target`; tag messages
request annotated tags; Bitbucket supplies a default message when `--message`
is omitted, and this API does not provide a lightweight-tag mode. Branch deletion always requires `--yes` and refuses the configured
main branch.

Branch restrictions use typed match modes: exactly one glob `--pattern` or
branching-model `--branch-type`. Approval/build restrictions require `--value`
(or `--approvals`/`--builds`); `--from-file` accepts YAML or JSON and `--export`
writes a secret-free reproducible policy file. User and group display-name
selectors must resolve uniquely.

Pipeline states include `PENDING`, `IN_PROGRESS`, `COMPLETED`, `PAUSED`,
`HALTED`, and `ERROR`.

## Generic `api` command

`api` is an escape hatch for any Bitbucket Cloud REST 2.0 endpoint that is newly
released or not yet wrapped by a typed command. It mirrors the behavior of
`gh api`.

```sh
bitbucket-cli api <endpoint> [flags]
```

Flags:

- `-X, --method` — GET (default), POST, PUT, PATCH, DELETE, HEAD, or OPTIONS.
- `-H, --header key:value` — additional request header (repeatable).
- `-f, --raw-field key=value` — always sends the value as a string (repeatable).
- `-F, --field key=value` — typed value; `true`, `false`, `null`, and integers
  are JSON-typed. Supports nesting (`target[ref_type]=branch`) and repeated
  arrays (`variables[]=a`).
- `--input FILE|-` — send a raw request body from a file or stdin.
- `--paginate` — follow Bitbucket `next` links and emit each JSON page.
  `--slurp` wraps all pages in an array; `--jq`/`--template` apply per page
  unless `--slurp` is set. `--max-pages N` bounds traversal; `0` is unlimited.
- `-i, --include` — print the response status and headers before the body.
- `--silent` — discard the response body.
- `-o, --output FILE` — write the raw response body to a file (no excerpt
  truncation).
- `-q, --jq EXPR` — filter JSON output using jq syntax (gojq).
- `-t, --template EXPR` — format JSON output with a Go template (`json` and
  `pretty` helpers are provided).
- `--cache DURATION` — persist GET responses in the user cache for the given
  duration (e.g. `30s`, `2m`). Cache entries are restricted to the current user.

Endpoint resolution accepts relative paths such as
`/repositories/{workspace}/{repo}/pullrequests` (placeholders expand from the
usual flags/config/git remote) or absolute `https://api.bitbucket.org/...`
URLs. Absolute URLs on other hosts are rejected. `-f`/`-F` fields are sent in
the JSON request body for POST/PUT/PATCH, and as query parameters for other
methods or when `--input` is used.

`--input` is mutually exclusive with fields in the request body. When both are
needed, the fields are encoded as query parameters and the input remains the
raw request body.

```sh
# Read the current user
bitbucket-cli api /user

# List and paginate PRs
bitbucket-cli api /repositories/{workspace}/{repo}/pullrequests --paginate

# Approve a PR (write operation)
bitbucket-cli api /repositories/{workspace}/{repo}/pullrequests/42/approve -X POST

# Trigger a pipeline with nested + typed fields
bitbucket-cli api /repositories/{workspace}/{repo}/pipelines \
  -X POST -F 'target[ref_type]=branch' -F 'target[ref_name]=main'

# Download a raw PR diff
bitbucket-cli api /repositories/{workspace}/{repo}/pullrequests/42/diff --output pr.diff

# Create a repository webhook (write operation)
bitbucket-cli api /repositories/{workspace}/{repo}/hooks -X POST \
  -F 'description=CI' \
  -F 'url=https://ci.example.com/bitbucket-hook' \
  -F 'active=true'
```

:::danger[Write safety]
`api` is a **write operation** whenever `--method` is POST, PUT, PATCH, or
DELETE. Run it only when the user has explicitly asked for the change.
:::

### `api` security notes

- Non-2xx responses are returned as structured errors whose excerpt is truncated
  and has any configured token redacted. Credentials never appear in the error
  output.
- `--output` writes the raw response body unchanged — the file may contain
  sensitive data, so treat it accordingly.
- Request bodies sent via `-f`/`-F`/`--input` can contain secrets; avoid logging
  them. Prefer Bitbucket scoped tokens (`BITBUCKET_TOKEN_TYPE=access` or OAuth)
  for automation.
- Webhook resource URLs (for example, when configuring webhooks) are external;
  the CLI rejects absolute API hosts other than `api.bitbucket.org`.
