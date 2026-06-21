package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Hashes struct {
	SHA256 string `json:"sha256"`
	SHA512 string `json:"sha512,omitempty"`
	BLAKE3 string `json:"blake3,omitempty"`
}

type Verification struct {
	Status     string  `json:"status"`
	Degree     float64 `json:"degree,omitempty"`
	VerifiedAt string  `json:"verified_at_utc,omitempty"`
}

// Manifest is one accepted/computed shard pointer.
type Manifest struct {
	File             string       `json:"file"`
	Table            string       `json:"table"`
	RangeStart       int64        `json:"range_start"`
	RangeEnd         int64        `json:"range_end"`
	RowCount         int64        `json:"row_count"`
	Columns          []Column     `json:"columns"`
	Format           string       `json:"format"`
	Compression      string       `json:"compression,omitempty"`
	AlgorithmRelease string       `json:"algorithm_release"`
	GeneratedAt      string       `json:"generated_at_utc,omitempty"`
	Hashes           Hashes       `json:"hashes"`
	Storage          []string     `json:"storage"`
	Verification     Verification `json:"verification"`
	Author           string       `json:"author,omitempty"`
	License          string       `json:"license,omitempty"`
	Path             string       `json:"-"`
}

func (m Manifest) tableName() string {
	if m.Table == "" {
		return "numbers"
	}
	return m.Table
}

// columnNames returns the property column names (excluding the key column n), sorted.
func (m Manifest) columnNames() []string {
	var out []string
	for _, c := range m.Columns {
		if c.Name == "n" {
			continue
		}
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

// WorkOrder is one pending hole to compute.
type WorkOrder struct {
	ID               string   `json:"id"`
	Table            string   `json:"table"`
	RangeStart       int64    `json:"range_start"`
	RangeEnd         int64    `json:"range_end"`
	Columns          []string `json:"columns"`
	AlgorithmRelease string   `json:"algorithm_release"`
	ExpectedRowCount int64    `json:"expected_row_count,omitempty"`
	CostSeconds      float64  `json:"cost_estimate_seconds,omitempty"`
	Path             string   `json:"-"`
}

func loadManifests(dir string) ([]Manifest, error) {
	var out []Manifest
	if !isDir(dir) {
		return out, nil
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		m.Path = p
		out = append(out, m)
		return nil
	})
	return out, err
}

func loadWorkOrders(dir string) ([]WorkOrder, error) {
	var out []WorkOrder
	if !isDir(dir) {
		return out, nil
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var w WorkOrder
		if err := json.Unmarshal(b, &w); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		w.Path = p
		out = append(out, w)
		return nil
	})
	return out, err
}
