# AGENTS.md — MatrixReq CLI (`mxreq`)

> Reference for AI agents working on this codebase. Covers architecture, conventions, patterns, and common tasks.

## Project Overview

**Binary:** `mxreq`
**Module:** `github.com/VladGavrila/matrixreq-cli`
**Go version:** 1.23+
**Direct dependencies:** cobra, viper, lipgloss (charmbracelet)
**Purpose:** CLI for the MatrixALM/MatrixQMS REST API v2.5

## Directory Structure

```
├── main.go                    # Entry point → cli.Execute()
├── Makefile                   # Build targets (macOS ARM64, Linux AMD64)
├── cli/                       # Cobra commands (presentation layer)
│   ├── root.go                # Root command, persistent flags, newService(), Execute()
│   ├── <resource>.go          # One file per resource domain (project, item, user, etc.)
│   └── init_templates.go      # Template scaffolding command
├── internal/
│   ├── api/                   # Hand-written request/response types (OpenAPI-derived)
│   │   ├── types.go           # Core types (ProjectType, CategoryType, FieldType, etc.)
│   │   ├── items.go           # TrimItem, TrimFolder, FieldValType, TrimLink
│   │   ├── requests.go        # CreateItemRequest, UpdateItemRequest, etc.
│   │   ├── responses.go       # AddItemAck, CopyItemAck, ListProjectAndSettings, etc.
│   │   └── <domain>.go        # users, groups, todos, search, jobs, files, audit, merge
│   ├── client/                # HTTP client with token auth
│   │   ├── client.go          # Get/Post/Put/Delete/PostForm/GetRaw + Wfgw* methods
│   │   └── errors.go          # APIError type, IsNotFound/IsUnauthorized/IsForbidden
│   ├── config/                # Viper-based config (XDG: ~/.config/mxreq/config.yaml)
│   │   └── config.go          # Config struct (url/token/default_project/jira_base_url)
│   ├── service/               # Business logic — interface-based domain services
│   │   ├── service.go         # MatrixService aggregate (holds all services + Client)
│   │   └── <domain>.go        # projects, items, categories, fields, users, groups, etc.
│   ├── output/                # Pluggable formatters
│   │   ├── output.go          # Formatter interface, Print(), PrintItem()
│   │   ├── json.go            # JSON output (MarshalIndent)
│   │   ├── table.go           # Lipgloss table output
│   │   └── text.go            # Plain text output
│   ├── fieldmap/              # Field ID resolution with disk cache
│   ├── itemsync/              # Item parsing (YAML/Python/Go/TypeScript) + sync/diff
│   ├── execution/             # Test execution result upload (TC→XTC mapping)
│   └── templates/             # Embedded code templates (go/python/typescript)
├── tests/                     # Shell-based integration tests
│   ├── helpers.sh             # Test helpers: assert, assert_fail, assert_output_contains
│   ├── run-all.sh             # Test runner
│   └── test-<resource>.sh     # One test script per command group
└── dist/                      # Build output (gitignored)
```

## Architecture & Data Flow

```
CLI command (cli/*.go)
  → newService() → config.Load() + client.New() + service.New()
  → service.<Domain>.<Method>(args)
  → client.Get/Post/Put/Delete(path)
  → HTTP request to MatrixALM API (base URL + /1 suffix)
  → Parse response → output.Print() or output.PrintItem()
```

### Layer Responsibilities

| Layer | Package | Role |
|-------|---------|------|
| Presentation | `cli/` | Cobra commands, flag parsing, output dispatch |
| Business Logic | `internal/service/` | Domain operations, URL construction, response mapping |
| Transport | `internal/client/` | HTTP requests, auth headers, error wrapping |
| Types | `internal/api/` | Request/response structs matching the API schema |
| Config | `internal/config/` | XDG config loading (flags > env > file) |
| Output | `internal/output/` | JSON, table (lipgloss), or text formatting |

## Key Conventions

### Configuration Precedence
Flags > Environment vars > Config file (`~/.config/mxreq/config.yaml`)

| Flag | Env Var | Config Key |
|------|---------|------------|
| `--url` | `MATRIX_URL` | `url` |
| `--token` | `MATRIX_TOKEN` | `token` |
| `--project` / `-p` | `MATRIX_DEFAULT_PROJECT` | `default_project` |
| `--jira-base` (on `jira add` only) | `MATRIX_JIRA_BASE_URL` | `jira_base_url` |
| `--output` / `-o` | — | — |
| `--debug` | — | — |

The `jira_base_url` key is set interactively via `mxreq jira init` and used to
build Jira `browse` URLs when linking issues (see the Jira Links subsystem).

### Debugging
`--debug` prints every HTTP request and response as pretty-printed JSON to stderr. Output includes method, full URL, request body, response status, and response body. Normal command output goes to stdout unaffected. Implemented in `internal/client/client.go` via `debugRequest()`/`debugResponse()` helpers called from every HTTP method. The flag is passed from `cli/root.go` → `client.New(url, token, debug)`.

### API Base Path
The client automatically appends `/rest/1` to the base URL (e.g., `http://host:8080` → `http://host:8080/rest/1`). All standard `Get/Post/Put/Delete` methods target this path.

For the workflow gateway at `/rest/2/wfgw/` (used by the Jira links subsystem), the client exposes a parallel trio: `WfgwGet`, `WfgwPostForm`, and `WfgwDeleteJSON`. They derive the root from `baseURL` (stripping `/rest/1`), reuse the same auth/debug/httpClient pipeline, and translate the HTML `HTTP Status 500` page that the Jira add-on returns for unconfigured projects into a friendly error. Use these methods when targeting any non-`/rest/1` endpoint — don't build ad-hoc requests.

### Authentication
`Authorization: Token <value>` header on every request.

### Write Operations
Commands that modify data require `--reason` / `-r` flag, marked as required via `MarkFlagRequired("reason")`.

**Exception — wfgw endpoints:** the `/rest/2/wfgw/` endpoint does **not** accept a `reason` parameter, so commands in that family (currently `jira add` and `jira remove`) still require `--reason` for CLI consistency but consume the value locally (displayed in the plan/confirmation output) rather than sending it to the server. Do the same for any future wfgw-backed write commands.

### Preflight checks
Some commands depend on config beyond `url`/`token` (e.g., `jira_base_url` for Jira links). Those commands should fail fast at the top of their `RunE` with a clear message rather than making HTTP calls that return misleading empty results. The pattern for Jira commands lives in `cli/jira.go`:

- `resolveJiraBase(flagVal)` — used by `jira add`, returns the effective base URL or an error that mentions both the `init` command and the `--jira-base` escape hatch.
- `requireJiraConfigured()` — used by `jira get` and `jira remove` (which don't consume the URL directly), returns an error pointing only at `jira init`.

Call these before `newService()` so the preflight runs even if other config is also missing.

## Adding a New Command

### 1. Create or extend a CLI file

Each resource gets one file: `cli/<resource>.go`. Follow this exact pattern:

```go
package cli

import (
    "fmt"
    "github.com/VladGavrila/matrixreq-cli/internal/api"
    "github.com/VladGavrila/matrixreq-cli/internal/output"
    "github.com/spf13/cobra"
)

// Register commands in init()
func init() {
    rootCmd.AddCommand(thingCmd)
    thingCmd.AddCommand(thingListCmd)
    thingCmd.AddCommand(thingGetCmd)
}

// Parent command (no RunE — just a grouping node)
var thingCmd = &cobra.Command{
    Use:     "thing",
    Aliases: []string{"th"},
    Short:   "Manage things",
}

// Subcommand — list
var thingListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all things",
    RunE: func(cmd *cobra.Command, args []string) error {
        svc, err := newService()
        if err != nil {
            return err
        }
        project, err := requireProject()  // if project-scoped
        if err != nil {
            return err
        }

        things, err := svc.Things.List(project)
        if err != nil {
            return err
        }

        // JSON: pass the raw struct
        if getOutputFormat() == "json" {
            return output.PrintItem(getOutputFormat(), things)
        }

        // Table/text: build headers + rows
        headers := []string{"ID", "Name", "Status"}
        var rows [][]string
        for _, t := range things {
            rows = append(rows, []string{
                fmt.Sprint(t.ID), t.Name, t.Status,
            })
        }
        return output.Print(getOutputFormat(), headers, rows)
    },
}
```

**Naming conventions:**
- Parent: `<resource>Cmd` (e.g., `thingCmd`)
- Children: `<resource><Action>Cmd` (e.g., `thingListCmd`, `thingCreateCmd`)
- All registered in `init()` functions

**Flags for write commands:**
```go
func init() {
    thingCreateCmd.Flags().StringP("reason", "r", "", "Reason for creation")
    _ = thingCreateCmd.MarkFlagRequired("reason")
    thingCreateCmd.Flags().String("name", "", "Thing name")
    _ = thingCreateCmd.MarkFlagRequired("name")
}
```

### 2. Add API types

In `internal/api/`, add request/response structs. Use these naming patterns:
- Request structs: `Create<Thing>Request`, `Update<Thing>Request`
- Response structs: `<Thing>Type`, `Add<Thing>Ack`
- List wrappers: named structs used in service layer for JSON unmarshaling

### 3. Add a service

In `internal/service/`:

```go
// Interface
type ThingService interface {
    List(project string) ([]api.ThingType, error)
    Get(project string, id int) (*api.ThingType, error)
    Create(project string, req *api.CreateThingRequest) (*api.AddThingAck, error)
}

// Private implementation
type thingService struct {
    client *client.Client
}

// Constructor
func newThingService(c *client.Client) ThingService {
    return &thingService{client: c}
}
```

Then add the field to `MatrixService` in `service.go` and initialize it in `New()`.

### 4. Add tests

- **Go unit tests:** Table-driven, in `<package>_test.go`, same package
- **Shell integration test:** `tests/test-<resource>.sh` following the existing pattern

## Service Layer Patterns

### MatrixService Aggregate
```go
type MatrixService struct {
    Client     *client.Client
    Projects   ProjectService
    Items      ItemService
    Categories CategoryService
    // ... all domain services
}
```

Commands access services via `svc.<Domain>.<Method>()`. For edge cases not covered by a service, use `svc.Client` directly.

### URL Construction
Services build paths using `fmt.Sprintf` with `url.PathEscape()`:
```go
path := fmt.Sprintf("/%s/cat", url.PathEscape(project))
```

### Response Unmarshaling
Services define private wrapper types for list endpoints:
```go
type getThingListAck struct {
    ThingList []api.ThingType `json:"thingList"`
}
```

## Error Handling

- All errors propagate up via early `return err`
- Wrap with context: `fmt.Errorf("creating thing: %w", err)`
- The root command catches errors and prints to stderr with `os.Exit(1)`
- `SilenceUsage: true` and `SilenceErrors: true` on root command
- Client provides `IsNotFound(err)`, `IsUnauthorized(err)`, `IsForbidden(err)` helpers

## Output Formatting

Three modes controlled by `--output` / `-o` flag (default: `table`):

| Format | Function | Behavior |
|--------|----------|----------|
| `json` | `output.PrintItem(format, struct)` | `json.MarshalIndent` the raw API struct |
| `table` | `output.Print(format, headers, rows)` | Lipgloss table with styled headers |
| `text` | `output.Print(format, headers, rows)` | Plain `header: value` lines |

**Pattern in commands:**
```go
if getOutputFormat() == "json" {
    return output.PrintItem(getOutputFormat(), data)
}
// Build headers + rows for table/text
return output.Print(getOutputFormat(), headers, rows)
```

**Lipgloss table notes:**
- `Row(...string)` method takes variadic strings
- `Headers(...string)` sets column headers
- `StyleFunc` for conditional row/cell styling

## Building & Testing

### Build
```bash
make build          # Both platforms
make macos-arm      # dist/mxreq-macos-arm64
make linux-amd64    # dist/mxreq-linux-amd64
go build -o mxreq . # Local dev build
```

Build flags: `CGO_ENABLED=0`, `-ldflags "-s -w"` (stripped binaries).

### Go Unit Tests
```bash
go test ./...                        # All tests
go test ./internal/fieldmap/...      # Single package
go test -run TestParsePython ./internal/itemsync/...  # Single test
```

Tests use table-driven patterns, `t.TempDir()` for isolation, `t.Setenv()` for env vars. No mock frameworks — tests construct real `api.*` types.

### Shell Integration Tests
```bash
./tests/run-all.sh [path-to-binary]  # Run all
./tests/test-item.sh [path-to-binary] # Run one

# Environment controls:
MXREQ_LIVE_TESTS=1    # Enable server integration tests (default: skip)
MXREQ_ADMIN_TESTS=1   # Enable admin/destructive tests
MXREQ_TEST_PROJECT=XX  # Project for live tests (default: SW_Sandbox)
MXREQ_TEST_FOLDER=XX   # Folder for item/folder tests
```

Each test script has three phases:
1. **Offline help tests** — always run, verify `--help` output
2. **Validation tests** — always run, verify required flag/arg rejection
3. **Live tests** — gated by `MXREQ_LIVE_TESTS=1`, test actual API calls

Shell test helpers: `assert`, `assert_fail`, `assert_output_contains`, `assert_output_not_contains`, `print_report`.

## Specialized Subsystems

### Field Map (`internal/fieldmap/`)
Resolves field labels to numeric IDs. Caches in `~/.config/mxreq/fieldcache.json`. Keys: `"Category.FieldLabel"` → field ID. Use `LoadOrFetch(svc, project)` to get or populate.

### Item Sync (`internal/itemsync/`)
Parses test files (Python/Go/TypeScript) and YAML definition files to sync items with the server. Supports:
- YAML docstrings embedded in test functions (`---` delimited)
- Standalone YAML definition files (`items: [...]`)
- `NEW_` prefix convention for items to be created
- Automatic function renaming after creation (e.g., `test_NEW_foo` → `test_TC_42_foo`)
- Diff comparison (fields, labels, links) with whitespace/JSON normalization

### XTC Commands (`cli/xtc.go`)
Four subcommands under `mxreq xtc`:

| Subcommand | What it does |
|------------|-------------|
| `xtc create` | Creates an XTC run folder from one or more TC folders (`POST /{project}/execute`). Accepts `--map Category.Label=Category.Label` to copy TC field values onto XTC items, and `--preset Category.Label=Value` to hard-set fields on all created XTC items. `--dry-run` prints the resolved request JSON without calling the API. |
| `xtc execute` | Combines `create` and `upload` in one shot: creates the XTC run (unless `--folder` is given to reuse an existing one) then immediately uploads results. Requires `--results <file>` or `--results-dir <dir>` (picks latest `results_*.yaml` automatically). |
| `xtc upload` | Uploads a results YAML into an existing XTC folder without creating a new run. Refuses to write any XTC that is out of sync with the local test (see Execution Upload below). |
| `xtc stats` | Fetches all XTCs in a folder and prints per-test status, step counts, and requirement coverage totals. |

Field label resolution in `--map` and `--preset` goes through `resolveFieldLabel(fm, spec)`, a local helper that splits `"Category.Label"` and calls `fm.Resolve()` against the cached fieldmap. No extra HTTP calls are made per flag.

### Execution Upload (`internal/execution/`)
Uploads test execution results. Maps test cases (TC) to execution cases (XTC) via title parsing (expects `"Title (TC-1377)"` format). Tracks worst-case requirement coverage across multiple test cases.

**Sync gate — an XTC is written only when it still matches the local test.** An XTC is created from a snapshot of its TC, so a local test that has gained or lost steps since the run was created no longer describes the same test case. `MatchSteps` (`match.go`, pure and unit-tested in `match_test.go`) compares step counts first, then matches results to steps by requirement link. Any of the following sets `OutOfSync`, and `UploadResults` then leaves the XTC **completely untouched** rather than writing a partial execution that Matrix would display as a finished run:

| Drift | Detected by |
|-------|-------------|
| Local and upstream step counts differ | count comparison (reported alone — requirement findings would be noise) |
| A requirement covered locally has no upstream step | `apply` finds no unfilled match |
| An upstream requirement step is not covered locally | post-match scan for an empty `Result` |
| Two results in one run claim the same XTC | `groupByTest` entry count |

Refusals are reported in `UploadResult.Skipped` (XTC ref → short reason) **and** `Issues` (operator-facing text), with `Successes[ref] = false` so the `n/N` summary stays truthful. A skipped XTC is distinct from a failed one: nothing was sent, so Matrix still holds what it held before. Consumers should read `-o json` rather than scraping the summary — `ElementOS/framework/sync/xtc-results.ts` (`parseUploadResult`) does exactly that.

Step-count drift is resolved by re-syncing the TC (`docstring:syncTests` on the ElementOS side) and recreating the XTC run, then re-uploading. Never "fix" it by loosening the gate.

**`TestStep` is a partial view and must round-trip losslessly.** An upload rewrites the item's entire Steps field, so any key `TestStep` does not model would be *deleted* from the step. Matrix stores a step as a free-form object whose keys come from the project's `xtc_config`, so `TestStep` carries custom `MarshalJSON`/`UnmarshalJSON` that stash every unmodeled key in an `extra` map and write it back untouched. `modeledStepKeys` is derived from the struct tags by reflection, so adding a typed field cannot leave a key in both places.

This is not theoretical: before that marshaler existed, every upload silently dropped `"Ref"` — the `xtc_config` column id CUJO reads for "Requirement/Tspec Link" — emptying that column on every XTC an upload touched, while `itemsync` kept writing it correctly on the TC side (see `stepsToJSON`). When adding a field to `TestStep`, do not switch it to a plain struct marshal.

### Templates (`internal/templates/`)
Embedded templates for Go/Python/TypeScript scaffolding. Used by `cli/init_templates.go`.

### Jira Links (`internal/service/jira.go`, `internal/api/jira.go`, `cli/jira.go`)
Manages external Matrix ↔ Jira issue links via the `/rest/2/wfgw/` workflow gateway (plugin 212 by default). This is orthogonal to Matrix's internal uplinks/downlinks — those still flow through `ItemService.CreateLink/DeleteLink` on `/rest/1`.

- `JiraService.GetLinks/CreateLinks/BreakLinks` map to the three wfgw actions (`GetIssues`, `CreateLinks`, `BreakLinks`) and use the client's `Wfgw*` transport methods.
- Request/response types in `internal/api/jira.go` — note the unusual shapes: `GetIssues` returns a top-level array; `CreateLinks` is POSTed as `application/x-www-form-urlencoded` with a `payload=<json>` body; `BreakLinks` is a DELETE with a JSON body (Go supports this, not all HTTP clients do).
- `cli/jira.go` implements `jira init` (persists `jira_base_url` to config), `jira get`, `jira add`, and `jira remove`. Preflight helpers are documented in the Preflight checks section above.
- **Ghost-item validation:** `jira add` fetches each target item and rejects placeholders (empty title + `v0`) inline before mutating — ghost items are a sign the real item lives in a different project. Skippable via `--skip-validate`.
- **Duplicate guard:** `jira add` runs `GetLinks` per item before calling `CreateLinks` so already-linked Jira keys are skipped and reported. The API is idempotent, but the guard is what lets the CLI print a meaningful plan and exit 0 when there's nothing to do.
- **Auto-pick on remove:** `jira remove` with no `--issue` will auto-select the single existing link on an item. Zero links → error; multiple links → error listing the candidate keys.

## Common Pitfalls

1. **Base URL `/rest/1` suffix** — The client appends `/rest/1` automatically. Don't include it in service paths. For `/rest/2/wfgw/`, use the dedicated `Wfgw*` client methods instead of hand-building requests.
2. **URL encoding** — Always use `url.PathEscape()` for path segments and `url.QueryEscape()` for query params.
3. **Lipgloss table API** — `Row()` takes variadic `...string`, not `[]string`. Use `Row(slice...)` to unpack.
4. **Config validation** — `config.Validate()` requires both `URL` and `Token`. Commands that don't need the API (like `config init`, `version`, `jira init`) skip `newService()`. Commands that require additional config beyond URL/token (e.g., `jira_base_url`) should preflight-check it before calling `newService()`.
5. **JSON output** — Always check `getOutputFormat() == "json"` before building table rows, to avoid unnecessary work.
6. **Required flags** — Use `MarkFlagRequired()` in `init()` — cobra handles validation before `RunE` runs.
7. **Project resolution** — `requireProject()` checks flag first, then config `default_project`. Fail early if neither set.
8. **wfgw idiosyncrasies** — The wfgw endpoint returns a raw HTML `HTTP Status 500` page (not JSON) when a project lacks the required add-on. The `Wfgw*` helpers detect this and translate it into a friendly error; don't second-guess that check by bypassing them.

## Release

When the user requests release notes after completing an implementation:

1. **Ask the user what version number to release** — do not assume or auto-increment.
2. Once the user provides the version, update `var version` in `cli/root.go`:
   ```go
   var version = "<new-version>"
   ```
3. Confirm the build passes with `make build` and that `mxreq --version` prints the new version.

4. Write the release notes in a RELEASE_<new-version>.md file