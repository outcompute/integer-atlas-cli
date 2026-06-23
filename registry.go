package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// registryTTL is how long a cached registry clone is trusted before an automatic
// refresh. `--refresh` forces one regardless.
const registryTTL = 5 * time.Minute

func fetchMarker(dest string) string { return filepath.Join(dest, ".git", "ia-last-fetch") }

func registryStale(dest string) bool {
	fi, err := os.Stat(fetchMarker(dest))
	if err != nil {
		return true
	}
	return time.Since(fi.ModTime()) > registryTTL
}

func touchFetchMarker(dest string) {
	_ = os.WriteFile(fetchMarker(dest), []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

// resolveRegistry returns the local path to the shards repo: a local directory used
// as-is, or a cached shallow clone of a git URL. With --release it checks out a ref.
func resolveRegistry(c *Config) (string, error) {
	if isDir(c.Registry) {
		abs, err := filepath.Abs(c.Registry)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	if err := c.ensureWorkspace(); err != nil {
		return "", err
	}
	dest := filepath.Join(c.cacheDir(), "registry")
	if !isDir(filepath.Join(dest, ".git")) {
		if err := gitRun("clone", "--depth", "1", c.Registry, dest); err != nil {
			return "", fmt.Errorf("clone registry %s: %w", c.Registry, err)
		}
		touchFetchMarker(dest)
	} else if c.Refresh || registryStale(dest) {
		// Auto-refresh on a short TTL (or when --refresh is given) so packs/work/
		// status reflect the published registry rather than a stale first clone.
		if err := gitRun("-C", dest, "fetch", "-q", "--depth", "1", "origin"); err == nil {
			_ = gitRun("-C", dest, "reset", "--hard", "-q", "FETCH_HEAD")
			touchFetchMarker(dest)
		}
	}
	if c.Release != "" {
		if err := gitRun("-C", dest, "fetch", "--depth", "1", "origin", c.Release); err != nil {
			return "", fmt.Errorf("fetch ref %s: %w", c.Release, err)
		}
		if err := gitRun("-C", dest, "checkout", c.Release); err != nil {
			return "", fmt.Errorf("checkout %s: %w", c.Release, err)
		}
	}
	return dest, nil
}

func acceptedDir(root string) string { return filepath.Join(root, "accepted") }
func pendingDir(root string) string  { return filepath.Join(root, "pending") }
