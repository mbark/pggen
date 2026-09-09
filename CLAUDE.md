# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

pggen is a CLI that generates type-safe Go code from Postgres SQL queries. It works by
*running* each query against a real Postgres and reading the type information back out of
the system catalogs, so there is no SQL parser to teach about new types — if Postgres can
run the query, pggen can generate for it.

This repo is `github.com/mbark/pggen`, a fork of `jschaf/pggen` (the README badges still
point at upstream). Fork-specific query pragmas: `output=<RowType>` (share one row struct
across queries) and `paginate=<spec>` with `-- sort:` blocks (keyset pagination fan-out) —
see README "Features" and `internal/parser/paginate.go`.

It also ships **`chgen`**, the same tool pointed at ClickHouse: same query file format,
same pragmas, a different database and driver. The two are separate binaries so each
keeps its own flags and neither grows a dialect switch. `paginate=` is rejected for
ClickHouse for now.

## Commands

Tooling is pinned in `mise.toml`; `mise install` gets the same Go and golangci-lint that CI
uses. Docker is also required.

```shell
mise run start              # long-lived Postgres on :5555 and ClickHouse on :9010
mise run stop               # (docker compose) — both needed by unit tests
mise run psql               # psql into the Postgres container
mise run clickhouse-client  # clickhouse-client into the ClickHouse container
mise run build              # go build ./...
mise run test               # go test ./... (depends on start)
mise run acceptance-test    # go test --tags=acceptance_test ./...
mise run update-acceptance-test  # rewrite committed example output
mise run lint               # golangci-lint run
mise run all                # lint + test + acceptance-test
```

ClickHouse uses 9010/8124 rather than the default 9000/8123 so this container can
coexist with one another project may already have running on the usual ports.

Single test: `go test ./internal/pginfer/ -run TestInferrer_InferTypes` (start Postgres
first if the package touches the database). Acceptance subtests are named after the example
dir: `go test --tags=acceptance_test ./example/ -run 'TestExamples/example/author'`.

If your Docker daemon socket is not `/var/run/docker.sock` (Docker Desktop on macOS), set
`DOCKER_HOST` in a gitignored `mise.local.toml` — the acceptance tests reach Docker through
the Go client and will not find it otherwise.

**The acceptance suite asserts the working tree has no git diff** (`assertNoGitDiff` in
`example/acceptance_test.go`), so commit — including `go.mod`/`go.sum` — before running it.

## Test layers

- **Unit** — pure logic, e.g. `internal/casing/casing_test.go`. `mise run test`.
- **Integration** — talks to the shared Postgres on :5555. `internal/pgtest` creates a
  randomly named schema per test (`pggen_test_<n>`, logged in the test output) so tests are
  isolated without a container per test. `mise run test`. The ClickHouse equivalent is
  `internal/chtest`, which creates a randomly named *database* per test — ClickHouse has no
  `search_path`, so a schema won't do.
- **Acceptance** — `//go:build acceptance_test`. `example/acceptance_test.go` compiles the
  CLI, spins up its own throwaway Postgres via `pgdocker`, regenerates every example listed
  in its table, and asserts no git diff. Adding an example means adding a row to that table
  *and* committing the generated output. `TestClickHouseExamples` in the same file is the
  chgen counterpart, backed by `chdocker`.
- **Per-example codegen tests** — `example/*/codegen_test.go` call `pggen.Generate` directly
  and diff against the checked-in `query.sql.go`; `example/*/query.sql_test.go` execute the
  generated queries. These are the first place to look when debugging codegen or generated
  query behaviour.

## Pipeline

`ARCHITECTURE.md` has the authoritative walkthrough. The shape:

`cmd/pggen/pggen.go` (flag parsing, glob resolution) → `generate.go` `Generate` →
connect Postgres (`--postgres-connection`, else `internal/pgdocker` starts a container) →
`internal/parser` produces `*ast.File` of `*ast.SourceQuery` → `internal/pginfer` infers
param and column types by preparing the query and reading field descriptions →
`internal/codegen/golang` templates and emits the Go file.

Things that are easy to miss:

- **pggen bootstraps itself.** `internal/pg/query.sql` holds the catalog queries pggen uses
  to resolve types, and `internal/pg/query.sql.go` is generated *by pggen* from it. Editing
  `internal/pg/query.sql` means regenerating — it is a row in the acceptance table.
- **Nullability is heuristic**, not analysis (`internal/pginfer/nullability.go`). A
  non-null column gets an ergonomic Go type (`string`) instead of `pgtype.Text`. A real
  solution needs control-flow analysis; `internal/pgplan` is an abandoned start on that.
- **Declarers** (`internal/codegen/golang/declarer*.go`) are how shared declarations —
  enums, composite types, the `Querier` interface, `RegisterTypes`, `output=` row structs —
  get emitted exactly once. Each has a namespaced `DedupeKey`. The templater picks a
  **leader file** (lexicographically first source path) and emits every declarer there, so
  changing the set of query files can move declarations between generated files.
- **Type mapping** lives in `internal/codegen/golang/type_resolver.go` and
  `gotype/known_types.go`; `--go-type pg_type=go.pkg/Type` overrides it. ClickHouse has its
  own pair, `ch_type_resolver.go` and `ch_known_types.go`, keyed by the canonical type name
  rather than an OID.
- **ClickHouse specifics.** `internal/ch` is a pure, database-free model of the ClickHouse
  type system plus a parser for type names; `internal/chinfer` runs `DESCRIBE` to learn a
  query's result columns. Two behaviours are easy to trip over:
  - The emitted SQL rewrites `{name:Type}` to `cast(@name AS Type)`
    (`ch.RewriteParams`). Server-side parameters travel as text and clickhouse-go renders
    `time.Time`, `uuid.UUID` and `decimal.Decimal` in forms the server rejects; the `@name`
    form uses client-side binding, which gets them right. Query files keep the native
    syntax, so they stay runnable in `clickhouse-client`.
  - ClickHouse enums map to `string`. They are anonymous — the labels are the type — so
    there is no name to derive a Go type from, and two columns sharing a label set are the
    same type. `--go-type "Enum8('a' = 1, 'b' = 2)=pkg.T"` overrides a specific one — and
    `pkg.T` has to be a `sql.Scanner` or a type the driver already handles, because
    clickhouse-go decodes into nothing else. A plain named type compiles and fails at run
    time. `chgen`'s `--go-type` splits on the *last* `=`, since the enum type carries its
    own. `example/clickhouse_multi` covers both.
  - With `join_use_nulls` off (the default), a LEFT JOIN does **not** make the right side's
    columns `Nullable` — unmatched rows get type defaults like `''` and `0`. Inference is
    only correct under the settings the application connects with; pass them with
    `chinfer.WithSettings`.
- The parser is hand-written (`internal/scanner`, `internal/token`, `internal/parser`),
  modelled on go/parser — it splits SQL into named queries by comment annotations, it does
  not understand SQL itself.

## Conventions

Design goals from `CONTRIBUTING.md`, worth honouring when adding features:

- Minimal API surface — one way to do a thing (`--query-glob` only, no `--query-file`).
- If it's expressible in SQL, don't add a pggen flag for it.
- Correctness over convenience: expose Postgres' details rather than smoothing them over.
- **Generated code must look hand-written, including formatting.** Output does not depend on
  gofmt, so the templates (`query.gotemplate`, `templated_file.go`) own whitespace exactly.
