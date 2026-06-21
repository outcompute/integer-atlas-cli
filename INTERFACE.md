# Integer Atlas CLI — interface

A single fast **Go binary** with embedded DuckDB. The only tool a consumer needs, and
the orchestrator contributors use. It talks to three things:

- the **Shards** repo — reads its `pending/` and `accepted/` directories directly
  (shallow clone / tarball) and produces PR content for submissions;
- external **storage** — downloads accepted shard files from their manifest URLs;
- the **Algos** executor (`atlas-algos`) — installed on demand and driven for `compute` / `verify`.

Command: **`integer-atlas`**, short alias **`ia`**.

Design stance: **automation-first**. Setup, manifest refresh, loading, and toolchain
installation happen automatically; power users get escape-hatch flags.

---

## 1. Installation (the bootstrap)

Setup is a guided one-liner, not a subcommand:

```
curl -fsSL https://raw.githubusercontent.com/outcompute/integer-atlas-cli/main/install.sh | sh
```

It detects the platform, asks **what you want**, installs accordingly, and creates the
local workspace:

- **SQL-only** → installs just the CLI (embedded DuckDB). **No Docker.** Enough to
  fetch and query.
- **Full UI** → installs the CLI **and** brings up the Docker stack (notebook +
  dashboards + service over the same shards) from the UI repo.

(The installer lives with the UI/distribution layer and is built alongside the UI repo;
it ties the CLI and UI together.)

---

## 2. Invocation & global options

```
integer-atlas <command> [args] [options]
```

| Global option | Default | Meaning |
| --- | --- | --- |
| `--workspace DIR` | auto (`$INTEGER_ATLAS_HOME` or `~/.integer-atlas`) | Local workspace (cache, DB, config). Auto-created on first use. |
| `--registry URL` | baked-in Shards repo | Shards repo to read (git URL or local path); point at a fork. |
| `--release REF` | latest on default branch | Pin a git ref (tag/commit) of the Shards repo for a reproducible snapshot. |
| `--refresh` | off | Force-refresh the manifest cache now (otherwise auto with a short TTL). |
| `--json` | off | Machine-readable JSON on stdout (else human tables). |
| `--quiet` / `--log-level L` | INFO | Quiet / set verbosity (logs on stderr). |
| `-y, --yes` | off | Assume "yes" for confirmation prompts. |
| `--version`, `--help` | | Version / help. |

**Workspace:** holds `config.toml`, `cache/` (a local copy of the Shards repo + downloaded
shard files), and `atlas.duckdb`. Created automatically; the installer also sets it up.

**Exit codes:** `0` ok · `2` bad usage · `3` nothing found · `4` verification failed ·
`1` other · `130` interrupted.

---

## 3. Commands

### Discover

| Command | Does | Options | Side effects | When |
| --- | --- | --- | --- | --- |
| `packs` | List the column groups present in accepted shards and the ranges they cover (derived from manifests). | `--state accepted\|pending\|all`, `--json`, `--refresh` | pulls the Shards repo if stale | See what exists and where the holes are. |
| `describe <column>` | Show a column's type and which shards/ranges carry it (richer descriptions/OEIS come from the Algos catalog when the toolchain is synced). | `--json` | none | Understand what a column means. |

### Consume

| Command | Does | Options | Side effects | When |
| --- | --- | --- | --- | --- |
| `fetch` | Download accepted shards for the request, verify hashes, and **auto-load** them into the local DB. | `<group>` \| (`--start`,`--end`,`--columns`), `--release`, `--no-load`, `--refresh` | downloads files; updates `atlas.duckdb` | Get data to query. |
| `sql` | Read-only SQL over the loaded data; no arg → REPL. | `"<query>"` \| `--file F`, `--format table\|csv\|json\|parquet`, `--output PATH` | read-only (writes only with `--output`) | Query/analyze; `--output` exports. |
| `status` | Show what's fetched/loaded, DB size, registry pointer, and toolchain state. | `--json` | none | Inspect local state. |

### Contribute

| Command | Does | Options | Side effects | When |
| --- | --- | --- | --- | --- |
| `work` | List pending work orders (holes): id, range, columns, expected rows, cost estimate. | `--json`, `--refresh` | pulls the Shards repo if stale | Find something to compute. |
| `compute` | Resolve the work order, **auto-install the algos toolchain** (pinned), show the estimate, create the shard, and write a **draft manifest auto-filled** (hashes, local path, row count, columns, algo release, format). Resumable. | `--task ID` \| `--manifest FILE` \| (`--pack`,`--start`,`--end`,`--columns`); `--chunk-size`, `--format parquet\|csv`, `--out`, `--algos-release` | installs toolchain if needed; writes shard + draft manifest | Compute an official hole or a custom shard. |
| `verify` | Read a manifest, **locate the shard** (its local path, else download from the manifest's URL), check row count/schema/contiguity, **match hashes**, and recompute a sample to `--degree`. | `--manifest FILE`, `--degree F` (default 0.1), `--seed` | may download the shard | Self-check before submit; maintainer full check (`--degree 1.0`). |
| `sideload` | Load a local/custom shard into a `side_*` table (and the UI) for testing — without touching canonical data. | `<shard-file>` \| `--manifest FILE`, `--table NAME`, `--owner NS` | updates workspace manifest + DB | Test a computed or custom shard. |
| `submit` | Take the draft manifest, fill in the storage URL + metadata, and print the **final manifest JSON + a ready-to-paste PR body** for the Shards repo. No GitHub automation. | `--manifest FILE`, `--url URL`, `--author`, `--license`, `--out FILE` | prints (optionally writes `--out`) | After uploading the shard, to open the Shards PR. |

### Utility

| Command | Does | When |
| --- | --- | --- |
| `doctor` | Check the setup: workspace, registry reachability, embedded DuckDB, toolchain. | Troubleshooting. |
| `version` | CLI version, embedded DuckDB version, registry URL in use. | Support/debug. |

---

## 4. The manifest lifecycle (how it's populated and used)

1. **Work order (pending)** — a JSON file in the Shards repo's `pending/`: `{id, range,
   columns, algorithm_release}`. Listed by `work`; consumed by `compute --task <id>`.
2. **Draft (computed)** — `compute` fills in everything it can: SHA256/SHA512/BLAKE3,
   the local shard path, row count, columns + types, `generated_at`, the algo release,
   and format/compression. The storage URL is left blank.
3. **Final** — after you upload the shard, `submit --url <loc>` adds the storage URL and
   metadata (author, license) and prints the manifest + PR body.
4. **Verify** — `verify --manifest <draft-or-final>` finds the shard via its local path
   or downloads it from the URL, matches the hashes, and recomputes to the chosen degree.

Custom shards skip steps 1/3: write your own work-order manifest, `compute --manifest`,
then `sideload` to test locally.

## 5. Automatic behaviors (and the power-user flags)

| Automatic | Escape hatch |
| --- | --- |
| Workspace is created on first use. | `--workspace DIR` |
| The local Shards-repo copy refreshes on a short TTL. | `--refresh` to force now |
| `fetch` loads shards into the DB after verifying. | `--no-load` to skip |
| `compute` installs the algos toolchain pinned to the release. | `--algos-release NAME` |

(These replace the former explicit `init`, `sql init`, `registry sync`, `coverage`, and
`algos sync` commands.)

## 6. How it touches the rest of the system

- **Shards repo:** `packs` / `describe` / `work` read the repo's `accepted/` and `pending/`
  directories (pulled as a shallow clone / tarball); `submit` emits PR content you paste
  into a Shards pull request (the CLI never writes to GitHub).
- **Storage (Kaggle / object store):** `fetch` downloads accepted shards from manifest
  URLs; `verify` downloads a shard when only its URL is known; `submit` records the URL
  where you uploaded yours.
- **Algos repo:** `compute`/`verify` install and drive `atlas-algos` pinned to an algo
  release. Consumers never trigger this.
- **Local workspace + UI:** `fetch`, `sql`, `sideload` work the local DuckDB; sideloaded
  shards are visible to the UI stack if installed.

## 7. End-to-end journeys

**Consumer (install → query):**
```
curl -fsSL https://raw.githubusercontent.com/outcompute/integer-atlas-cli/main/install.sh | sh      # choose SQL-only
integer-atlas packs                             # what's available + holes
integer-atlas describe omega_big                # type & which ranges carry it
integer-atlas fetch --start 1 --end 1000000 --columns omega_big,is_prime
integer-atlas sql "SELECT omega_big, count(*) FROM numbers GROUP BY 1 ORDER BY 1"
```

**Contributor (find work → publish):**
```
integer-atlas work                              # pick a hole
integer-atlas compute --task T-collatz-0007     # estimate + create + draft manifest
integer-atlas verify --manifest ./<draft>.json --degree 0.1
integer-atlas sideload --manifest ./<draft>.json    # test locally / in the UI
# (upload the shard to your storage)
integer-atlas submit --manifest ./<draft>.json --url <hosted-url>   # manifest + PR body
# (open a PR to the Shards repo, paste the content)
```

**Custom compute:**
```
integer-atlas compute --manifest ./my-order.json
integer-atlas sideload --manifest ./my-order.computed.json
integer-atlas sql "…"
```

## 8. Notes

- Setup is via the `curl … | sh` installer; SQL-only needs no Docker, the full UI uses Docker.
- `fetch` auto-loads, the local Shards-repo copy auto-refreshes, and `compute` uses the
  installed `atlas-algos` — power-user flags (`--refresh`, `--no-load`, `--algos-release`,
  `--workspace`) override these.
- The CLI reads the Shards repo directories directly: `--registry` is the repo (git URL or
  local path), `--release` is a git ref.
