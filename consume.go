package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	start := fs.Int64("start", 0, "range start (inclusive)")
	end := fs.Int64("end", 0, "range end (inclusive)")
	cols := fs.String("columns", "", "comma-separated columns; selects the shard groups that carry them")
	tableFlag := fs.String("table", "", "comma-separated pack/table names to fetch (e.g. core,factor)")
	all := fs.Bool("all", false, "fetch every accepted shard (the whole dataset)")
	noLoad := fs.Bool("no-load", false, "download/verify only; skip loading into the DB")
	// Allow leading positional pack names (`fetch core --start 1 ...`); Go's flag
	// parser otherwise stops at the first non-flag arg and ignores later flags.
	var leadTables []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		leadTables = append(leadTables, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, err := resolveRegistry(cfg)
	if err != nil {
		return errReturn(err)
	}
	accepted, err := loadManifests(acceptedDir(root))
	if err != nil {
		return errReturn(err)
	}

	// What to fetch: pack/table names (from --table and/or positional args),
	// columns, and/or a range. A shard is a whole file, so packs/columns choose
	// which shards to download — not which columns within them (narrow at SQL time).
	tables := append(splitCSV(*tableFlag), leadTables...)
	tables = append(tables, fs.Args()...) // also allow trailing positional pack names
	want := splitCSV(*cols)
	hasContentSel := len(tables) > 0 || len(want) > 0
	hasRange := *start > 0 || *end > 0

	if !hasContentSel && !hasRange && !*all {
		fmt.Fprintln(os.Stderr, "refusing to fetch the entire dataset. Narrow it with a pack "+
			"(e.g. `integer-atlas fetch core`), --columns, or a bounded --start/--end; or pass --all.")
		return exitUsage
	}

	// Validate requested pack names against what's actually published.
	avail := map[string]bool{}
	for _, m := range accepted {
		avail[m.tableName()] = true
	}
	for _, t := range tables {
		if !avail[t] {
			names := make([]string, 0, len(avail))
			for n := range avail {
				names = append(names, n)
			}
			sort.Strings(names)
			fmt.Fprintf(os.Stderr, "unknown pack %q; available: %s\n", t, strings.Join(names, ", "))
			return exitUsage
		}
	}
	tableSet := map[string]bool{}
	for _, t := range tables {
		tableSet[t] = true
	}

	var sel []Manifest
	for _, m := range accepted {
		// range overlap; the upper bound is enforced only when --end is given
		if *end > 0 && (m.RangeEnd < *start || m.RangeStart > *end) {
			continue
		}
		if *end <= 0 && *start > 0 && m.RangeEnd < *start {
			continue
		}
		if hasContentSel {
			inTable := len(tables) > 0 && tableSet[m.tableName()]
			inCols := len(want) > 0 && hasAnyColumn(m, want)
			if !inTable && !inCols {
				continue
			}
		}
		sel = append(sel, m)
	}
	if len(sel) == 0 {
		fmt.Println("no accepted shards match the request")
		return exitNotFound
	}

	if err := cfg.ensureWorkspace(); err != nil {
		return errReturn(err)
	}
	sd, md := shardsDir(cfg), manifestsDir(cfg)
	for _, d := range []string{sd, md} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return errReturn(err)
		}
	}

	for _, m := range sel {
		if len(m.Storage) == 0 {
			fmt.Fprintf(os.Stderr, "skip %s: manifest has no storage URL\n", m.File)
			continue
		}
		dest := filepath.Join(sd, m.File)
		fmt.Printf("fetching %s\n", m.File)
		if err := download(m.Storage[0], dest); err != nil {
			fmt.Fprintf(os.Stderr, "  download failed: %v\n", err)
			return exitErr
		}
		checked, mismatch, err := verifyHashes(dest, m.Hashes)
		if err != nil {
			return errReturn(err)
		}
		if mismatch != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", mismatch)
			return exitVerify
		}
		// cache the manifest so the DB views can be (re)built from the cache.
		mb, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(md, m.File+".json"), append(mb, '\n'), 0o644); err != nil {
			return errReturn(err)
		}
		if len(checked) > 0 {
			fmt.Printf("  ok %s\n", strings.Join(checked, "+"))
		} else {
			fmt.Println("  ok")
		}
	}

	if *noLoad {
		fmt.Printf("\ndownloaded %d shard(s) to %s (not loaded)\n", len(sel), sd)
		return exitOK
	}
	db, err := openDB(cfg)
	if err != nil {
		return errReturn(err)
	}
	defer db.Close()
	if err := rebuildViews(db, cfg); err != nil {
		return errReturn(err)
	}
	fmt.Printf("\nloaded %d shard(s); query with `integer-atlas sql \"SELECT ... FROM numbers\"`\n", len(sel))
	return exitOK
}
