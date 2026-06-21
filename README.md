# Integer Atlas — CLI

A single fast **Go binary** — the only tool a consumer needs. DuckDB is embedded, so it is a true drop-in with no external dependencies.

## Purpose

- Fetch shards and load them into a local DuckDB.
- Query the manifests for pending, computed, or accepted shards.
- For contributors/maintainers: sync the Algos toolchain and drive `compute` / `verify`.

## Why Go

A single self-contained binary per platform with embedded DuckDB (statically linked, no separate install) and good HTTP support.

## Command surface

The full command reference is in [INTERFACE.md](INTERFACE.md). Command is `integer-atlas`
(alias `ia`); the design is **automation-first** (setup, manifest refresh, loading, and
toolchain install happen automatically, with power-user flags).

| Command | Does |
| --- | --- |
| `packs` | list packs, ranges covered, and state (accepted/pending) |
| `describe <pack\|column>` | descriptions, types, OEIS |
| `fetch` | download accepted shards, verify, and auto-load into the local DB |
| `sql` | read-only SQL over loaded data (or REPL) |
| `status` | what's fetched/loaded locally |
| `work` | list pending shards needing computation |
| `compute` | estimate + create a shard + a draft auto-filled manifest (auto-installs the Algos toolchain) |
| `verify` | locate a shard from its manifest, match hashes, recompute to `--degree` |
| `sideload` | load a local/custom shard for testing / the UI |
| `submit` | finalize the manifest + print a paste-ready PR body (no GitHub automation) |
| `doctor`, `version` | diagnostics / versions |

Setup is a guided `curl … | sh` installer (SQL-only = CLI only, no Docker; full UI = CLI
+ Docker stack), not a subcommand. A consumer who only runs `fetch` / `sql` never needs
the Algos toolchain; `compute`/`verify` install it on first use.

Folded into automatic behavior (with escape-hatch flags): the former `init`, `sql init`,
`registry sync`, `coverage`, and `algos sync`.

## Build

Go module with **embedded DuckDB** (go-duckdb, cgo) — needs a C toolchain (clang/gcc).
Builds to a single binary; DuckDB is statically linked, no separate install.

```
go build -o integer-atlas .
./integer-atlas packs                                # discover available data
./integer-atlas sql "SELECT count(*) FROM numbers"   # after a fetch
```

**Commands**
- discover/consume — `packs`, `describe`, `work`, `status`, `version`, `doctor`; `fetch`
  (download + SHA-256 verify + auto-load); `sql` (embedded DuckDB; the `numbers` view
  unions same-column-group shards across ranges and FULL-joins different groups on `n`;
  `--format table|csv|json|parquet`, `--output`, REPL); `sideload` (register a local
  shard as a `side_*` view).
- contribute — `compute` / `verify` / `submit` drive `atlas-algos` (needs it on PATH or
  `--algos-bin` / `INTEGER_ATLAS_ALGOS`).

Reads the Shards repo as a local path or git URL (`--registry`); `--release` checks out a
git ref.

## Manifest pointer

Hard-pointed at a single stable `index.json` on the Shards release branch (overridable
via `--registry`). The rolling manifest at the branch tip is the default (always latest);
`--release <name>` pins an immutable dataset snapshot. The cache auto-refreshes (force
with `--refresh`).

## Dependencies

Consumes Shards over HTTP; drives Algos via subprocess. Does not import either repo's code.
