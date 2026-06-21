package main

import (
	"fmt"
	"path/filepath"
)

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
	} else if c.Refresh {
		_ = gitRun("-C", dest, "fetch", "--depth", "1", "origin")
		_ = gitRun("-C", dest, "reset", "--hard", "origin/HEAD")
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
