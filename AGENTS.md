# AGENTS.md — bitbucket-cli

Standalone Go CLI for Bitbucket Cloud (PRs, branches, repo info). A command-line
port of the Pi `bitbucket` agent extension so any agent/script can drive Bitbucket
without the Pi runtime.

## Layout

| Path | Responsibility |
| --- | --- |
| `main.go` | Entry point → `cmd.Execute()` |
| `cmd/` | Cobra commands. One file per area: `pr.go` (read), `pr_comment.go` + `pr_write.go` + `pr_attach.go` (write), `branch.go`, `repo.go`, `status.go`, `config.go`, `auth.go`, `root.go`. `common.go` holds shared helpers. |
| `bitbucket/client.go` | Thin REST 2.0 client: provider-based auth, JSON `Request`, multipart `UploadFiles`, `Paginate`, normalized `HTTPError`, 403 scope hints, token redaction. |
| `auth/` | Credential abstraction: `Provider` (Basic/Bearer), `SecretStore` (platform backends: macOS Keychain via `security`, Linux Secret Service via `secret-tool`, `UnsupportedStore` fallback; `MemoryStore` for tests), `ErrStoreUnavailable` sentinel, `AvailabilityStore.Available()` preflight, token types. |
| `config/config.go` | Config resolution: env → YAML file → git-remote auto-detect, keychain-backed token lookup, legacy plaintext migration. |
| `output/` | `RenderJSON`/`RenderLines`/`WriteError` and `*Summary` text formatters. |
| `docs/` | Astro/Starlight static documentation site. |

## Conventions

- **Output contract**: JSON on stdout by default; `--pretty` emits one-line text
  summaries (`output.*Summary`). Errors go to stderr as a JSON envelope, exit 1.
  Use `emitObject` / `emitList` (in `cmd/common.go`) so every command renders the
  same way.
- **Read vs write commands**: write commands (`pr comment`, `pr attach`,
  `pr create`, `pr update`) must carry a `Long` description stating they are write
  operations to run only when the user explicitly asked. This is a deliberate safety
  marker for agents.
- **HTTP**: no per-command HTTP code. Call `client.Request(ctx, path,
  bitbucket.RequestOptions{Method, Body}, &out)` for JSON APIs — it supports
  GET/POST/PUT with a JSON body and returns `*HTTPError` for non-2xx (surfaces
  method/url/status/excerpt). For multipart uploads, call `client.UploadFiles(ctx,
  path, "files", uploads, &out)` so auth and error normalization stay centralized.
- **Shared helpers** (`cmd/common.go`): `newClient`, `resolveRepo` (→ `/repositories/{ws}/{repo}` base path), `parseID`, `ctx`, `fail`, `emitObject`, `emitList`.
- **Body input** (`cmd/pr_write.go`): `readBody(text, file, stdin)` resolves a body
  from `--description` / `--description-file` (`-` = stdin); the two sources are
  mutually exclusive. Reuse it for any future text-body flag.
- **PR attachments** (`cmd/pr_attach.go`): Bitbucket Cloud has no native PR
  attachment API. `pr attach` uploads local files to repository Downloads, then posts
  a PR comment with canonical `https://bitbucket.org/{workspace}/{repo}/downloads/{file}`
  links. Downloads artifacts with the same filename are replaced by Bitbucket; the
  command rejects duplicate basenames within one invocation.

## PR update rule (important)

Bitbucket's `PUT /pullrequests/{id}` requires `title` and will **clear reviewers**
if they are omitted. `pr update` therefore does a **read-modify-write**: GET the PR,
carry over `title` (unless changed) and `reviewers`, then PUT only with the changed
fields added. Preserve this behavior for any future PR mutation.

## Testing

- `go test ./...`. Tests stub HTTP via `testTransport` (a `roundTripFunc`) injected
  by the `run(t, transport, args...)` helper in `cmd/commands_test.go`.
- `run` resets global + per-command flag state (`resetFlags`) because cobra reuses
  the `rootCmd` singleton across `Execute()` calls — flag values and `Changed`
  markers leak between tests otherwise. Slice flags such as `pr attach --file` may
  also need their backing package variables reset explicitly.
- Tests are sandboxed from the developer's environment: `run`/`runAt` set a
  temporary `BITBUCKET_CONFIG` and inject an in-memory `auth.SecretStore`
  (`auth.SetGlobalStore`), so the real `~/.config/bitbucket-cli.yaml` and macOS
  Keychain are never touched.

## Build

`go build -o bitbucket-cli .` or `go install .`. Deps: `spf13/cobra`,
`gopkg.in/yaml.v3` only. Release via goreleaser (`.goreleaser.yaml`).

The docs site uses npm from `docs/` and emits static output to `docs/dist/`:
`make docs-build`. Cloudflare Pages uses root `docs`, build command
`npm run build`, and output `dist`.
