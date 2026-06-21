package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func cmdFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	start := fs.Int64("start", 0, "range start (inclusive)")
	end := fs.Int64("end", 0, "range end (inclusive)")
	cols := fs.String("columns", "", "comma-separated columns to fetch")
	noLoad := fs.Bool("no-load", false, "download/verify only; skip loading into the DB")
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

	want := splitCSV(*cols)
	var sel []Manifest
	for _, m := range accepted {
		if *end > 0 && (m.RangeEnd < *start || m.RangeStart > *end) {
			continue // no overlap
		}
		if len(want) > 0 && !hasAnyColumn(m, want) {
			continue
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
		got, err := sha256File(dest)
		if err != nil {
			return errReturn(err)
		}
		if m.Hashes.SHA256 != "" && got != m.Hashes.SHA256 {
			fmt.Fprintf(os.Stderr, "  hash mismatch (want %s, got %s)\n", shortHash(m.Hashes.SHA256), shortHash(got))
			return exitVerify
		}
		// cache the manifest so the DB views can be (re)built from the cache.
		mb, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(md, m.File+".json"), append(mb, '\n'), 0o644); err != nil {
			return errReturn(err)
		}
		fmt.Printf("  ok %s\n", shortHash(got))
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
