package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/marcboeker/go-duckdb/v2"
)

func openDB(cfg *Config) (*sql.DB, error) {
	if err := cfg.ensureWorkspace(); err != nil {
		return nil, err
	}
	return sql.Open("duckdb", filepath.Join(cfg.Workspace, "atlas.duckdb"))
}

func shardsDir(cfg *Config) string    { return filepath.Join(cfg.cacheDir(), "shards") }
func manifestsDir(cfg *Config) string { return filepath.Join(cfg.cacheDir(), "manifests") }

// sqlQuote escapes a value for a single-quoted SQL string literal.
func sqlQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

func readerFor(absPath string) string {
	q := sqlQuote(absPath)
	if strings.EqualFold(filepath.Ext(absPath), ".csv") {
		return fmt.Sprintf("read_csv_auto('%s')", q)
	}
	return fmt.Sprintf("read_parquet('%s')", q)
}

// rebuildViews (re)creates one logical view per table from the cached shard manifests:
// shards of the same column set are unioned across ranges, and different column groups
// are FULL-joined on n. Idempotent; safe to call whenever the cache changes.
func rebuildViews(db *sql.DB, cfg *Config) error {
	mans, err := loadManifests(manifestsDir(cfg))
	if err != nil {
		return err
	}
	byTable := map[string][]Manifest{}
	for _, m := range mans {
		if _, err := os.Stat(filepath.Join(shardsDir(cfg), m.File)); err != nil {
			continue // shard file not present locally
		}
		byTable[m.tableName()] = append(byTable[m.tableName()], m)
	}
	for table, list := range byTable {
		stmt, err := tableViewSQL(table, list, shardsDir(cfg))
		if err != nil {
			return err
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("build view %q: %w", table, err)
		}
	}

	// Unified `numbers` view: FULL JOIN every loaded table on n, so a consumer can
	// query columns from any pack together (this is the documented entry point).
	// Skipped if a pack is itself named "numbers" (its own view already serves).
	if _, isNumbers := byTable["numbers"]; !isNumbers && len(byTable) > 0 {
		tnames := make([]string, 0, len(byTable))
		for t := range byTable {
			tnames = append(tnames, t)
		}
		sort.Strings(tnames)
		q := `CREATE OR REPLACE VIEW "numbers" AS SELECT * FROM "` + tnames[0] + `"`
		for i := 1; i < len(tnames); i++ {
			q += ` FULL JOIN "` + tnames[i] + `" USING (n)`
		}
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("build view numbers: %w", err)
		}
	}
	return nil
}

func tableViewSQL(table string, list []Manifest, dir string) (string, error) {
	groups := map[string][]Manifest{}
	var order []string
	for _, m := range list {
		key := strings.Join(m.columnNames(), ",")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], m)
	}
	sort.Strings(order)

	var subs []string
	for i, key := range order {
		var selects []string
		for _, m := range groups[key] {
			abs, _ := filepath.Abs(filepath.Join(dir, m.File))
			selects = append(selects, "SELECT * FROM "+readerFor(abs))
		}
		subs = append(subs, "("+strings.Join(selects, " UNION ALL BY NAME ")+") AS g"+strconv.Itoa(i))
	}
	q := `CREATE OR REPLACE VIEW "` + table + `" AS SELECT * FROM ` + subs[0]
	for i := 1; i < len(subs); i++ {
		q += " FULL JOIN " + subs[i] + " USING (n)"
	}
	return q, nil
}

// ---- sql command ----

func cmdSQL(args []string) int {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	var query string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query, args = args[0], args[1:]
	}
	file := fs.String("file", "", "read the query from a file")
	format := fs.String("format", "table", "table|csv|json|parquet")
	out := fs.String("output", "", "write results to a file (uses --format)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if query == "" {
		query = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}

	db, err := openDB(cfg)
	if err != nil {
		return errReturn(err)
	}
	defer db.Close()
	if err := rebuildViews(db, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not (re)build views:", err)
	}

	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return errReturn(err)
		}
		query = string(b)
	}
	if strings.TrimSpace(query) == "" {
		return sqlRepl(db, *format)
	}
	return runStmt(db, query, *format, *out)
}

func runStmt(db *sql.DB, query, format, out string) int {
	query = strings.TrimRight(strings.TrimSpace(query), ";")
	if out != "" {
		stmt := fmt.Sprintf("COPY (%s) TO '%s' (FORMAT %s)", query, sqlQuote(out), copyFormat(format))
		if _, err := db.Exec(stmt); err != nil {
			return errReturn(err)
		}
		fmt.Printf("written %s\n", out)
		return exitOK
	}
	rows, err := db.Query(query)
	if err != nil {
		return errReturn(err)
	}
	defer rows.Close()
	return printRows(rows, format)
}

func sqlRepl(db *sql.DB, format string) int {
	fmt.Fprintln(os.Stderr, `integer-atlas sql — type a query; \q to quit`)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for {
		fmt.Fprint(os.Stderr, "sql> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "":
			continue
		case `\q`, "exit", "quit":
			return exitOK
		}
		rows, err := db.Query(strings.TrimRight(line, ";"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		printRows(rows, format)
		rows.Close()
	}
	return exitOK
}

func copyFormat(format string) string {
	if format == "table" {
		return "csv"
	}
	return format
}

func scanAll(rows *sql.Rows) ([]string, [][]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var data [][]any
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range ptrs {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		data = append(data, raw)
	}
	return cols, data, rows.Err()
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func printRows(rows *sql.Rows, format string) int {
	cols, data, err := scanAll(rows)
	if err != nil {
		return errReturn(err)
	}
	switch format {
	case "json":
		out := make([]map[string]any, 0, len(data))
		for _, r := range data {
			m := map[string]any{}
			for i, c := range cols {
				if b, ok := r[i].([]byte); ok {
					m[c] = string(b)
				} else {
					m[c] = r[i]
				}
			}
			out = append(out, m)
		}
		return printJSON(out)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		_ = w.Write(cols)
		for _, r := range data {
			rec := make([]string, len(cols))
			for i := range cols {
				rec[i] = cellString(r[i])
			}
			_ = w.Write(rec)
		}
		w.Flush()
		return exitOK
	default: // table
		widths := make([]int, len(cols))
		for i, c := range cols {
			widths[i] = len(c)
		}
		strData := make([][]string, len(data))
		for ri, r := range data {
			strData[ri] = make([]string, len(cols))
			for i := range cols {
				s := cellString(r[i])
				strData[ri][i] = s
				if len(s) > widths[i] {
					widths[i] = len(s)
				}
			}
		}
		printRow := func(vals []string) {
			parts := make([]string, len(vals))
			for i, v := range vals {
				parts[i] = fmt.Sprintf("%-*s", widths[i], v)
			}
			fmt.Println(strings.Join(parts, "  "))
		}
		printRow(cols)
		for _, r := range strData {
			printRow(r)
		}
		fmt.Printf("(%d rows)\n", len(data))
		return exitOK
	}
}

// ---- sideload command ----

func cmdSideload(args []string) int {
	fs := flag.NewFlagSet("sideload", flag.ContinueOnError)
	cfg := addGlobalFlags(fs)
	var file string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		file, args = args[0], args[1:]
	}
	name := fs.String("table", "", "side table name (default derived from the file)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if file == "" {
		file = fs.Arg(0)
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "usage: integer-atlas sideload <shard-file> [--table NAME]")
		return exitUsage
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return errReturn(err)
	}
	if _, err := os.Stat(abs); err != nil {
		return errReturn(err)
	}
	view := "side_" + sanitizeIdent(*name)
	if *name == "" {
		base := filepath.Base(abs)
		view = "side_" + sanitizeIdent(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	db, err := openDB(cfg)
	if err != nil {
		return errReturn(err)
	}
	defer db.Close()
	stmt := fmt.Sprintf(`CREATE OR REPLACE VIEW "%s" AS SELECT * FROM %s`, view, readerFor(abs))
	if _, err := db.Exec(stmt); err != nil {
		return errReturn(err)
	}
	fmt.Printf("sideloaded %s as view %q\n", filepath.Base(abs), view)
	fmt.Printf("query it, e.g.:  integer-atlas sql \"SELECT * FROM %s LIMIT 5\"\n", view)
	return exitOK
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "shard"
	}
	return out
}
