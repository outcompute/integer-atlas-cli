package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdPacks(args []string) int {
	fs := flag.NewFlagSet("packs", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	state := fs.String("state", "accepted", "accepted|pending|all")
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

	type group struct {
		table  string
		cols   []string
		shards int
		minS   int64
		maxE   int64
		rows   int64
	}
	groups := map[string]*group{}
	var keys []string
	for _, m := range accepted {
		cols := m.columnNames()
		key := m.tableName() + "|" + strings.Join(cols, ",")
		g := groups[key]
		if g == nil {
			g = &group{table: m.tableName(), cols: cols, minS: m.RangeStart, maxE: m.RangeEnd}
			groups[key] = g
			keys = append(keys, key)
		}
		g.shards++
		if m.RangeStart < g.minS {
			g.minS = m.RangeStart
		}
		if m.RangeEnd > g.maxE {
			g.maxE = m.RangeEnd
		}
		g.rows += m.RowCount
	}
	sort.Strings(keys)

	if cfg.JSON {
		type packView struct {
			Table      string   `json:"table"`
			Columns    []string `json:"columns"`
			Shards     int      `json:"shards"`
			RangeStart int64    `json:"range_start"`
			RangeEnd   int64    `json:"range_end"`
			Rows       int64    `json:"rows"`
		}
		list := []packView{}
		for _, k := range keys {
			g := groups[k]
			list = append(list, packView{g.table, g.cols, g.shards, g.minS, g.maxE, g.rows})
		}
		return printJSON(list)
	}

	if *state != "pending" {
		if len(keys) == 0 {
			fmt.Println("(no accepted shards)")
		} else {
			fmt.Printf("%-10s %-7s %-24s %s\n", "TABLE", "SHARDS", "RANGE", "COLUMNS")
			for _, k := range keys {
				g := groups[k]
				fmt.Printf("%-10s %-7d %-24s %s\n", g.table, g.shards,
					fmt.Sprintf("%d..%d", g.minS, g.maxE), strings.Join(g.cols, ","))
			}
		}
	}
	if *state == "pending" || *state == "all" {
		pend, _ := loadWorkOrders(pendingDir(root))
		fmt.Printf("\npending work orders: %d  (run `integer-atlas work`)\n", len(pend))
	}
	return exitOK
}

func cmdDescribe(args []string) int {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	// Accept the column as a leading positional (before flags) or after them.
	var col string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		col, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if col == "" {
		col = fs.Arg(0)
	}
	if col == "" {
		fmt.Fprintln(os.Stderr, "usage: integer-atlas describe <column>")
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

	var typ string
	var minS, maxE int64
	var shards int
	for _, m := range accepted {
		for _, c := range m.Columns {
			if c.Name != col {
				continue
			}
			if shards == 0 {
				minS, maxE = m.RangeStart, m.RangeEnd
			}
			typ = c.Type
			if m.RangeStart < minS {
				minS = m.RangeStart
			}
			if m.RangeEnd > maxE {
				maxE = m.RangeEnd
			}
			shards++
		}
	}
	if shards == 0 {
		fmt.Printf("column %q not found in accepted shards\n", col)
		return exitNotFound
	}
	if cfg.JSON {
		return printJSON(map[string]any{"column": col, "type": typ, "shards": shards,
			"range_start": minS, "range_end": maxE})
	}
	fmt.Printf("%s\n  type:   %s\n  shards: %d\n  range:  %d..%d\n", col, typ, shards, minS, maxE)
	fmt.Println("  (richer description / OEIS come from the Algos catalog when the toolchain is synced)")
	return exitOK
}

func cmdWork(args []string) int {
	fs := flag.NewFlagSet("work", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, err := resolveRegistry(cfg)
	if err != nil {
		return errReturn(err)
	}
	pend, err := loadWorkOrders(pendingDir(root))
	if err != nil {
		return errReturn(err)
	}
	if cfg.JSON {
		return printJSON(pend)
	}
	if len(pend) == 0 {
		fmt.Println("no pending work orders")
		return exitOK
	}
	fmt.Printf("%-42s %-24s %-5s %s\n", "ID", "RANGE", "COLS", "EST")
	for _, w := range pend {
		fmt.Printf("%-42s %-24s %-5d %s\n", w.ID,
			fmt.Sprintf("%d..%d", w.RangeStart, w.RangeEnd), len(w.Columns), estStr(w.CostSeconds))
	}
	return exitOK
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dbPath := filepath.Join(cfg.Workspace, "atlas.duckdb")
	st := map[string]any{"workspace": cfg.Workspace, "registry": cfg.Registry,
		"database": dbPath, "engine": "embedded (go-duckdb)"}
	if fi, err := os.Stat(dbPath); err == nil {
		st["database_bytes"] = fi.Size()
	}
	root, err := resolveRegistry(cfg)
	if err != nil {
		st["registry_error"] = err.Error()
	} else {
		acc, _ := loadManifests(acceptedDir(root))
		pend, _ := loadWorkOrders(pendingDir(root))
		st["registry_path"] = root
		st["accepted_shards"] = len(acc)
		st["pending_work_orders"] = len(pend)
	}
	if cfg.JSON {
		return printJSON(st)
	}
	fmt.Printf("workspace : %s\n", cfg.Workspace)
	fmt.Printf("registry  : %s\n", cfg.Registry)
	if v, ok := st["registry_path"]; ok {
		fmt.Printf("repo copy : %s\n", v)
		fmt.Printf("accepted  : %v shards\n", st["accepted_shards"])
		fmt.Printf("pending   : %v work orders\n", st["pending_work_orders"])
	} else {
		fmt.Printf("registry  : ERROR %v\n", st["registry_error"])
	}
	if b, ok := st["database_bytes"].(int64); ok {
		fmt.Printf("database  : %s (%.1f MB)\n", dbPath, float64(b)/1e6)
	} else {
		fmt.Printf("database  : %s (not created yet)\n", dbPath)
	}
	fmt.Printf("engine    : embedded (go-duckdb)\n")
	return exitOK
}

func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	ok := true
	check := func(name string, good, required bool, detail string) {
		mark := "ok"
		if !good {
			if required {
				mark = "FAIL"
				ok = false
			} else {
				mark = "warn"
			}
		}
		fmt.Printf("[%-4s] %-32s %s\n", mark, name, detail)
	}
	check("git", haveExec("git"), true, "required to read remote registries")
	root, err := resolveRegistry(cfg)
	check("registry", err == nil, true, cfg.Registry)
	if err == nil {
		acc, _ := loadManifests(acceptedDir(root))
		pend, _ := loadWorkOrders(pendingDir(root))
		check("manifests", true, true, fmt.Sprintf("%d accepted, %d pending", len(acc), len(pend)))
	}
	check("atlas-algos", haveExec("atlas-algos"), false, "optional; needed for compute/verify")
	dbOK := false
	if db, derr := openDB(cfg); derr == nil {
		dbOK = db.Ping() == nil
		db.Close()
	}
	check("duckdb (embedded)", dbOK, true, "go-duckdb")
	if ok {
		return exitOK
	}
	return exitErr
}
