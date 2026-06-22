package main

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"lukechampine.com/blake3"
)

const (
	exitOK       = 0
	exitErr      = 1
	exitUsage    = 2
	exitNotFound = 3
	exitVerify   = 4
)

const defaultRegistry = "https://github.com/outcompute/integer-atlas-shards"

// Config holds the global flags shared by every command.
type Config struct {
	Workspace string
	Registry  string
	Release   string
	JSON      bool
	Refresh   bool
	LogLevel  string
	Yes       bool
}

func addGlobalFlags(fs *flag.FlagSet) *Config {
	c := &Config{}
	fs.StringVar(&c.Workspace, "workspace", defaultWorkspace(), "local workspace directory")
	fs.StringVar(&c.Registry, "registry", envOr("INTEGER_ATLAS_REGISTRY", defaultRegistry), "shards repo (git URL or local path)")
	fs.StringVar(&c.Release, "release", "", "git ref of the shards repo to pin")
	fs.BoolVar(&c.JSON, "json", false, "machine-readable JSON output")
	fs.BoolVar(&c.Refresh, "refresh", false, "force refresh the local shards-repo copy")
	fs.StringVar(&c.LogLevel, "log-level", "info", "log verbosity")
	fs.BoolVar(&c.Yes, "yes", false, "assume yes for prompts")
	fs.BoolVar(&c.Yes, "y", false, "assume yes for prompts")
	return c
}

func defaultWorkspace() string {
	if v := os.Getenv("INTEGER_ATLAS_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".integer-atlas"
	}
	return filepath.Join(home, ".integer-atlas")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c *Config) cacheDir() string { return filepath.Join(c.Workspace, "cache") }

func (c *Config) ensureWorkspace() error { return os.MkdirAll(c.cacheDir(), 0o755) }

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// verifyHashes recomputes, in a single read, every digest the manifest declares
// (sha256, sha512, blake3) and compares each. It returns the algorithms verified
// (in stable order). A non-empty mismatch string names the first digest that did
// not match; err is reserved for I/O problems. An unset digest is skipped.
func verifyHashes(path string, want Hashes) (checked []string, mismatch string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	type entry struct {
		name string
		want string
		h    hash.Hash
	}
	var entries []entry
	if want.SHA256 != "" {
		entries = append(entries, entry{"sha256", want.SHA256, sha256.New()})
	}
	if want.SHA512 != "" {
		entries = append(entries, entry{"sha512", want.SHA512, sha512.New()})
	}
	if want.BLAKE3 != "" {
		entries = append(entries, entry{"blake3", want.BLAKE3, blake3.New(32, nil)})
	}
	if len(entries) == 0 {
		return nil, "", nil
	}

	ws := make([]io.Writer, len(entries))
	for i, e := range entries {
		ws[i] = e.h
	}
	if _, err := io.Copy(io.MultiWriter(ws...), f); err != nil {
		return nil, "", err
	}

	for _, e := range entries {
		got := hex.EncodeToString(e.h.Sum(nil))
		if got != e.want {
			return checked, fmt.Sprintf("%s mismatch: want %s got %s", e.name, shortHash(e.want), shortHash(got)), nil
		}
		checked = append(checked, e.name)
	}
	return checked, "", nil
}

func download(url, dest string) error {
	if strings.HasPrefix(url, "file://") {
		return copyFile(strings.TrimPrefix(url, "file://"), dest)
	}
	if !strings.Contains(url, "://") {
		return copyFile(url, dest) // bare local path
	}
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
