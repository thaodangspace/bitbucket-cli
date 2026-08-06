---
title: Generated command reference
description: Generated from the Cobra command tree.
---

## `alias`

Manage command aliases

## `alias delete <name>`

Delete a command alias

## `alias list`

List command aliases

## `alias set <name> <command>`

Set a command alias

## `api <endpoint>`

Make an arbitrary Bitbucket Cloud REST 2.0 request

Flags: `--cache` — Cache GET responses for this duration (e.g. 30s, 2m); `--field` — Typed field in key=value form; true/false/null and integers are typed, supports target[x]=y nesting and a[]=v arrays (repeatable); `--header` — Request header in key:value form (repeatable); `--include` — Include response status and headers before the body; `--input` — Raw request body from a file, or - for stdin; `--jq` — Apply a jq expression to JSON output (supports .foo, .foo.bar, [<index>], []); `--max-pages` — Maximum pages to follow with --paginate (0 means unlimited); `--method` — HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS); defaults to GET; `--output` — Write the raw response body to a file instead of stdout; `--paginate` — Follow Bitbucket 'next' pagination links for GET requests; `--raw-field` — String field in key=value form (repeatable); `--silent` — Suppress the response body; `--slurp` — With --paginate, apply --jq/--template to the accumulated array instead of each item; `--template` — Format JSON output with a Go template (adds 'json' and 'pretty' helpers)

## `auth`

Manage bitbucket-cli authentication

## `auth login`

Authenticate with Bitbucket and save credentials securely

Flags: `--email` — Atlassian account email; `--token-type` — Token type: api, access, or oauth; `--with-token` — Read the token from stdin instead of prompting

## `auth logout`

Remove stored credentials from the config file and credential store

Flags: `--yes` — Skip confirmation (required when stdin is not a TTY)

## `auth status`

Report authentication status and credential source

Flags: `--json` — Force JSON output (default)

## `auth token`

Print the active token (opt-in, for scripting)

## `branch`

Branch commands

## `branch list`

List branches in a repository

Flags: `--limit` — Maximum branches to return; `--query` — Bitbucket q expression, e.g. name ~ "feature/"

## `browse [path]`

Open the repository in a browser

Flags: `--branch` — Branch to view; `--no-browser` — Print the URL without opening a browser

## `completion bash|zsh|fish|powershell`

Generate shell completion script

## `config`

Read and write the bitbucket-cli config file

## `config get <key>`

Print a stored config value

## `config list`

Show stored config values (API token redacted)

## `config path`

Print the config file path

## `config set <key> <value>`

Set a config value, writing it to the config file

## `pipeline`

Pipeline commands

## `pipeline get <uuid>`

Get a single pipeline run by UUID with its steps

## `pipeline list`

List pipeline runs for a repository

Flags: `--limit` — Maximum pipelines to return; `--state` — Filter by state: PENDING, IN_PROGRESS, COMPLETED, PAUSED, HALTED, ERROR

## `pr`

Pull request commands

## `pr attach <id>`

Upload files and link them from a pull request comment

Flags: `--file` — Local file to upload (repeatable, required); `--message` — Optional markdown text before the attachment links

## `pr comment <id>`

Post a markdown comment on a pull request

Flags: `--body` — Markdown comment body to post (required); `--reply-to` — Parent comment ID to reply to

## `pr comments <id>`

List comments on a pull request

Flags: `--limit` — Maximum comments to return

## `pr commits <id>`

List commits on a pull request

Flags: `--limit` — Maximum commits to return

## `pr create`

Create a new pull request

Flags: `--close-source-branch` — Close the source branch when the PR is merged; `--description-file` — Read description from file (- for stdin); `--description` — Description (markdown); `--destination` — Destination branch name (defaults to the repo main branch); `--source` — Source branch name (required); `--title` — Pull request title (required)

## `pr get <id>`

Get a single pull request by ID

## `pr list`

List pull requests for a repository

Flags: `--author` — Filter by author account ID (e.g. from the Bitbucket profile URL); `--limit` — Maximum pull requests to return; `--mine` — Filter to pull requests authored by the authenticated user; `--state` — Filter by state: OPEN, MERGED, DECLINED, or SUPERSEDED

## `pr update <id>`

Update a pull request's title and/or description

Flags: `--description-file` — Read description from file (- for stdin); `--description` — New description (markdown); `--title` — New pull request title

## `repo`

Repository commands

## `repo get`

Get details for a Bitbucket Cloud repository

## `status`

Check bitbucket-cli configuration
