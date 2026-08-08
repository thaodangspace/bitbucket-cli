---
title: Generated command reference
description: Generated from the Cobra command tree.
---

## `alias`

Manage command aliases

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli alias`

## `alias delete <name>`

Delete a command alias

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli alias delete <name>`

## `alias list`

List command aliases

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli alias list`

## `alias set <name> <command>`

Set a command alias

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli alias set <name> <command>`

## `annotation`

Manage Code Insights annotations

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli annotation`
- JSON fields: `external_id,path,file_path,line,start_line,end_line,summary,message,severity,result,link`

## `annotation delete <commit> <report-id> <annotation-id>`

Delete a report annotation

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli annotation delete <commit> <report-id> <annotation-id>`
- JSON fields: `external_id,path,file_path,line,start_line,end_line,summary,message,severity,result,link`

Flags: `--yes` — Confirm deletion

## `annotation list <commit> <report-id>`

List report annotations

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli annotation list <commit> <report-id>`
- JSON fields: `external_id,path,file_path,line,start_line,end_line,summary,message,severity,result,link`

Flags: `--limit` — Maximum annotations to return

## `annotation upsert <commit> <report-id>`

Create or update report annotations

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli annotation upsert <commit> <report-id>`

> Warning: report payloads and annotations are visible to repository users with access; never include secrets.
- JSON fields: `external_id,path,file_path,line,start_line,end_line,summary,message,severity,result,link`

Flags: `--file` — JSON annotation object or array

## `api <endpoint>`

Make an arbitrary Bitbucket Cloud REST 2.0 request

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli api <endpoint>`

Flags: `--cache` — Cache GET responses for this duration (e.g. 30s, 2m); `--field` — Typed field in key=value form; true/false/null and integers are typed, supports target[x]=y nesting and a[]=v arrays (repeatable); `--header` — Request header in key:value form (repeatable); `--include` — Include response status and headers before the body; `--input` — Raw request body from a file, or - for stdin; `--jq` — Apply a jq expression to JSON output (supports .foo, .foo.bar, [<index>], []); `--max-pages` — Maximum pages to follow with --paginate (0 means unlimited); `--method` — HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS); defaults to GET; `--output` — Write the raw response body to a file instead of stdout; `--paginate` — Follow Bitbucket 'next' pagination links for GET requests; `--raw-field` — String field in key=value form (repeatable); `--silent` — Suppress the response body; `--slurp` — With --paginate, apply --jq/--template to the accumulated array instead of each item; `--template` — Format JSON output with a Go template (adds 'json' and 'pretty' helpers)

## `auth`

Manage bitbucket-cli authentication

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli auth`

## `auth login`

Authenticate with Bitbucket and save credentials securely

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli auth login`

Flags: `--email` — Atlassian account email; `--token-type` — Token type: api, access, or oauth; `--with-token` — Read the token from stdin instead of prompting

## `auth logout`

Remove stored credentials from the config file and credential store

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli auth logout`

Flags: `--yes` — Skip confirmation (required when stdin is not a TTY)

## `auth status`

Report authentication status and credential source

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli auth status`

Flags: `--json` — Force JSON output (default)

## `auth token`

Print the active token (opt-in, for scripting)

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli auth token`

## `branch`

Branch commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch`

## `branch create <name> --target <commit|branch|tag>`

Create a branch

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch create <name> --target <commit|branch|tag>`

Flags: `--target` — Commit hash, branch, or tag to point at

## `branch delete <name> --yes`

Delete a branch

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch delete <name> --yes`

Flags: `--yes` — Confirm deletion

## `branch list`

List branches in a repository

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch list`
- JSON fields: `name,target,links,requested_target,resolved_target`

Flags: `--limit` — Maximum branches to return; `--query` — Bitbucket q expression, e.g. name ~ "feature/"; `--sort` — Sort field, optionally prefixed with -

## `branch view <name>`

View a branch

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch view <name>`
- JSON fields: `name,target,links,requested_target,resolved_target`

## `branch-restriction`

Branch restriction commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

## `branch-restriction create --kind <kind> (--pattern <glob>|--branch-type <type>)`

Create a branch restriction

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction create --kind <kind> (--pattern <glob>|--branch-type <type>)`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

Flags: `--approvals` — Required approvals; `--branch-type` — Branching-model branch type; `--builds` — Required passing builds; `--from-file` — Read policy fields from YAML or JSON; `--group` — Group selector (repeatable); `--kind` — Restriction kind; `--pattern` — Glob pattern; `--user` — User selector (repeatable); `--value` — Kind-specific numeric requirement

## `branch-restriction delete <id> --yes`

Delete a branch restriction

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction delete <id> --yes`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

Flags: `--yes` — Confirm deletion

## `branch-restriction edit <id> [flags]`

Edit a branch restriction

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction edit <id> [flags]`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

Flags: `--approvals` — Required approvals; `--branch-type` — Branching-model branch type; `--builds` — Required passing builds; `--from-file` — Read policy fields from YAML or JSON; `--group` — Group selector (repeatable); `--kind` — Restriction kind; `--pattern` — Glob pattern; `--user` — User selector (repeatable); `--value` — Kind-specific numeric requirement

## `branch-restriction list`

List branch restrictions

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction list`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

Flags: `--branch-type` — Branching-model branch type; `--export` — Export restrictions to a YAML or JSON file (omit the value for stdout); `--kind` — Restriction kind; `--limit` — Maximum restrictions to return; `--pattern` — Glob pattern

## `branch-restriction view <id>`

View a branch restriction

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branch-restriction view <id>`
- JSON fields: `id,kind,branch_match_kind,pattern,branch_type,value,users,groups`

## `branching-model`

Branching model commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branching-model`
- JSON fields: `development,production,branch_types,default_branch_deletion`

## `branching-model edit`

Edit the repository branching model

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli branching-model edit`
- JSON fields: `development,production,branch_types,default_branch_deletion`

Flags: `--bugfix-prefix` — Bugfix branch prefix; `--development-branch` — Development branch name; `--development-use-main` — Make development track the main branch; `--disable-production` — Disable the production branch; `--disable` — Disable a branch type (repeatable: feature, bugfix, release, hotfix); `--enable-production` — Enable the production branch; `--enable` — Enable a branch type (repeatable: feature, bugfix, release, hotfix); `--feature-prefix` — Feature branch prefix; `--hotfix-prefix` — Hotfix branch prefix; `--production-branch` — Production branch name; `--release-prefix` — Release branch prefix

## `branching-model view`

View the repository branching model

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli branching-model view`
- JSON fields: `development,production,branch_types,default_branch_deletion`

## `browse [path]`

Open the repository in a browser

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli browse [path]`

Flags: `--branch` — Branch to view; `--no-browser` — Print the URL without opening a browser

## `commit`

Browse commits and commit checks

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit`

## `commit approve <commit>`

Approve a commit

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit approve <commit>`

## `commit comment`

Manage commit comments

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit comment`
- JSON fields: `id,content,user,inline,created_on,updated_on`

## `commit comment create <commit>`

Create a commit comment

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit comment create <commit>`
- JSON fields: `id,content,user,inline,created_on,updated_on`

Flags: `--body-file` — Read the body from a file (- for stdin); `--body` — Markdown comment body; `--from` — Old-side line number; `--path` — File path for an inline comment; `--to` — New-side line number

## `commit comment delete <commit> <comment-id>`

Delete a commit comment

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit comment delete <commit> <comment-id>`
- JSON fields: `id,content,user,inline,created_on,updated_on`

Flags: `--yes` — Confirm deletion

## `commit comment edit <commit> <comment-id>`

Edit a commit comment

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit comment edit <commit> <comment-id>`
- JSON fields: `id,content,user,inline,created_on,updated_on`

Flags: `--body-file` — Read the replacement body from a file (- for stdin); `--body` — Replacement markdown body

## `commit comment list <commit>`

List comments on a commit

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit comment list <commit>`
- JSON fields: `id,content,user,inline,created_on,updated_on`

Flags: `--limit` — Maximum comments to return

## `commit diff <commit-or-range>`

Show a commit or range diff

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit diff <commit-or-range>`

Range semantics: `A..B` follows Bitbucket's API semantics—commits reachable from B excluding commits reachable from A.

Flags: `--context` — Number of context lines; `--name-only` — Show changed file names; `--patch` — Use Bitbucket's patch representation; `--stat` — Show diffstat

## `commit list [<ref>]`

List repository commits

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit list [<ref>]`
- JSON fields: `hash,message,author,date,links,requested_selector,resolved_hash`

Flags: `--exclude` — Exclude commits reachable from this ref (repeatable); `--include` — Include commits reachable from this ref (repeatable); `--limit` — Maximum commits to return; `--path` — Only commits affecting this path; `--query` — Bitbucket query expression (BBQL)

## `commit status`

Manage commit build statuses

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit status`
- JSON fields: `key,state,name,description,url,refname,created_on,updated_on`

## `commit status list <commit>`

List build statuses for a commit

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit status list <commit>`
- JSON fields: `key,state,name,description,url,refname,created_on,updated_on`

Flags: `--limit` — Maximum statuses to return

## `commit status set <commit>`

Create or update a commit build status

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit status set <commit>`
- JSON fields: `key,state,name,description,url,refname,created_on,updated_on`

Flags: `--description` — Status description; `--key` — Unique status key; `--name` — Status name; `--ref` — Reference name; `--state` — Status: INPROGRESS, SUCCESSFUL, FAILED, or STOPPED; `--url` — Build target URL

## `commit unapprove <commit>`

Remove your approval from a commit

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli commit unapprove <commit>`

## `commit view <commit>`

View a repository commit

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli commit view <commit>`
- JSON fields: `hash,message,author,date,links,requested_selector,resolved_hash`

Flags: `--comments` — Include commit comments; `--reports` — Include Code Insights reports; `--statuses` — Include commit build statuses; `--web` — Open the commit in a browser

## `completion bash|zsh|fish|powershell`

Generate shell completion script

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli completion bash|zsh|fish|powershell`

## `config`

Read and write the bitbucket-cli config file

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli config`

## `config get <key>`

Print a stored config value

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli config get <key>`

## `config list`

Show stored config values (API token redacted)

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli config list`

## `config path`

Print the config file path

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli config path`

## `config set <key> <value>`

Set a config value, writing it to the config file

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli config set <key> <value>`

## `default-reviewer`

Default reviewer commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli default-reviewer`
- JSON fields: `uuid,account_id,display_name,nickname,links`

## `default-reviewer add <user-selector>`

Add a default reviewer

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli default-reviewer add <user-selector>`
- JSON fields: `uuid,account_id,display_name,nickname,links`

## `default-reviewer list`

List default reviewers

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli default-reviewer list`
- JSON fields: `uuid,account_id,display_name,nickname,links`

## `default-reviewer remove <user-selector> --yes`

Remove a default reviewer

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli default-reviewer remove <user-selector> --yes`
- JSON fields: `uuid,account_id,display_name,nickname,links`

Flags: `--yes` — Confirm removal

## `deploy-key`

Manage repository deploy keys (read-only Git access)

- Classification: **read**
- Required scopes: `read:ssh-key:bitbucket and admin:repository:bitbucket`
- Example: `bitbucket-cli deploy-key`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

## `deploy-key add --file <public-key>`

Add a repository deploy key

- Classification: **write**
- Required scopes: `write:ssh-key:bitbucket and admin:repository:bitbucket`
- Example: `bitbucket-cli deploy-key add --file <public-key>`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--file` — OpenSSH public-key file, or - for stdin (required); `--label` — Key label; `--repository` — Repository selector (required)

## `deploy-key delete <id> --yes`

Delete a repository deploy key

- Classification: **write**
- Required scopes: `write:ssh-key:bitbucket and admin:repository:bitbucket`
- Example: `bitbucket-cli deploy-key delete <id> --yes`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--repository` — Repository selector (required); `--yes` — Confirm deletion

## `deploy-key list`

List repository deploy keys

- Classification: **read**
- Required scopes: `read:ssh-key:bitbucket and admin:repository:bitbucket`
- Example: `bitbucket-cli deploy-key list`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--limit` — Maximum deploy keys to return; `--repository` — Repository selector (workspace/repo or URL)

## `permission`

Repository permission commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli permission`
- JSON fields: `permission,user,group,repository,before,after,changed`

## `permission grant --repository <workspace/repo> (--user <selector>|--group <slug>)`

Grant an explicit repository permission

- Classification: **write**
- Required scopes: `admin:repository:bitbucket, write:permission:bitbucket`
- Example: `bitbucket-cli permission grant --repository <workspace/repo> (--user <selector>|--group <slug>)`
- JSON fields: `permission,user,group,repository,before,after,changed`

Flags: `--group` — Group slug; `--permission` — Permission level: read, write, or admin; `--user` — User selector

## `permission groups`

List explicit repository group permissions

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli permission groups`
- JSON fields: `permission,user,group,repository,before,after,changed`

Flags: `--limit` — Maximum permissions to return

## `permission repos --user <user-selector>`

List repositories accessible to a user

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli permission repos --user <user-selector>`
- JSON fields: `permission,user,group,repository,before,after,changed`

Flags: `--limit` — Maximum permissions to return; `--query` — Additional Bitbucket q expression; `--user` — User selector (required); `--workspace` — Workspace slug or UUID

## `permission revoke --repository <workspace/repo> (--user <selector>|--group <slug>)`

Revoke an explicit repository permission

- Classification: **write**
- Required scopes: `admin:repository:bitbucket, delete:permission:bitbucket`
- Example: `bitbucket-cli permission revoke --repository <workspace/repo> (--user <selector>|--group <slug>)`
- JSON fields: `permission,user,group,repository,before,after,changed`

Flags: `--group` — Group slug; `--user` — User selector; `--yes` — Confirm revocation

## `permission users`

List explicit repository user permissions

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli permission users`
- JSON fields: `permission,user,group,repository,before,after,changed`

Flags: `--limit` — Maximum permissions to return

## `pipeline`

Pipeline commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli pipeline`

## `pipeline cache`

Manage pipeline caches

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline cache`

## `pipeline cache delete <uuid|all>`

Delete a pipeline cache

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline cache delete <uuid|all>`

Flags: `--all` — Confirm all-cache deletion; `--yes` — Confirm deletion

## `pipeline cache list`

List pipeline caches

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline cache list`

Flags: `--limit` — Maximum caches

## `pipeline config`

Manage pipeline configuration

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline config`

## `pipeline config disable`

Disable Pipelines

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline config disable`

Flags: `--yes` — Confirm disabling Pipelines

## `pipeline config enable`

Enable Pipelines

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline config enable`

## `pipeline config view`

View pipeline configuration

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline config view`

## `pipeline get <uuid>`

Get a single pipeline run by UUID with its steps

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline get <uuid>`
- JSON fields: `uuid,build_number,state,target,trigger,steps`

Flags: `--web` — Open the pipeline in a browser

## `pipeline list`

List pipeline runs for a repository

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline list`
- JSON fields: `uuid,build_number,state,target,trigger,steps`

Flags: `--limit` — Maximum pipelines to return; `--state` — Filter by state: PENDING, IN_PROGRESS, COMPLETED, PAUSED, HALTED, ERROR

## `pipeline log <pipeline> [step]`

Read a pipeline step log

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline log <pipeline> [step]`

Flags: `--follow` — Poll for new log content; `--interval` — Polling interval; `--output` — Write atomically to this file

## `pipeline run`

Start a pipeline

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline run`

Flags: `--branch` — Run against a branch; `--commit` — Run against a commit hash; `--custom` — Custom pipeline selector; `--secured-variable` — Secured variable key; read its value from stdin (repeatable); `--tag` — Run against a tag; `--variable` — Pipeline variable KEY=VALUE (repeatable); `--wait` — Wait for the pipeline to finish

## `pipeline runner`

Manage pipeline runners

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline runner`

## `pipeline runner create`

Create a pipeline runner

- Classification: **write**
- Required scopes: `read:runner:bitbucket and write:runner:bitbucket`
- Example: `bitbucket-cli pipeline runner create`

Flags: `--labels` — Runner labels; `--name` — Runner name; `--workspace` — Create workspace runner

## `pipeline runner delete <uuid>`

Delete a pipeline runner

- Classification: **write**
- Required scopes: `write:runner:bitbucket`
- Example: `bitbucket-cli pipeline runner delete <uuid>`

Flags: `--workspace` — Delete workspace runner; `--yes` — Confirm deletion

## `pipeline runner edit <uuid>`

Edit a pipeline runner

- Classification: **write**
- Required scopes: `read:runner:bitbucket and write:runner:bitbucket`
- Example: `bitbucket-cli pipeline runner edit <uuid>`

Flags: `--enabled` — Enable or disable runner; `--labels` — Runner labels; `--name` — Runner name; `--workspace` — Edit workspace runner

## `pipeline runner list`

List pipeline runners

- Classification: **read**
- Required scopes: `read:runner:bitbucket`
- Example: `bitbucket-cli pipeline runner list`

Flags: `--limit` — Maximum runners; `--workspace` — Use workspace runners

## `pipeline runner view <uuid>`

View a pipeline runner

- Classification: **read**
- Required scopes: `read:runner:bitbucket`
- Example: `bitbucket-cli pipeline runner view <uuid>`

Flags: `--workspace` — Use workspace runner

## `pipeline schedule`

Manage pipeline schedules

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule`

## `pipeline schedule create`

Create a pipeline schedule

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule create`

Flags: `--branch` — Schedule branch; `--commit` — Schedule commit hash; `--cron` — Five-field cron expression; `--custom` — Custom pipeline selector; `--enabled` — Enable the schedule; `--tag` — Schedule tag

## `pipeline schedule delete <uuid>`

Delete a pipeline schedule

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule delete <uuid>`

Flags: `--yes` — Confirm deletion

## `pipeline schedule edit <uuid>`

Edit a pipeline schedule

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule edit <uuid>`

Flags: `--branch` — New branch; `--commit` — New commit; `--cron` — New cron expression; `--custom` — New custom selector; `--enabled` — Enable or disable; `--tag` — New tag

## `pipeline schedule list`

List pipeline schedules

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule list`

## `pipeline schedule runs <uuid>`

List schedule runs

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline schedule runs <uuid>`

Flags: `--limit` — Maximum runs

## `pipeline steps <pipeline>`

List pipeline steps

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline steps <pipeline>`

Flags: `--limit` — Maximum steps to return

## `pipeline stop <pipeline>`

Stop a pipeline

- Classification: **write**
- Required scopes: `write:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline stop <pipeline>`

Flags: `--yes` — Confirm stopping the pipeline

## `pipeline test-report <pipeline> [step]`

Read a pipeline test report

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline test-report <pipeline> [step]`

Flags: `--cases` — Include test cases; `--output` — Write raw report output atomically to this file

## `pipeline variable`

Manage pipeline variables

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline variable`

## `pipeline variable delete <key-or-uuid>`

Delete a pipeline variable

- Classification: **write**
- Required scopes: `admin:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline variable delete <key-or-uuid>`

Flags: `--environment` — Deployment environment UUID; `--scope` — Variable scope; `--yes` — Confirm deletion

## `pipeline variable list`

List pipeline variables

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline variable list`

Flags: `--environment` — Deployment environment UUID; `--scope` — repository, workspace, or deployment

## `pipeline variable set <key>`

Create or update a pipeline variable

- Classification: **write**
- Required scopes: `admin:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline variable set <key>`

Flags: `--create` — Require creation; `--environment` — Deployment environment UUID; `--scope` — Variable scope; `--secured` — Mark variable secured; `--update` — Require update; `--value-file` — Read value from a file; `--value-stdin` — Read value from stdin; `--value` — Variable value

## `pipeline watch <pipeline>`

Watch a pipeline until it finishes

- Classification: **read**
- Required scopes: `read:pipeline:bitbucket`
- Example: `bitbucket-cli pipeline watch <pipeline>`
- JSON fields: `uuid,build_number,state,target,trigger,steps`

Flags: `--exit-status` — Exit non-zero for an unsuccessful result; `--interval` — Polling interval

## `pr`

Pull request commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli pr`

## `pr attach <id>`

Upload files and link them from a pull request comment

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr attach <id>`

Flags: `--file` — Local file to upload (repeatable, required); `--message` — Optional markdown text before the attachment links

## `pr checkout [<selector>]`

Check out a pull request locally

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr checkout [<selector>]`

Flags: `--branch` — Local branch name (defaults to the pull request source branch); `--detach` — Check out the source commit without creating a local branch; `--force` — Allow resetting an existing or diverged local branch; `--recurse-submodules` — Initialize and update submodules recursively

## `pr checks [<selector>]`

Show pull request commit checks

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr checks [<selector>]`

Flags: `--interval` — Polling interval; `--watch` — Wait for checks to finish

## `pr comment [<id>]`

Create or manage pull request comments

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr comment [<id>]`
- JSON fields: `id,content,user,parent,inline,pending,resolution,created_on,updated_on`

Flags: `--body-file` — Read the comment body from a file (- for stdin); `--body` — Markdown comment body; `--from` — Positive old-side file line number for an inline comment; `--path` — File path for an inline comment; `--pending` — Send Bitbucket's pending=true comment field; this creates no local draft and may be rejected by unsupported workflows; `--reply-to` — Parent comment ID to reply to; `--to` — Positive new-side file line number for an inline comment

## `pr comment delete <pr> <comment-id>`

Delete a pull request comment

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr comment delete <pr> <comment-id>`
- JSON fields: `id,content,user,parent,inline,pending,resolution,created_on,updated_on`

Flags: `--yes` — Confirm deletion

## `pr comment edit <pr> <comment-id>`

Edit a pull request comment

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr comment edit <pr> <comment-id>`
- JSON fields: `id,content,user,parent,inline,pending,resolution,created_on,updated_on`

Flags: `--body-file` — Read the replacement body from a file (- for stdin); `--body` — Replacement markdown body

## `pr comments <id>`

List comments on a pull request

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr comments <id>`
- JSON fields: `id,content,user,parent,inline,pending,resolution,created_on,updated_on`

Flags: `--limit` — Maximum comments to return

## `pr commits <id>`

List commits on a pull request

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr commits <id>`

Flags: `--limit` — Maximum commits to return

## `pr conflicts [<selector>]`

Show pull request conflicts

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr conflicts [<selector>]`

## `pr create`

Create a new pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr create`

Flags: `--close-source-branch` — Close the source branch when the PR is merged; `--description-file` — Read description from file (- for stdin); `--description` — Description (markdown); `--destination` — Destination branch name (defaults to the repo main branch); `--source` — Source branch name (required); `--title` — Pull request title (required)

## `pr current`

Find the open pull request for the current branch

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr current`
- JSON fields: `id,title,state,author,source,destination,reviewers`

Flags: `--web` — Open the current branch pull request in a browser

## `pr decline [<selector>]`

Decline a pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr decline [<selector>]`

Flags: `--message-file` — Read decline message from a file (- for stdin); `--message` — Decline message; `--yes` — Confirm the destructive action

## `pr diff [<selector>]`

Show pull request changes

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr diff [<selector>]`

Flags: `--name-only` — Show changed filenames only; `--patch` — Show a patch; `--stat` — Show diffstat

## `pr list`

List pull requests for a repository

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr list`
- JSON fields: `id,title,state,author,source,destination,reviewers`

Flags: `--author` — Filter by author account ID (e.g. from the Bitbucket profile URL); `--limit` — Maximum pull requests to return; `--mine` — Filter to pull requests authored by the authenticated user; `--state` — Filter by state: OPEN, MERGED, DECLINED, or SUPERSEDED

## `pr merge [<selector>]`

Merge a pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr merge [<selector>]`

Flags: `--async` — Return after Bitbucket accepts an asynchronous merge; `--close-source-branch` — Close the source branch after merging; `--interval` — Merge task polling interval; `--message-file` — Read merge message from a file (- for stdin); `--message` — Merge commit message; `--strategy` — Merge strategy: merge_commit, squash, or fast_forward; `--timeout` — Maximum time to wait for an asynchronous merge

## `pr remove-change-request [<id>]`

Remove your change request from a pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr remove-change-request [<id>]`

## `pr reopen [<selector>]`

Reopen a declined pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr reopen [<selector>]`

## `pr review [<id>]`

Approve, request changes, or comment on a pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr review [<id>]`

Flags: `--approve` — Approve the pull request; `--body-file` — Read the review/comment body from a file (- for stdin); `--body` — Review/comment body; `--comment` — Post a review comment without approving or requesting changes; `--request-changes` — Request changes (requires a non-empty review body)

## `pr status`

Show pull requests for review or the current branch

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr status`
- JSON fields: `id,title,state,author,source,destination,reviewers`

Flags: `--mine` — Show pull requests authored by the current account; `--review-requested` — Show pull requests where the current account is a reviewer; `--workspace` — Workspace slug for status views

## `pr task`

Manage pull request tasks

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr task`

## `pr task create <pr>`

Create a pull request task

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr task create <pr>`

Flags: `--body-file` — Read task body from a file (- for stdin); `--body` — Task body; `--comment` — Associate the task with a comment ID

## `pr task delete <pr> <task-id>`

Delete a pull request task

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr task delete <pr> <task-id>`

Flags: `--yes` — Confirm deletion

## `pr task list <pr>`

List pull request tasks

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr task list <pr>`
- JSON fields: `id,content,state,comment,creator,pending,resolved_on,resolved_by,created_on,updated_on`

Flags: `--limit` — Maximum tasks to return; `--state` — Filter by task state: OPEN or RESOLVED

## `pr task update <pr> <task-id>`

Update a pull request task

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr task update <pr> <task-id>`

Flags: `--body-file` — Read replacement task body from a file (- for stdin); `--body` — Replacement task body; `--state` — Task state: OPEN or RESOLVED

## `pr thread`

Resolve or reopen pull request comment threads

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr thread`

## `pr thread reopen <pr> <comment-id>`

Reopen a pull request comment thread

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr thread reopen <pr> <comment-id>`

## `pr thread resolve <pr> <comment-id>`

Resolve a pull request comment thread

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr thread resolve <pr> <comment-id>`

## `pr unapprove [<id>]`

Remove your approval from a pull request

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr unapprove [<id>]`

## `pr update <id>`

Update a pull request's title and/or description

- Classification: **write**
- Required scopes: `read:pullrequest:bitbucket, write:pullrequest:bitbucket`
- Example: `bitbucket-cli pr update <id>`

Flags: `--description-file` — Read description from file (- for stdin); `--description` — New description (markdown); `--title` — New pull request title

## `pr view [<selector>]`

View a pull request by ID, URL, or current branch

- Classification: **read**
- Required scopes: `read:pullrequest:bitbucket`
- Example: `bitbucket-cli pr view [<selector>]`
- JSON fields: `id,title,state,author,source,destination,reviewers`

Flags: `--activity` — Include pull request activity; `--comments` — Include pull request comments; `--web` — Open the pull request in a browser

## `project`

Project commands

- Classification: **read**
- Required scopes: `read:project:bitbucket`
- Example: `bitbucket-cli project`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

## `project create --key <key> --name <name>`

Create a project

- Classification: **write**
- Required scopes: `admin:project:bitbucket`
- Example: `bitbucket-cli project create --key <key> --name <name>`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

Flags: `--description` — Project description; `--key` — Project key (required); `--name` — Project name (required); `--private` — Whether the project is private; `--workspace` — Workspace slug or UUID

## `project delete <key-or-uuid> --yes`

Delete a project

- Classification: **write**
- Required scopes: `admin:project:bitbucket`
- Example: `bitbucket-cli project delete <key-or-uuid> --yes`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

Flags: `--workspace` — Workspace slug or UUID; `--yes` — Confirm deletion

## `project edit <key-or-uuid>`

Edit a project

- Classification: **write**
- Required scopes: `admin:project:bitbucket`
- Example: `bitbucket-cli project edit <key-or-uuid>`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

Flags: `--description` — New project description; `--key` — New project key; `--name` — New project name; `--private` — Whether the project is private; `--workspace` — Workspace slug or UUID

## `project list`

List projects

- Classification: **read**
- Required scopes: `read:project:bitbucket`
- Example: `bitbucket-cli project list`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

Flags: `--limit` — Maximum projects to return; `--query` — Bitbucket q expression; `--workspace` — Workspace slug or UUID

## `project view <key-or-uuid>`

View a project

- Classification: **read**
- Required scopes: `read:project:bitbucket`
- Example: `bitbucket-cli project view <key-or-uuid>`
- JSON fields: `key,name,uuid,description,is_private,links,repositories`

Flags: `--repos` — Include repositories in the project; `--web` — Open the project in a browser; `--workspace` — Workspace slug or UUID

## `repo`

Repository commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli repo`

## `repo browse [<workspace/repo>] [<path>]`

Open a repository path in a browser

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli repo browse [<workspace/repo>] [<path>]`

Flags: `--branch` — Branch or ref to browse

## `repo clone <workspace/repo|url> [<directory>] [-- <git-flags>...]`

Clone a repository

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli repo clone <workspace/repo|url> [<directory>] [-- <git-flags>...]`

Flags: `--protocol` — Clone protocol: https or ssh (defaults to config)

## `repo create <name>`

Create a repository

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli repo create <name>`
- JSON fields: `uuid,full_name,name,is_private,mainbranch,links`

Flags: `--clone` — Clone after creation; `--description` — Repository description; `--directory` — Directory for --clone; `--main-branch` — Main branch name; `--private` — Make the repository private; `--project` — Project key; `--protocol` — Clone protocol: https or ssh (defaults to config); `--remote-name` — Remote name used by --source; `--source` — Existing local git repository to push after creation

## `repo delete [<workspace/repo>]`

Delete a repository

- Classification: **delete**
- Required scopes: `delete:repository:bitbucket`
- Example: `bitbucket-cli repo delete [<workspace/repo>]`

Flags: `--yes` — Confirm deletion

## `repo edit [<workspace/repo>]`

Edit a repository

- Classification: **admin**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli repo edit [<workspace/repo>]`
- JSON fields: `uuid,full_name,name,is_private,mainbranch,links`

Flags: `--description` — New repository description; `--fork-policy` — Fork policy: allow_forks, no_public_forks, or no_forks; `--has-issues` — Enable or disable issue tracking; `--has-wiki` — Enable or disable wiki; `--main-branch` — New main branch name; `--name` — New repository name; `--private` — Set private visibility

## `repo fork [<workspace/repo>]`

Fork a repository

- Classification: **write**
- Required scopes: `read:repository:bitbucket, write:repository:bitbucket`
- Example: `bitbucket-cli repo fork [<workspace/repo>]`
- JSON fields: `uuid,full_name,name,is_private,mainbranch,links`

Flags: `--clone` — Clone the fork after creation; `--directory` — Directory for --clone; `--name` — Fork name; `--protocol` — Clone protocol: https or ssh (defaults to config); `--remote-name` — Remote name for the fork; `--remote` — Configure the fork as a git remote; `--workspace` — Target workspace; `--yes` — Confirm remote changes

## `repo list [<workspace>]`

List repositories in a workspace

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli repo list [<workspace>]`
- JSON fields: `uuid,full_name,name,is_private,mainbranch,links`

Flags: `--fork` — Only fork repositories; `--limit` — Maximum repositories to return; `--private` — Only private repositories; `--project` — Filter by project key; `--public` — Only public repositories; `--query` — Bitbucket q expression; `--role` — Repository role: owner, member, or contributor; `--sort` — Sort field, optionally prefixed with -; `--source` — Only non-fork repositories

## `repo set-default [<workspace/repo>]`

Set the local repository default

- Classification: **write**
- Required scopes: `none (local git configuration)`
- Example: `bitbucket-cli repo set-default [<workspace/repo>]`

Flags: `--value` — Repository selector (normally use the positional argument)

## `repo view [<workspace/repo>]`

View a repository

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli repo view [<workspace/repo>]`
- JSON fields: `uuid,full_name,name,is_private,mainbranch,links`

Flags: `--branch` — Branch or ref used for --readme; `--readme` — Print the repository README; `--web` — Open the repository in a browser

## `report`

Manage Code Insights reports

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli report`
- JSON fields: `uuid,title,details,result,reporter,report_type,data,created_on,updated_on`

## `report delete <commit> <report-id>`

Delete a Code Insights report

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli report delete <commit> <report-id>`
- JSON fields: `uuid,title,details,result,reporter,report_type,data,created_on,updated_on`

Flags: `--yes` — Confirm deletion

## `report list <commit>`

List reports for a commit

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli report list <commit>`
- JSON fields: `uuid,title,details,result,reporter,report_type,data,created_on,updated_on`

Flags: `--limit` — Maximum reports to return

## `report upsert <commit> <report-id>`

Create or update a Code Insights report

- Classification: **write**
- Required scopes: `read:repository:bitbucket and write:repository:bitbucket`
- Example: `bitbucket-cli report upsert <commit> <report-id>`

> Warning: report payloads and annotations are visible to repository users with access; never include secrets.
- JSON fields: `uuid,title,details,result,reporter,report_type,data,created_on,updated_on`

Flags: `--data-file` — JSON report fields; `--details` — Report details; `--result` — Result: PASSED, FAILED, or PENDING; `--title` — Report title

## `report view <commit> <report-id>`

View a Code Insights report

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli report view <commit> <report-id>`
- JSON fields: `uuid,title,details,result,reporter,report_type,data,created_on,updated_on`

## `ssh-key`

Manage account SSH keys

- Classification: **read**
- Required scopes: `read:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

## `ssh-key add --file <public-key>`

Add an account SSH key

- Classification: **write**
- Required scopes: `write:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key add --file <public-key>`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--expires` — Expiry date (YYYY-MM-DD) or RFC3339; `--file` — OpenSSH public-key file, or - for stdin (required); `--label` — Key label; `--user` — Unsupported for writes; account SSH keys default to the authenticated user

## `ssh-key delete <id> --yes`

Delete an account SSH key

- Classification: **write**
- Required scopes: `write:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key delete <id> --yes`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--user` — Unsupported for writes; omit this flag; `--yes` — Confirm deletion

## `ssh-key edit <id>`

Edit an account SSH key

- Classification: **write**
- Required scopes: `write:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key edit <id>`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--expires` — New expiry date; `--label` — New key label; `--user` — Unsupported for writes; omit this flag; `--yes` — Reserved for compatibility

## `ssh-key list`

List account SSH keys

- Classification: **read**
- Required scopes: `read:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key list`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--limit` — Maximum keys to return; `--user` — User selector for read operations (defaults to the authenticated user)

## `ssh-key view <id>`

View an account SSH key

- Classification: **read**
- Required scopes: `read:ssh-key:bitbucket`
- Example: `bitbucket-cli ssh-key view <id>`
- JSON fields: `id,label,algorithm,fingerprint,created_on,last_used,expires_at,scope`

Flags: `--user` — User selector for read operations

## `status`

Check bitbucket-cli configuration

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli status`

## `tag`

Tag commands

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli tag`

## `tag create <name> --target <commit|branch>`

Create a tag

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli tag create <name> --target <commit|branch>`

Flags: `--message` — Annotated tag message (Bitbucket creates an annotated tag and supplies a default when omitted); `--target` — Commit hash or branch to point at

## `tag delete <name> --yes`

Delete a tag

- Classification: **write**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli tag delete <name> --yes`

Flags: `--yes` — Confirm deletion

## `tag list`

List tags in a repository

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli tag list`
- JSON fields: `name,target,message,links,requested_target,resolved_target`

Flags: `--limit` — Maximum tags to return; `--query` — Bitbucket q expression; `--sort` — Sort field, optionally prefixed with -

## `tag view <name>`

View a tag

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli tag view <name>`
- JSON fields: `name,target,message,links,requested_target,resolved_target`

## `webhook`

Manage Bitbucket webhooks

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli webhook`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

## `webhook apply --file <yaml|json>`

Apply declarative webhook configuration

- Classification: **write**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli webhook apply --file <yaml|json>`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--allow-insecure-localhost` — Allow http://localhost destinations; `--allow-private` — Confirm non-public webhook destinations; `--allow-unknown-event` — Skip event catalog validation; `--dry-run` — Report operations without changing Bitbucket; `--file` — YAML or JSON configuration file (required); `--no-event-cache` — Do not read or write the event catalog cache; `--prune` — Delete hooks absent from the file; `--repository` — Repository selector (workspace/repo or URL); `--workspace` — Workspace slug or UUID; `--yes` — Confirm pruning

## `webhook create --url <https-url> --event <key>...`

Create a webhook

- Classification: **write**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli webhook create --url <https-url> --event <key>...`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--active-set` — Send the active value (including false); `--active` — Whether the webhook is active; `--allow-insecure-localhost` — Allow http://localhost destinations; `--allow-private` — Confirm non-public webhook destinations; `--allow-unknown-event` — Skip event catalog validation; `--description` — Webhook description; `--event` — Webhook event key (repeatable); `--no-event-cache` — Do not read or write the event catalog cache; `--repository` — Repository selector (workspace/repo or URL); `--secret-env` — Read the secret from this environment-variable name; `--secret-prompt` — Read the secret from a hidden TTY prompt; `--secret-stdin` — Read the secret from stdin; never pass it as an argument; `--url` — HTTPS webhook URL (required); `--workspace` — Workspace slug or UUID

## `webhook delete <uuid> --yes`

Delete a webhook

- Classification: **write**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli webhook delete <uuid> --yes`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--repository` — Repository selector (workspace/repo or URL); `--workspace` — Workspace slug or UUID; `--yes` — Confirm deletion

## `webhook edit <uuid>`

Edit a webhook

- Classification: **write**
- Required scopes: `admin:repository:bitbucket`
- Example: `bitbucket-cli webhook edit <uuid>`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--active` — Whether the webhook is active; `--allow-insecure-localhost` — Allow http://localhost destinations; `--allow-private` — Confirm non-public webhook destinations; `--allow-unknown-event` — Skip event catalog validation; `--description` — New webhook description; `--event` — Replace webhook event keys (repeatable); `--no-event-cache` — Do not read or write the event catalog cache; `--repository` — Repository selector (workspace/repo or URL); `--secret-env` — Read a replacement secret from this environment-variable name; `--secret-prompt` — Read a replacement secret from a hidden TTY prompt; `--secret-stdin` — Read a replacement secret from stdin; `--url` — New HTTPS webhook URL; `--workspace` — Workspace slug or UUID

## `webhook events [subject]`

List webhook event keys

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli webhook events [subject]`
- JSON fields: `event`

Flags: `--no-event-cache` — Do not read or write the event catalog cache; `--subject` — Catalog subject: repository, workspace, or user

## `webhook export --output <file>`

Export webhooks as declarative YAML or JSON

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli webhook export --output <file>`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--output` — Output YAML or JSON file (required); `--repository` — Repository selector (workspace/repo or URL); `--workspace` — Workspace slug or UUID

## `webhook list`

List webhooks

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli webhook list`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--limit` — Maximum webhooks to return; `--repository` — Repository selector (workspace/repo or URL); `--workspace` — Workspace slug or UUID

## `webhook view <uuid>`

View a webhook

- Classification: **read**
- Required scopes: `read:repository:bitbucket`
- Example: `bitbucket-cli webhook view <uuid>`
- JSON fields: `uuid,description,url,active,events,created_at,updated_at,subject`

Flags: `--repository` — Repository selector (workspace/repo or URL); `--workspace` — Workspace slug or UUID

## `workspace`

Workspace and membership commands

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace`
- JSON fields: `name,slug,uuid,links,members,projects`

## `workspace invite <email>`

Invite a workspace member

- Classification: **write**
- Required scopes: `admin:workspace:bitbucket`
- Example: `bitbucket-cli workspace invite <email>`
- JSON fields: `name,slug,uuid,links,members,projects`

Flags: `--group` — Workspace group slug (unsupported until invitations are exposed by the API); `--permission` — Workspace permission: read, write, or admin

## `workspace list`

List workspaces

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace list`
- JSON fields: `name,slug,uuid,links,members,projects`

Flags: `--limit` — Maximum workspaces to return; `--query` — Bitbucket q expression; `--role` — Workspace role: member, collaborator, or owner

## `workspace member`

Workspace member commands

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace member`
- JSON fields: `name,slug,uuid,links,members,projects`

## `workspace member view <user-selector>`

View a workspace member

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace member view <user-selector>`
- JSON fields: `name,slug,uuid,links,members,projects`

## `workspace members [<workspace>]`

List workspace members

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace members [<workspace>]`
- JSON fields: `name,slug,uuid,links,members,projects`

Flags: `--limit` — Maximum members to return; `--query` — Bitbucket q expression

## `workspace remove-member <user-selector> --yes`

Remove a workspace member

- Classification: **write**
- Required scopes: `admin:workspace:bitbucket`
- Example: `bitbucket-cli workspace remove-member <user-selector> --yes`
- JSON fields: `name,slug,uuid,links,members,projects`

Flags: `--yes` — Confirm member removal

## `workspace view [<workspace>]`

View a workspace

- Classification: **read**
- Required scopes: `read:workspace:bitbucket`
- Example: `bitbucket-cli workspace view [<workspace>]`
- JSON fields: `name,slug,uuid,links,members,projects`

Flags: `--members` — Include workspace members; `--projects` — Include workspace projects; `--web` — Open the workspace in a browser
