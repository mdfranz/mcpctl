# mcpctl — manage and validate project MCP servers

## Context

The user maintains MCP server definitions separately across Claude Code's `.mcp.json`, Codex CLI's `.codex/config.toml`, and OpenCode's `opencode.json`. The same logical server is duplicated across formats, with different environment-variable and authentication conventions.

`mcpctl` will be a lightweight Go CLI/TUI (Bubble Tea) that edits one canonical definition and synchronizes its supported representation across all three project files. It will also invoke the clients' own MCP commands to report configuration, connection, and authentication status, and delegate interactive authentication to the selected client.

Project files remain the source of configuration; there is no fourth canonical config file in the MVP. Writes remain project-scoped. Client commands may read effective configuration and credentials from other scopes; diagnostics must distinguish that context from the project files mcpctl manages.

## Implementation status (2026-09-22)

Repo: https://github.com/mdfranz/mcpctl (public), branch `main`. `go build ./...`, `go vet ./...`, and `go test ./...` pass; `make install` puts `mcpctl` on `PATH` at `~/.local/bin`.

### Built and tested

- **`internal/config`** — canonical `Server`/`EnvVar`/`HeaderValue` model; load/parse adapters for all three clients (unmodeled fields preserved as `Extras`, unloadable entries reported as `Unsupported` rather than dropped); a surgical byte-preserving Codex TOML editor (`codex_block.go`/`codex_render.go`) that only rewrites a server's own table spans; merge-preserving Claude/OpenCode JSON writers; `BuildPlan`/`Apply` with pre-write hash-recheck, atomic per-file writes, rollback on partial failure, and a project lock file.
- **`internal/client` + `internal/status`** — bounded, shell-safe subprocess runner; per-client parsers for `claude`/`codex`/`opencode mcp list`/`get` output, validated against real captured CLI output (fixtures in `testdata/clients/`); `status.Check` orchestrates all three concurrently and cross-references live results against each client's own project file so an entry present in the file but not reported live is flagged, not silently dropped.
- **`internal/status/doctor.go`** — local preflight only (no live `mcp list`/`get`, no server launches): client availability, config syntax, stdio executable resolution, env var presence (never values — tested explicitly), remote URL sanity, cross-client divergent-definition findings.
- **`internal/tui`** — Bubble Tea app: server list with per-client status columns, detail view, and `a`/`e`/`d` add/edit/delete flows through a diff preview before `Apply` (reusing the same write path, not a separate one). Compact `NAME=value` / `NAME=$` / `NAME=$:default` grammar for env/header form fields, with escaping and round-trip tests.
- **CLI**: `mcpctl status [--json] [--dir] [--timeout]`, `mcpctl doctor [--json] [--dir]`, `mcpctl` / `mcpctl tui` (no args launches the TUI, per the CLI surface below).
- **`Makefile`**: `build`, `test`, `vet`, `fmt`/`fmt-check`, `install`/`uninstall`, `run`, `check`.

### Confirmed or corrected against real CLI behavior during implementation

- Codex CLI *does* honor a project-scoped `.codex/config.toml` as this plan assumes — but only for **trusted projects** (tracked in a `[projects."<path>"]` table in `~/.codex/config.toml`, and it extends to subdirectories). An untrusted project silently gets the global-only view, which looks identical to "no project config" unless you know to check trust. `doctor`/`status` don't yet surface trust state explicitly — worth adding.
- Claude Code's failure glyph is `✘` (U+2718 HEAVY BALLOT X), not `✗` (U+2717) as first guessed; corrected after a real project surfaced it. The "Contains(Failed)" text fallback is what actually caught it before the glyph fix landed.
- Codex's `mcp list`/`get` report configuration only, no live connectivity (`Connection` is always `unchecked` for Codex), and expose no scope/origin field, so `status` infers project attribution by matching the reported command/URL against the project's own file rather than trusting a client-reported scope.
- OpenCode's `mcp list` output is an ANSI box-drawing UI, not structured data or a flag-selectable format; parsed after stripping SGR escape sequences.

### Deliberately not built yet (gaps against this plan)

- `mcpctl sync` (dry-run/apply) and `mcpctl add`/`edit`/`delete` as direct CLI subcommands — the write path exists and is exercised by the TUI, but there's no non-interactive CLI entry point yet.
- A dedicated conflict-resolution picker (`conflicts.go`) — divergent per-client definitions currently surface via the diff preview and via `doctor`'s pairwise-diff conflict findings, not an interactive per-field source-selection UI. `BuildPlan` takes the desired canonical state directly from the caller; it doesn't itself compute the Claude>OpenCode>Codex precedence merge.
- The TUI's `l` auth/login client-picker handoff.
- User-visible backup history/restore (Phase 2) and tool discovery/listing (Phase 3).
- `status`'s "connected" classification for Claude/OpenCode is still provisional — not yet observed against a genuinely working, successfully-connected server in the wild.
- TOML comments *inside* a table being rewritten aren't preserved (only the surrounding untouched document is byte-exact); Claude/OpenCode JSON writes preserve unknown values semantically, not original byte formatting.

## Architecture decisions

1. **Full resync with preview**: every successful add/edit/delete renders the complete current project server set for all three clients. Preview changes before applying them. A standalone `sync` command supports dry runs. Resolve actual conflicts before writing; precedence supplies a suggested choice, not automatic authorization to overwrite divergent definitions.
2. **Preserve client-specific settings**: project canonical fields into each format while retaining unmodeled per-server fields in their original client. Do not copy extras between clients. Deleting a server intentionally removes its entire definition. Unsupported or ambiguous syntax blocks saving rather than being discarded.
3. **Surgical TOML edits**: use `pelletier/go-toml/v2` for semantic reads and syntax-aware source spans for edits. Preserve untouched comments and content exactly. Do not depend on a regex-only table boundary finder.
4. **Explicit portability limits**: model literal and passthrough env vars, including optional defaults. Claude renders `${VAR}` / `${VAR:-default}`; OpenCode renders `{env:VAR}`; Codex uses `env_vars` and an `env` table. Where defaults cannot be represented, show a portability warning before saving. Expected conversion loss is not drift.
5. **Separate runtime evidence**: configuration, connectivity, authentication, and check execution are separate dimensions. Config presence or command exit success alone must never imply a working authenticated connection.
6. **Client-owned authentication**: use each client's login flow and credential store. Never synchronize OAuth tokens into project files or assume login in one client authenticates another. Status refresh never intentionally starts an interactive login flow.
7. **Lightweight implementation**: stdlib `flag`, `os/exec`, and `context`; no Cobra/Viper or MCP SDK required for the MVP. No args launches the TUI in the current directory.

## CLI surface

```sh
mcpctl                                      # TUI
mcpctl status                               # all project servers, all clients
mcpctl status my-server --client claude
mcpctl status --json
mcpctl auth my-server --client codex
mcpctl doctor
mcpctl doctor --json
mcpctl sync --dry-run
mcpctl sync                                 # preview and confirm in a terminal
mcpctl sync --yes                            # explicit noninteractive apply
```

Support `--dir PATH` for project selection and `--timeout DURATION` for status checks. Parse flags consistently before or after positional arguments in the documented examples; stdlib `flag` needs explicit handling for this. Keep `--help` and `--version` available.

`status` never rewrites managed configuration. Native checks can launch stdio servers, contact endpoints, or refresh credentials through the client, so describe them as active checks. `doctor` is local preflight by default and does not launch MCP servers or login flows.

Define stable exit codes: `0` means no detected issues in the requested check, `1` means drift or a diagnosed configuration/connection/authentication problem, and `2` means invalid invocation or an incomplete check (missing client, timeout, unsupported output, execution failure). Incomplete checks take precedence over diagnosed issues. Unknown authentication alone is not failure when the client cannot expose it; JSON records coverage and unchecked dimensions explicitly. `sync --dry-run` returns `1` when changes are pending. Noninteractive `sync` requires `--yes`; unresolved conflicts still block apply.

## Client status and authentication adapters (MVP)

Commands below were checked against installed CLI help during the plan review on 2026-09-22. Detect capabilities at runtime; do not assume every installed version supports the same commands or output.

| Client | Inspection | Authentication | Limitations |
| --- | --- | --- | --- |
| Codex | `codex mcp list --json`; `codex mcp get NAME --json` for details | `codex mcp login NAME` | List/get inspect configuration; do not treat them as live MCP connection tests. Use auth evidence only if output actually supplies it. |
| Claude | `claude mcp list`; `claude mcp get NAME` for details and servers absent from list | `claude mcp login NAME` when supported | Distinguish connected, needs authentication, failed, disabled, rejected, and pending project approval. Older clients may require interactive `/mcp`. |
| OpenCode | `opencode mcp list`; `opencode mcp auth list` | `opencode mcp auth NAME` | Combine connection and OAuth evidence. Keep `opencode mcp debug NAME` as an explicit diagnostic action rather than routine refresh. |

Implementation requirements:

- Discover binaries and versions, then probe supported command options through help. Missing clients appear individually as unavailable; they do not prevent checking other clients.
- Run commands with an argv array, never shell interpolation, and set the working directory to the selected project. Use the inherited environment without printing secrets.
- Prefer machine-readable output when supported. Use versioned text fixtures otherwise; strip ANSI sequences and treat unrecognized output as an incomplete check, not as success or unauthenticated.
- Use context deadlines, bounded concurrency, output limits, and cancellation that cleans up spawned processes. Status commands receive no interactive stdin. Bubble Tea updates asynchronously, with per-client progress and cancellation.
- Batch list commands once per client; issue targeted detail commands only as needed. Parse per-server results even when the process exits successfully: a list command can succeed while individual servers fail.
- Compare the observed name and command/URL with the project definition to avoid attributing a shadowing global server's success to the project server. Show source/scope when exposed by the client; otherwise label project attribution as unverified.
- Retain timestamp, client/version, command, exit code, duration, and a bounded redacted diagnostic. Mask credentials, headers, env values, and sensitive URL components before display or JSON output; do not persist raw subprocess output by default.
- Mark results stale after config edits, reloads, or authentication. Recheck after explicit login completes; successful login is not itself proof of connection health.
- In the TUI, suspend rendering and hand the terminal to interactive login, then restore the UI. Noninteractive auth requests return an actionable instruction. Do not change workspace trust/approval decisions automatically.

### Status model

One result per server per client, kept outside the configuration model:

```go
type StatusResult struct {
    ServerName    string
    Client        string
    ClientVersion string
    ConfigState   string // present | missing | disabled | pending_approval | rejected | unknown
    Connection    string // connected | failed | timed_out | unchecked
    AuthState     string // authenticated | required | not_required | unknown
    AuthMethod    string // oauth | bearer | headers | none | unknown
    CheckState    string // complete | unavailable | unsupported | timed_out | failed | cancelled
    Scope         string // project | other | unknown
    CheckedAt     time.Time
    Duration      time.Duration
    Evidence      []Evidence // command, exit code, redacted summary, observed config identity
}
```

Use typed enums in implementation. A token reference or stored credential is evidence of configuration, not proof that the credential is valid. Connected does not necessarily mean authenticated: servers may accept anonymous sessions. Report `not_required` only with evidence; do not infer it from an absent OAuth record. A network failure must not be classified as an authentication failure without supporting output. Codex connectivity may remain `unchecked` in the MVP; a completed configuration check does not imply all dimensions were checked.

## Local preflight (`doctor`, MVP)

- Check client availability/version and supported status/auth commands.
- Parse project files and report invalid syntax, unsupported layouts, portability warnings, and unresolved conflicts.
- Check stdio executable availability and referenced env-var presence without displaying values. Respect known working-directory and default semantics; distinguish absent from empty variables.
- Validate remote URL and header configuration locally. Do not claim a present API key works or that an executable check proves a server can initialize.
- Report actionable next steps, such as installing a client, setting a missing variable, refreshing status, logging in, or resolving a client-reported approval blocker. Approval state is available only when observed; do not invent it from file presence.

## Canonical configuration and merge

```go
type EnvVar struct {
    Name    string
    Kind    EnvVarKind // literal | passthrough
    Value   string     // literal
    Default *string    // passthrough; nil means no default, distinct from empty default
}

type HeaderValue struct {
    Kind    EnvVarKind // literal | passthrough
    Value   string     // literal
    EnvName string     // passthrough
}

type Server struct {
    Name             string // new names: ^[A-Za-z0-9_-]+$
    Type             ServerType // stdio | remote
    Command          string
    Args             []string
    Env              []EnvVar // ordered, deterministic rendering
    URL              string
    Transport        string // preserve HTTP/SSE distinctions; reject unsupported conversions
    Headers          map[string]HeaderValue
    BearerTokenEnvVar string // environment variable name, never a resolved token
}
```

Codex supports `http_headers`, `env_http_headers`, and `bearer_token_env_var`; do not drop remote headers. Map literal/env-backed headers through the clients' supported representations. Translate bearer env references to an Authorization header only when the target format can express equivalent behavior. Reject collisions between an explicit Authorization header and bearer configuration, or resolve them explicitly before saving. Report unsupported conversions before apply.

Maintain per-client source metadata and opaque extras outside `Server`. Preserve fields such as timeouts, OAuth options, enabled state, tool filters, and other unmodeled settings in their original format. Do not copy client-specific credentials or authentication metadata into another client. Unsupported transport types or existing server names remain visible and must not be silently renamed or removed.

`Load(dir)` returns a snapshot containing canonical candidates, original documents and hashes, source metadata, client-specific extras, structured conflicts, and portability warnings. Proposed source precedence remains **Claude > OpenCode > Codex**, favoring retention of env defaults, but users can choose a source per conflicting server in the TUI. No client is universally authoritative for every field.

Drift compares actual client settings with the expected projection of the selected canonical definition. A default intentionally omitted in Codex/OpenCode generates a portability warning, not a recurring conflict after every save. Compare maps and env collections semantically rather than relying on order. Detect missing server definitions as pending sync changes.

If a user selects a less expressive source, preview any resulting loss of defaults or other information. Because there is no separate canonical file, deleting the only source that retains such information loses it; surface that limitation instead of inventing values on reload.

## Safe sync and format preservation

Split save into `Load` → `Plan` → `Apply`. The plan contains projected output for all three files, redacted diffs, portability warnings, deletions, and required conflict resolutions. The same plan powers TUI preview and `sync --dry-run`.

Before apply, render and parse all outputs and verify input hashes still match the snapshot. Refuse to overwrite concurrent edits. Coordinate concurrent mcpctl writers with a project lock; external editors do not honor that lock, so recheck inputs immediately before replacement and document that the race cannot be completely eliminated. Stage temp files in their destination directories, preserve existing permissions, and use restrictive permissions for newly created files containing literal secrets.

Use per-file atomic rename, but do not describe the three-file operation as atomic. Retain originals for recovery; on partial failure attempt rollback without overwriting intervening external changes, and report exactly which files were updated/restored and how to recover. Remove staging artifacts after successful completion. A user-facing backup history and restore command come in a later phase; failure recovery is MVP.

### Codex TOML

- Parse existing TOML before editing. Locate owned keys and tables with syntax-aware spans accounting for quoted keys, indentation, comments, multiline strings, and nested tables. The new-name restriction does not constrain formatting in existing files.
- Update owned values while preserving unmodeled keys and unrelated bytes. Avoid full replacement of a server table when it would discard client-specific content.
- Track all tables belonging to a server, including separated nested tables, before deleting it. Do not assume a server occupies one contiguous block.
- If the MVP editor cannot safely handle a valid layout (for example inline/dotted definitions), report its location and refuse the entire save before any write.
- Add `.codex/` and `config.toml` when needed. Use deterministic rendering for generated values. Normalize only inserted/replaced separators, not blank-line runs throughout untouched content.
- Require idempotent output and byte-for-byte preservation outside changed spans. Validate the resulting document with the TOML parser before apply.

### Claude and OpenCode JSON

Merge both top-level objects and individual server objects using `json.RawMessage`; overwrite owned fields and retain unknown fields. Remove obsolete owned fields on type changes without leaving contradictory command/URL settings; flag incompatible client-specific extras for resolution.

Indented JSON serialization preserves unknown values semantically, not necessarily their original bytes or formatting. Test that distinction. Diagnose unsupported JSONC or alternate active configuration rather than silently creating a competing JSON file. Block ambiguous saves until an adapter supports the layout; JSONC editing is outside the initial writer scope.

## TUI

The list shows each project server with per-client status columns, drift/portability indicators, and last-check age. Detail shows the separate status dimensions, configuration source, redacted evidence, and next action.

- `a` add, `e` edit, `d` delete, `enter` detail.
- `r` reload files; `s` refresh status; `l` authenticate using a client picker; `q` quit/cancel work.
- Add/edit/delete flows proceed through conflict resolution if needed, then a diff preview before apply. Deletion is explicitly confirmed in the preview; avoid redundant confirmations.
- Form supports literal vs passthrough env/header values explicitly. If retaining the compact line grammar (`NAME=value`, `NAME=$passthrough`, `NAME=$passthrough:default`), define escaping for literal dollar-prefixed values and delimiters, and test it. Headers need their own literal/env distinction.
- Renaming remains delete+add in the MVP; warn that client-owned authentication associated with the old name may need to be re-established.
- All client commands run asynchronously except intentional interactive login handoff. A slow/unavailable client must not freeze the UI.

## Package layout

```text
mcpctl/
  cmd/mcpctl/main.go        # TUI entry; status/auth/doctor/sync flags and exit codes
  internal/config/
    model.go               # canonical model, source snapshots, extras, conflicts
    store.go               # load and projection-aware merge
    plan.go                # render, validate, diff, conflict resolutions
    apply.go               # locking, staging, hash checks, replacement and recovery
    claude.go
    opencode.go
    codex.go
    codex_block.go         # syntax-aware spans and surgical edits
    codex_render.go
    names.go
  internal/client/
    client.go              # adapter interface and capability discovery
    runner.go              # argv execution, cwd, deadlines, bounded output, cancellation
    codex.go
    claude.go
    opencode.go
  internal/status/
    model.go               # per-client evidence and independent status dimensions
    check.go               # orchestration, reconciliation with project definitions
    doctor.go              # local preflight
  internal/redact/          # shared diagnostic, JSON, and diff redaction
  internal/tui/
    app.go
    list.go
    detail.go
    form.go
    preview.go
    conflicts.go
    confirm.go
    styles.go
  testdata/
    config/                # preservation, portability, unsupported-layout fixtures
    clients/               # sanitized CLI outputs by version, including failures
```

Dependencies: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/bubbles`, `github.com/charmbracelet/lipgloss`, and `github.com/pelletier/go-toml/v2`. Prefer stdlib for everything else. Validate source-span support early; introduce an additional TOML editing dependency only if needed for correct preservation.

## Delivery phases

1. **MVP — usable and reviewable**: safe configuration merge/write, client-specific preservation, conflict resolution, sync preview/dry run, status/auth adapters, local doctor, JSON diagnostics, and responsive TUI. Implement status adapters, format preservation, and preview early to validate the riskiest assumptions.
2. **Next — daily operation**: effective configuration/source view with scope and shadowing explanations; per-client enable/disable without deleting definitions; user-visible backup history and restore. MVP already labels uncertain scope and preserves existing enabled settings.
3. **Later — discovery**: list exposed tools where the client supports it, without invoking them; consider deeper MCP initialization checks for clients whose CLI cannot report connectivity. Keep this separate from claims made by MVP status.

## Verification and acceptance criteria

### Configuration and sync

- Round-trip literals, passthrough variables, empty/nonempty defaults, literal/env-backed headers, and bearer env references with per-client projection assertions.
- Save/reload a default-bearing server: no false drift, but a visible portability warning where defaults are omitted.
- Preserve unknown top-level and per-server JSON values semantically; preserve untouched TOML spans byte-for-byte, including client-specific OAuth/timeouts/tool settings.
- TOML fixtures cover quoted keys, indentation, inline comments, multiline strings containing bracket-like text, separated nested tables, sibling names (`foo` vs `foo-bar`), insertion/deletion, and idempotency. Unsupported layouts fail before any file is written.
- Test missing/malformed files, unsupported transports/names, conflicting Authorization settings, source selection, and concurrent edits. Preview and apply must use the same rendered plan.
- Inject staging/rename/rollback failures and verify partial-write reporting and recoverability. Verify permissions and that rollback does not overwrite later external edits.
- Redaction tests ensure diff previews and diagnostics never expose literal secrets or resolved env values.

### Client adapters and status

- Use fake executables and sanitized output fixtures; routine tests require no real servers, network access, or credentials.
- Cover JSON and text outputs, ANSI formatting, unknown output, missing clients, unsupported commands, timeouts, cancellation, output limits, and nonzero exits.
- Cover mixed per-server success/failure with process exit zero, pending/rejected approval, disabled servers, and project/global name collisions.
- Assert configured/token-present does not imply connected/authenticated, network failure does not imply login required, and unsupported Codex connectivity stays unchecked.
- Assert correct argv and cwd, bounded concurrent checks, subprocess cleanup, no interactive prompts during status, and no managed config changes from status/doctor.
- Auth tests verify explicit client selection, terminal handoff, cancellation, status invalidation/recheck, and no credential copying.
- CLI tests cover documented flag placement, JSON schema/coverage, exit codes, and noninteractive sync behavior.

### Manual end-to-end

In a scratch project, add a stdio server with literal/passthrough/default env vars, preview and apply, then reload without false drift. Introduce a real conflicting value and resolve it explicitly. Add a remote server with headers and confirm all supported projections, including Codex. Preserve client-specific fields through unrelated edits. Delete a server and verify removal of its nested tables only.

Use controlled local test servers to exercise connected, anonymous, auth-required, failed, and timed-out states. Check missing-client and project-approval behavior. Complete an explicit OAuth login where available and verify subsequent status independently. Confirm the TUI remains responsive and secrets do not appear in diagnostics.

Implementation completion requires `go build ./...`, `go vet ./...`, and `go test ./...` passing. These are planned checks, not a claim that implementation exists or has passed them.

## References

Reviewed 2026-09-22; runtime CLI capability detection remains required.

- [Official OpenAI MCP documentation](https://developers.openai.com/codex/mcp): configuration, headers, env-backed bearer tokens, and OAuth login.
- [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp): list/get status, project approval, and CLI authentication.
- [OpenCode MCP documentation](https://opencode.ai/docs/mcp-servers/): connection status, OAuth auth list, and explicit debug flow.
