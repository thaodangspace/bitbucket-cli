---
name: bitbucket-cli
description: "Drive Bitbucket Cloud (pull requests, branches, repo info) from the command line via the `bitbucket-cli` tool. Use when the user asks to list/read/create/update/comment on Bitbucket pull requests, inspect review feedback, fix requested changes, inspect branches or repository details, or otherwise interact with Bitbucket Cloud — and a Bitbucket repo (not GitHub) is in play."
---

# Driving Bitbucket Cloud with `bitbucket-cli`

`bitbucket-cli` is a standalone Go CLI for Bitbucket Cloud pull requests, branches,
and repository info. It is built for agents: **output is JSON on stdout by default**
(parse it directly), and `--pretty` switches to one-line human text. Errors are a JSON
envelope on stderr with a non-zero exit code.

Use this skill for **Bitbucket** repos. For GitHub, use `gh` instead.

## Before you start: verify config

Credentials resolve in this order: **env vars → config file → git-remote auto-detect**.

```bash
bitbucket-cli status        # reports config validity + the default repo
```

If `status` reports missing credentials, the user must set an Atlassian API token:

```bash
bitbucket-cli config set email you@example.com
bitbucket-cli config set api_token <atlassian-api-token>   # never echo or log this
bitbucket-cli config set default_workspace <workspace-slug>  # optional
bitbucket-cli config set default_repo <repo-slug>            # optional
bitbucket-cli config list   # token is redacted
```

The token needs `read:repository:bitbucket`, `read:pullrequest:bitbucket`, and (for
writes) `write:pullrequest:bitbucket`. Inside a git repo whose `origin` is
`bitbucket.org`, workspace/repo are auto-detected — `--workspace`/`--repo` override.

## Read commands (safe — run freely)

```bash
bitbucket-cli pr list --state OPEN          # states: OPEN|MERGED|DECLINED|SUPERSEDED
bitbucket-cli pr list --limit 50            # default limit is 20
bitbucket-cli pr get <id>                   # one PR
bitbucket-cli pr comments <id>              # comments on a PR
bitbucket-cli pr commits <id>               # commits on a PR
bitbucket-cli branch list --query 'name ~ "feature/"'
bitbucket-cli repo list [workspace]         # discover repositories
bitbucket-cli repo view [workspace/repo]     # repository details or --readme
bitbucket-cli repo get                       # compatibility alias for view
bitbucket-cli repo clone workspace/repo      # clone without embedding tokens
bitbucket-cli repo browse workspace/repo path
```

Pipe JSON into `jq` to extract fields, e.g. `bitbucket-cli pr list --state OPEN | jq '.[].id'`.

## Review-fix workflow

When the user asks you to check review feedback and fix it, always add a notes step
before editing code:

1. Read the PR and review context with `bitbucket-cli pr get <id>` and
   `bitbucket-cli pr comments <id>`. Use `bitbucket-cli pr commits <id>` when branch
   context is needed.
2. Create a dated note under `~/code/brain/spec/YYYYMMDD/`, following the same
   sequential numbering pattern as `~/code/skills/` specs:
   `<weight>_task_<title>.md`.
3. In the note, capture the requested changes, affected files, implementation steps,
   and verification commands. Keep it lightweight; this is the working record for the
   review-fix pass.
4. Implement the fixes, run verification, and summarize which review items were
   addressed. Only post or update Bitbucket comments when the user explicitly asked.

`repo set-default` stores a local git preference. Repository resolution precedence
is explicit selector, local git config, matching Bitbucket remote, then global config.

## Write commands (only when the user explicitly asks)

These mutate Bitbucket. **Do not run them speculatively** — only when the user has
clearly asked you to post/create/update. State what you are about to do first.

```bash
# Post a markdown comment or reply to an existing comment/thread
bitbucket-cli pr comment <id> --body "Thanks, taking a look."
bitbucket-cli pr comment <id> --body "Fixed now." --reply-to <comment-id>

# Create a PR (destination defaults to the repo main branch)
bitbucket-cli pr create --source feature/login --title "Add login" \
  --description "Implements the login flow." [--destination main]

# Long bodies: pipe markdown via stdin with `-`
generate-notes | bitbucket-cli pr create --source feature/login \
  --title "Add login" --description-file -

# Update a PR's title/description (reviewers are preserved automatically)
bitbucket-cli pr update <id> --title "Add login (v2)"
bitbucket-cli pr update <id> --description-file release-notes.md
```

`--description` and `--description-file` are mutually exclusive; `--description-file -`
reads stdin.

## Global flags & output contract

- `--workspace <slug>` / `--repo <slug>`: target a specific repo (e.g. `bitbucket-cli --workspace acme --repo web pr list`).
- `--pretty`: one-line text summaries instead of JSON (use for showing the user, not for parsing).
- **List commands** emit a JSON array of full Bitbucket objects; **single-entity** commands emit one object.
- **Errors** → stderr as `{"error":{"message":...,"status":...,"method":...,"url":...,"excerpt":...}}`, exit 1. Check the exit code and surface `.error.message`.

## Tips for agents

- Default to JSON; only add `--pretty` when presenting results to a human.
- Always `bitbucket-cli status` (or trust auto-detect) before assuming a workspace/repo.
- Never print or commit the API token, `.env`, or a config file containing a real token.
- If a command fails, read the JSON error envelope on stderr — `status` and `excerpt` usually explain why (auth, missing scope, wrong repo).
