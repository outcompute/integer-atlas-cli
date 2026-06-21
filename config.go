package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
