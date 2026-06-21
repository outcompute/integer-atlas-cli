package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func printJSON(v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitErr
	}
	fmt.Println(string(b))
	return exitOK
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasAnyColumn(m Manifest, want []string) bool {
	set := map[string]bool{}
	for _, c := range m.Columns {
		set[c.Name] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func estStr(sec float64) string {
	switch {
	case sec <= 0:
		return "-"
	case sec < 90:
		return fmt.Sprintf("%.0fs", sec)
	case sec < 5400:
		return fmt.Sprintf("%.0fm", sec/60)
	default:
		return fmt.Sprintf("%.1fh", sec/3600)
	}
}

func haveExec(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func errReturn(err error) int {
	fmt.Fprintln(os.Stderr, "error:", err)
	return exitErr
}

// runCaptureStdout runs a command, streaming its stderr through, capturing stdout,
// and returning the child's exit code (-1 if it couldn't start).
func runCaptureStdout(name string, args ...string) (string, int) {
	cmd := exec.Command(name, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "error running %s: %v\n", name, err)
	return out.String(), -1
}

// resolveAlgos returns a runnable atlas-algos path/name or an error with install hints.
func resolveAlgos(bin string) (string, error) {
	if strings.ContainsRune(bin, '/') {
		if _, err := os.Stat(bin); err != nil {
			return "", fmt.Errorf("%s not found", bin)
		}
		return bin, nil
	}
	if haveExec(bin) {
		return bin, nil
	}
	return "", fmt.Errorf("%q not found on PATH — install the Algos toolchain "+
		"(pip install integer-atlas-algos) or pass --algos-bin", bin)
}
