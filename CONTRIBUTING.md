# Contributing to gomodulith

Thanks for your interest in contributing! This project is a library first and a
CLI second, so the public API and the architecture semantics deserve extra
care.

## Development

Requirements: Go 1.23 or newer.

```bash
go build ./...        # build the library and CLI
go vet ./...          # static analysis
go test ./...         # run the full test suite
go test ./modulith/ -cover   # coverage
gofmt -l .            # formatting check (must be empty)
```

## Tests

- The library tests construct temporary Go module fixtures with `t.TempDir()`
  and verify discovery, verification, graph, cycle and diff behaviour against
  them. When adding a feature, add a fixture-driven test.
- Tests that call `os.Chdir` must **not** use `t.Parallel()` (the working
  directory is process-global).
- Real Go import cycles are compile errors, so module-cycle tests build module
  cycles from multiple packages per module using distinct import pairs — this
  keeps the fixture compilable while still producing a module-level cycle.

## Public API design

- Keep the core `modulith` package free of `testing`-specific behaviour; the
  test-facing helpers (`Scan`, `VerifyTest`) live alongside but call through to
  the pure API.
- Preserve provider/factual data exactly: identifiers, import paths, and
  package names come from `go/packages`, never invented.
- New exported identifiers need a doc comment and, ideally, an example.

## Pull requests

1. Run `gofmt -w .`, `go vet ./...`, and `go test ./...` before pushing.
2. Add tests that cover the new behaviour.
3. Keep changes focused; if you are changing architecture semantics (for
   example the meaning of "allowed dependency"), open an issue first to
   discuss the design.
