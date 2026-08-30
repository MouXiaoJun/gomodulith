# gomodulith

**Architecture verification and modular monolith toolkit for Go.**

`gomodulith` makes modular monolith architecture explicit, testable, documentable, and friendly to both humans and AI coding agents.

It is inspired by the ideas behind Spring Modulith and architecture-testing tools, but is designed around Go's package model, `internal` visibility rules, `go/packages`, and idiomatic `go test` workflows.

## Features

- **Module discovery** — derive application modules from a Go codebase using the `internal/<module>/...` convention, or declare them explicitly.
- **Boundary verification** — detect imports that reach into another module's private (non-API) packages.
- **Dependency rules** — declare which modules may (or may not) depend on which others.
- **Cycle detection** — detect cyclic module dependencies, including cycles that span multiple packages per module and are invisible at the package level.
- **Architecture documentation** — generate Mermaid / D2 / JSON representations of the real module graph.
- **AI/CI-readable contracts** — export machine-readable architecture models that coding agents, CI, and code-review automation can consume.
- **Architecture diffing** — compare two exported models — or two git revisions — and report exactly what changed.
- **SARIF output** — export verification findings as SARIF 2.1.0 for GitHub code scanning.
- **Project configuration** — configure discovery and dependency rules from a `.gomodulith.yaml` or `.gomodulith.toml` file, auto-discovered by the CLI.
- **Event-driven rules** — declare published events and event-only boundaries between modules.
- **AI agent contract** — generate a Markdown architecture contract (`.gomodulith/architecture.md`) that AI coding agents read before writing code.
- **LSP-style diagnostics** — emit violations as LSP diagnostics (`verify --lsp`) for editors and language servers.
- **Incremental caching** — cache the loaded model on disk so repeated `verify` runs skip the expensive `go/packages` load.
- **Type-level API analysis** — detect public surfaces that expose private types, as a symbol-level architecture smell.

## Installation

```bash
go get github.com/MouXiaoJun/gomodulith/modulith
```

To install the CLI:

```bash
go install github.com/MouXiaoJun/gomodulith/cmd/gomodulith@latest
```

## Quick start

A typical modular monolith looks like this:

```text
internal/
├── user/
│   ├── api/      # public API of the module
│   ├── domain/
│   └── internal/ # private implementation
├── order/
│   ├── api/
│   └── internal/
└── payment/
    └── api/
```

By convention, a module's `api` sub-packages form its **public API**. Cross-module imports may use another module's public API, but must not reach into its implementation details.

### Architecture tests

Add a test to your module that scans the codebase and verifies the architecture:

```go
package app_test

import (
    "testing"

    "github.com/MouXiaoJun/gomodulith/modulith"
)

func TestArchitecture(t *testing.T) {
    app := modulith.Scan(t, "./...")
    app.VerifyTest(t)
}
```

A violation fails the test with a report like this:

```text
✗ order
✓ payment
✓ user

order   -> user         OK
    example.com/app/internal/order/api -> example.com/app/internal/user/api
    example.com/app/internal/order/domain -> example.com/app/internal/user/domain
payment -> order        OK

order · example.com/app/internal/order/domain:  [cross-module-private-access] module "order" imports private package "example.com/app/internal/user/domain" of module "user"; import the public API instead

architecture violations: 1 error(s), 0 warning(s)
```

### Explicit module rules

For projects that do not follow the default convention, declare modules and rules explicitly:

```go
func TestArchitecture(t *testing.T) {
    app := modulith.New().
        Module("user", "./internal/user/...").
        Module("order", "./internal/order/...").
        Module("payment", "./internal/payment/...")

    app.ModuleRules("order").AllowDependencies("user", "payment")
    app.ModuleRules("payment").AllowDependencies("user")
    app.ModuleRules("order").ForbidDependencies("billing")

    app.VerifyTest(t)
}
```

In explicit mode, a module without declared `AllowDependencies` is **closed** by default: any module dependency it has is reported as undeclared.

### Event-driven interaction rules

For modules that should be decoupled beyond API boundaries, gomodulith can
enforce **event-driven interaction**. A module's `events` packages (configurable
via `EventElement`, default `events`) are treated as public interaction
surfaces alongside `api` packages.

```go
app.ModuleRules("user").PublishEvents("UserRegistered", "UserDeleted")
app.ModuleRules("notifications").AllowDependencies("user").EventDrivenFrom("user")
```

- `PublishEvents` declares the event types a module publishes; each must exist
  as an exported type in the module, otherwise `missing-published-event` is
  reported.
- `EventDrivenFrom("user")` means the module may import **only** `user`'s event
  packages; any import of `user`'s other packages is an
  `event-driven-violation`.
- If the target module has no event packages at all, `event-package-missing`
  warns that event interaction is impossible.

### Pure API

The library API does not depend on `testing` and can be used from tools, CI, and build scripts:

```go
app, err := modulith.Load(context.Background(), "./...")
if err != nil {
    log.Fatal(err)
}

res, err := app.Verify()
if err != nil {
    log.Fatal(err)
}
for _, issue := range res.Errors() {
    fmt.Println(issue)
}

jsonData, _ := app.ExportJSON(true)   // machine-readable contract + findings
mermaid := app.ExportMermaid()        // graph documentation
```

## CLI

```bash
gomodulith verify [patterns...] [--json|--sarif|--lsp] [--no-cache] [--out <file>]  # verify boundaries and rules (exit 1 on violation)
gomodulith graph [--format text|mermaid|json|d2]                  # render the module graph
gomodulith explain <module>                                       # explain a module's API, dependencies and dependents
gomodulith export [--verify] [--format json|sarif|mermaid|d2|dot] [--out <file>]  # export the architecture model
gomodulith contract [--out <path>]                                # write the AI-agent architecture contract
gomodulith diff <base> <head>                                     # show architecture changes between two states
```

`diff` accepts either a path to an exported architecture JSON file, or a git revision (`HEAD`, `HEAD~1`, a tag or commit hash) that is scanned from a temporary checkout — for example `gomodulith diff origin/main HEAD` to see what a pull request changed.

When a `.gomodulith.yaml` / `.gomodulith.toml` exists in the project (or a parent directory), the CLI reads it automatically and applies its modules and rules.

Example CI usage:

```yaml
- name: Verify architecture
  run: gomodulith verify ./...

- name: Export findings as SARIF
  if: always()
  run: gomodulith export --format sarif --verify --out gomodulith.sarif

- name: Upload SARIF to code scanning
  if: always()
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: gomodulith.sarif
```

A complete, copy-paste workflow is provided in [`examples/architecture-ci.yml`](examples/architecture-ci.yml).

## Checks

`gomodulith verify` runs the following checks:

| Check | Severity | Description |
|---|---|---|
| `cross-module-private-access` | error | A module imports another module's private package instead of its public API |
| `undeclared-dependency` | error | A module depends on a module that is not in its allowed dependencies |
| `forbidden-dependency` | error | A module depends on a module explicitly declared as forbidden |
| `cycle` | error | Cyclic module dependencies |
| `invalid-public-api` | error | A declared public API package does not exist or belongs to another module |
| `missing-public-api` | warning | A module has no public API package (error when `PublicAPIRequired` is set) |
| `missing-published-event` | error | A declared published event type does not exist in the module |
| `event-driven-violation` | error | A module declared event-driven towards another module imports a non-event package of it |
| `event-package-missing` | warning | A module is event-driven towards a module that has no event packages |
| `cross-module-type-leakage` | error | A public surface exposes a type defined in another module's private packages |
| `internal-api-leakage` | error | A public surface exposes a type defined in the module's own private packages |
| `orphan-package` | warning | A package is not part of any module (opt-in via `ReportOrphans`) |

## Configuration

Discovery and verification are controlled by `modulith.Config`:

```go
cfg := modulith.NewConfig()
cfg.ModuleRoot = "internal"     // directory scanned for modules
cfg.APIElement = "api"          // sub-package marking a module's public API
cfg.ReportOrphans = true        // warn about unclassified packages
cfg.PublicAPIRequired = true    // make missing public APIs an error

app := modulith.NewWithConfig(cfg)
```

### Project configuration file

For teams, the same settings plus module rules can live in a checked-in
`.gomodulith.yaml` or `.gomodulith.toml`. The CLI discovers it automatically
(upward from the working directory) and applies it to `verify`, `graph`,
`explain`, `export` and `diff`.

```yaml
# .gomodulith.yaml
module_root: internal            # optional; defaults to "internal"
api_element: api                 # optional; defaults to "api"
event_element: events            # optional; defaults to "events"
report_orphans: false
public_api_required: false
patterns: ["./..."]              # default load patterns when none are given

modules:                         # optional: enables explicit rule mode
  user:
    patterns: ["./internal/user/..."]
    publish_events: ["UserRegistered"]
  order:
    patterns: ["./internal/order/..."]
    allowed: ["user"]            # order may depend on user
    forbidden: ["billing"]
    public: ["./internal/order/api"]
  notifications:
    patterns: ["./internal/notifications/..."]
    allowed: ["user"]
    event_driven_from: ["user"]  # only user's events packages may be imported
```

```toml
# .gomodulith.toml
module_root = "internal"
report_orphans = true
patterns = ["./..."]

[modules.user]
patterns = ["./internal/user/..."]

[modules.order]
patterns = ["./internal/order/..."]
allowed = ["user"]
publish_events = ["OrderPlaced"]
```

When `modules` is empty, the file only adjusts convention discovery. When it
declares modules, the application runs in explicit rule mode (as in the
programmatic `New()` API). The library exposes the same configuration through
`modulith.LoadConfigFile` / `modulith.ParseConfig` / `FileConfig.Build`.

### AI-agent architecture contract

AI coding agents should not have to reverse-engineer the architecture from
code. `gomodulith contract` writes a Markdown contract describing every
module's public API, dependency rules, published events and verification
status:

```bash
gomodulith contract          # writes .gomodulith/architecture.md
```

The contract starts with "Architecture Contract" and lists, per module, what
other code may and may not do with it, plus the current verification status.
Point your agent (Cursor, Claude Code, ...) at this file, e.g. by mentioning it
in your `AGENTS.md`:

```markdown
Before modifying code, read `.gomodulith/architecture.md` and follow the
architecture rules it describes.
```

From Go, use `app.ExportContract()`.

### Analysing another directory or git revision

The library can scan an arbitrary directory without touching the process
working directory:

```go
app, err := modulith.LoadIn(ctx, "/path/to/checkout", "./...")
```

This is what powers the CLI's `diff <git-ref> <git-ref>` and is handy for
tools that analyse a revision other than the current checkout.

### SARIF output

`app.ExportSARIF()` returns verification findings as a SARIF 2.1.0 document,
ready for GitHub code scanning and similar consumers. Use it from Go, or via
`gomodulith verify --sarif` / `gomodulith export --format sarif --verify`.

### LSP-style diagnostics

For editors and language servers, `gomodulith verify --lsp` emits violations as
LSP `Diagnostic` objects, each attached to the first source file of the
offending package (`file://` URI, zero-based range, LSP severity):

```json
{
  "uri": "file:///path/to/internal/order/domain/dom.go",
  "range": { "start": { "line": 0, "character": 0 }, "end": { "line": 0, "character": 0 } },
  "severity": 1,
  "code": "cross-module-private-access",
  "source": "gomodulith",
  "message": "module \"order\" imports private package ..."
}
```

From Go, use `app.ExportDiagnostics()`. Diagnostics are package-level: they
point at the package's file, not a specific line.

### Incremental caching

On large repositories the `go/packages` load dominates runtime. `gomodulith
verify` caches the loaded model in `.gomodulith/cache/model.json` and skips the
load on subsequent runs when nothing changed. The cache key covers `go.mod`,
`go.sum`, Go source contents, configuration, and the effective `go env` build
environment (including toolchain, build tags, target OS/architecture and cgo).
Equal-sized edits still invalidate the cache even when mtimes are restored.

Caching is conservative: active Go workspaces, local module replacements,
vendor trees, overlays/alternate modfiles, external package drivers, symlinked
trees and non-local load patterns fall back to normal loading. Old cache
formats are ignored. The cache does not provide a snapshot of concurrent edits
or track external C headers/toolchain modifications; use `--no-cache` when
those inputs may change.

```bash
gomodulith verify            # first run populates the cache
gomodulith verify            # subsequent runs reuse it
gomodulith verify --no-cache # bypass the cache
```

From Go, use `LoadCachedIn` / `LoadExplicitCached` with your own cache
directory.

### Type-level API analysis

Beyond import boundaries, `gomodulith` inspects the **types** a module's public
surface (public API and event packages) exposes, using `go/types`. Two checks
catch API design defects that import-level checks cannot see:

```go
// internal/order/api/api.go — order's public API exposes order's internal type
type OrderDTO struct {
    Order domain.Order // domain is order's private package
}
// -> internal-api-leakage: external consumers cannot name domain.Order
```

```go
// internal/order/events/events.go — order's event payload carries user's private type
type OrderPlaced struct {
    By domain.UserID // user/domain is user's private package
}
// -> cross-module-type-leakage: subscribers cannot construct the event
```

A public surface may only reference types that live in a public surface: its
own public/event packages, or another module's public/event packages. Leaking a
private type makes the API unusable from outside and couples internals into the
public contract. Use `app.PackageByID("…").APITypeRefs` to inspect what a package
exposes.

## Roadmap

### v0.1 — Architecture model ✅

- [x] load packages with `go/packages`
- [x] discover modules from conventions
- [x] build module dependency graph
- [x] detect cycles
- [x] report cross-module boundary violations
- [x] `gomodulith verify`
- [x] architecture tests through `go test`

### v0.2 — Rules and configuration ✅

- [x] explicit module definitions
- [x] allowed / forbidden dependencies
- [x] public API package declarations
- [x] useful diagnostics with import paths
- [x] programmatic configuration

### v0.3 — Documentation ✅

- [x] Mermaid graph output
- [x] D2 graph output
- [x] JSON architecture export
- [x] module dependency reports (`explain`)
- [x] architecture diffing between exports

### v0.4 — CI and architecture evolution ✅

- [x] git-revision diffing (`gomodulith diff <ref> <ref>`)
- [x] SARIF output for GitHub code scanning
- [x] YAML/TOML project configuration files

### v0.5 — Events, agents and ergonomics ✅

- [x] event-based module interaction rules (`PublishEvents`, `EventDrivenFrom`)
- [x] architecture contract for AI coding agents (`gomodulith contract` → `.gomodulith/architecture.md`)
- [x] Graphviz DOT graph export (`export --format dot`)
- [x] directory-aware test helpers (`ScanDir` / `ScanDirWithConfig`)
- [x] CI workflow now publishes the contract and graphs as artifacts

### v0.6 — Editors and speed ✅

- [x] LSP-style diagnostics (`verify --lsp` / `ExportDiagnostics`)
- [x] incremental on-disk model cache (`verify` cache, `--no-cache`, `LoadCachedIn` / `LoadExplicitCached`)

### v0.7 — Symbol-level architecture smell detection ✅

- [x] type-level API analysis via `go/types` (exported API surface references)
- [x] `cross-module-type-leakage` — public surfaces must not expose another module's private types
- [x] `internal-api-leakage` — public surfaces must not expose the module's own private types (unusable from outside)
- [ ] unused-event detection (published events nobody subscribes to)

## Project philosophy

A modular monolith should not depend on developers remembering a diagram from six months ago.

The source code already contains the real dependency graph. `gomodulith` reads that graph, compares it with the intended architecture, and makes divergence impossible to ignore.

```text
architecture convention
        ↓
architecture model
        ↓
verification
        ↓
CI
        ↓
documentation + AI context
```

## Non-goals

`gomodulith` is **not**:

- a dependency injection container;
- a web framework;
- a service mesh;
- a microservice framework;
- a replacement for the Go compiler's `internal` rules;
- a general-purpose linter replacement;
- a runtime module system.

The focus is narrow: **make modular Go architecture explicit and enforceable.**

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Issues discussing module discovery rules, package conventions, diagnostics, and public API design are especially welcome.

## License

[MIT](LICENSE)
