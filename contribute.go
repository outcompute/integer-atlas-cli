package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// compute, verify, and submit: bridge between the Shards manifest shapes and the
// stateless Algos executor (atlas-algos). sideload lives in db.go (it uses DuckDB).

func algosBinFlag(fs *flag.FlagSet) *string {
	return fs.String("algos-bin", envOr("INTEGER_ATLAS_ALGOS", "atlas-algos"),
		"path/name of the atlas-algos executable")
}

// algosWorkOrder is what `atlas-algos compute` expects.
type algosWorkOrder struct {
	ID               string   `json:"id,omitempty"`
	Start            int64    `json:"start"`
	End              int64    `json:"end"`
	Columns          []string `json:"columns"`
	AlgorithmRelease string   `json:"algorithm_release,omitempty"`
}

// algosManifest is what `atlas-algos compute` writes (and its result points at).
type algosManifest struct {
	Filename         string   `json:"filename"`
	RangeStart       int64    `json:"range_start"`
	RangeEnd         int64    `json:"range_end"`
	RowCount         int64    `json:"row_count"`
	Columns          []Column `json:"columns"`
	Format           string   `json:"format"`
	Compression      string   `json:"compression"`
	Hashes           Hashes   `json:"hashes"`
	AlgorithmRelease string   `json:"algorithm_release"`
	GeneratedAt      string   `json:"generated_at_utc"`
}

type computeResult struct {
	Status   string `json:"status"`
	Shard    string `json:"shard"`
	Manifest string `json:"manifest"`
	RowCount int64  `json:"row_count"`
}

func cmdCompute(args []string) int {
	fs := flag.NewFlagSet("compute", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	task := fs.String("task", "", "pending work-order id from the registry")
	woFile := fs.String("manifest", "", "a custom work-order JSON file")
	start := fs.Int64("start", 0, "range start (with --end --columns)")
	end := fs.Int64("end", 0, "range end")
	cols := fs.String("columns", "", "comma-separated columns")
	out := fs.String("out", "", "output shard path (default ./<id>)")
	chunk := fs.Int("chunk-size", 0, "rows per chunk (0 = executor default)")
	format := fs.String("format", "parquet", "parquet|csv")
	algosRelease := fs.String("algos-release", "", "algorithm release to stamp/pin")
	algosBin := algosBinFlag(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	// Resolve the work order.
	var wo WorkOrder
	switch {
	case *task != "":
		root, err := resolveRegistry(cfg)
		if err != nil {
			return errReturn(err)
		}
		orders, err := loadWorkOrders(pendingDir(root))
		if err != nil {
			return errReturn(err)
		}
		found := false
		for _, w := range orders {
			if w.ID == *task {
				wo, found = w, true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "work order %q not found in pending/\n", *task)
			return exitNotFound
		}
	case *woFile != "":
		b, err := os.ReadFile(*woFile)
		if err != nil {
			return errReturn(err)
		}
		if err := json.Unmarshal(b, &wo); err != nil {
			return errReturn(fmt.Errorf("%s: %w", *woFile, err))
		}
	case *end > 0 && *cols != "":
		wo = WorkOrder{ID: "custom", RangeStart: *start, RangeEnd: *end, Columns: splitCSV(*cols)}
	default:
		fmt.Fprintln(os.Stderr, "usage: integer-atlas compute (--task ID | --manifest FILE | --start S --end E --columns C)")
		return exitUsage
	}
	if *algosRelease != "" {
		wo.AlgorithmRelease = *algosRelease
	}

	bin, err := resolveAlgos(*algosBin)
	if err != nil {
		return errReturn(err)
	}

	if err := cfg.ensureWorkspace(); err != nil {
		return errReturn(err)
	}
	// Write the executor's work-order shape to a temp file.
	awo := algosWorkOrder{ID: wo.ID, Start: wo.RangeStart, End: wo.RangeEnd,
		Columns: wo.Columns, AlgorithmRelease: wo.AlgorithmRelease}
	tmp, err := os.CreateTemp(cfg.cacheDir(), "workorder-*.json")
	if err != nil {
		return errReturn(err)
	}
	defer os.Remove(tmp.Name())
	if err := json.NewEncoder(tmp).Encode(awo); err != nil {
		return errReturn(err)
	}
	tmp.Close()

	outPath := *out
	if outPath == "" {
		outPath = "./" + wo.ID
	}
	cargs := []string{"compute", "--manifest", tmp.Name(), "--out", outPath, "--format", *format}
	if *chunk > 0 {
		cargs = append(cargs, "--chunk-size", fmt.Sprintf("%d", *chunk))
	}
	stdout, code := runCaptureStdout(bin, cargs...)
	if code != 0 {
		return exitErr
	}
	var res computeResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		return errReturn(fmt.Errorf("parse executor result: %w", err))
	}

	// Translate the executor's manifest into the Shards manifest shape (draft).
	amBytes, err := os.ReadFile(res.Manifest)
	if err != nil {
		return errReturn(err)
	}
	var am algosManifest
	if err := json.Unmarshal(amBytes, &am); err != nil {
		return errReturn(err)
	}
	table := wo.Table
	if table == "" {
		table = "numbers"
	}
	sm := Manifest{
		File: am.Filename, Table: table, RangeStart: am.RangeStart, RangeEnd: am.RangeEnd,
		RowCount: am.RowCount, Columns: am.Columns, Format: am.Format, Compression: am.Compression,
		AlgorithmRelease: am.AlgorithmRelease, GeneratedAt: am.GeneratedAt, Hashes: am.Hashes,
		Storage: []string{}, Verification: Verification{Status: "computed"},
	}
	smBytes, _ := json.MarshalIndent(sm, "", "  ")
	if err := os.WriteFile(res.Manifest, append(smBytes, '\n'), 0o644); err != nil {
		return errReturn(err)
	}

	fmt.Printf("\nshard:    %s  (%d rows)\n", res.Shard, res.RowCount)
	fmt.Printf("manifest: %s  (Shards format, draft)\n", res.Manifest)
	fmt.Println("next: verify it, upload the shard, then `integer-atlas submit --manifest <manifest> --url <loc>`")
	return exitOK
}

func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	manifestPath := fs.String("manifest", "", "manifest JSON to verify")
	shardPath := fs.String("shard", "", "shard file (default: located via the manifest)")
	degree := fs.Float64("degree", 0.1, "fraction of rows to recompute (0..1)")
	seed := fs.Int("seed", 0, "sample seed")
	algosBin := algosBinFlag(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: integer-atlas verify --manifest FILE [--degree F]")
		return exitUsage
	}
	b, err := os.ReadFile(*manifestPath)
	if err != nil {
		return errReturn(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errReturn(err)
	}

	// Locate the shard: explicit, then next to the manifest, then download.
	path := *shardPath
	if path == "" {
		cand := filepath.Join(filepath.Dir(*manifestPath), m.File)
		if _, err := os.Stat(cand); err == nil {
			path = cand
		}
	}
	if path == "" {
		if len(m.Storage) == 0 {
			return errReturn(fmt.Errorf("cannot locate shard %q (no local copy, no storage URL)", m.File))
		}
		if err := cfg.ensureWorkspace(); err != nil {
			return errReturn(err)
		}
		dir := filepath.Join(cfg.cacheDir(), "shards")
		os.MkdirAll(dir, 0o755)
		path = filepath.Join(dir, m.File)
		fmt.Printf("downloading %s\n", m.File)
		if err := download(m.Storage[0], path); err != nil {
			return errReturn(err)
		}
	}

	// Hash check (the CLI does this itself before recomputing): every digest the
	// manifest declares, not just sha256.
	checked, mismatch, err := verifyHashes(path, m.Hashes)
	if err != nil {
		return errReturn(err)
	}
	if mismatch != "" {
		fmt.Fprintln(os.Stderr, mismatch)
		return exitVerify
	}
	if len(checked) > 0 {
		fmt.Printf("%s ok\n", strings.Join(checked, "+"))
	}

	bin, err := resolveAlgos(*algosBin)
	if err != nil {
		return errReturn(err)
	}
	stdout, code := runCaptureStdout(bin, "verify", "--manifest", *manifestPath,
		"--shard", path, "--degree", fmt.Sprintf("%g", *degree), "--seed", fmt.Sprintf("%d", *seed))
	if stdout != "" {
		fmt.Print(stdout)
	}
	switch code {
	case 0:
		return exitOK
	case 3:
		return exitVerify
	default:
		return exitErr
	}
}

func cmdSubmit(args []string) int {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	_ = addGlobalFlags(fs)
	manifestPath := fs.String("manifest", "", "draft manifest to finalize")
	url := fs.String("url", "", "public URL where the shard is hosted")
	author := fs.String("author", "", "author name")
	license := fs.String("license", "", "license (e.g. CC0-1.0)")
	out := fs.String("out", "", "also write the final manifest here")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *manifestPath == "" || *url == "" {
		fmt.Fprintln(os.Stderr, "usage: integer-atlas submit --manifest FILE --url URL")
		return exitUsage
	}
	b, err := os.ReadFile(*manifestPath)
	if err != nil {
		return errReturn(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errReturn(err)
	}
	m.Storage = []string{*url}
	if *author != "" {
		m.Author = *author
	}
	if *license != "" {
		m.License = *license
	}
	final, _ := json.MarshalIndent(m, "", "  ")
	if *out != "" {
		if err := os.WriteFile(*out, append(final, '\n'), 0o644); err != nil {
			return errReturn(err)
		}
	}

	dest := fmt.Sprintf("computed/%s/%s.json", m.tableName(), trimExt(m.File))
	fmt.Println("=== manifest (add to the Shards repo at the path below) ===")
	fmt.Printf("path: %s\n\n", dest)
	fmt.Println(string(final))
	fmt.Println("\n=== PR ===")
	fmt.Printf("title: add %s\n", m.File)
	fmt.Printf("body:  %s shard %d..%d (%d columns) computed with %q; hosted at %s\n",
		m.tableName(), m.RangeStart, m.RangeEnd, len(m.Columns), m.AlgorithmRelease, *url)
	fmt.Println("Open a PR against the Shards repo adding the file above, then merge after CI verify.")
	return exitOK
}

func trimExt(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		return name[:len(name)-len(ext)]
	}
	return name
}
