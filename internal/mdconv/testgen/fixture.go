package testgen

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
)

// Fixture is a test document stored as a gzipped JSON file.
type Fixture struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Content string `json:"content"` // Confluence storage format XHTML
}

// LoadFixture reads a gzipped JSON fixture file.
func LoadFixture(path string) (*Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening fixture %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompressing fixture %s: %w", path, err)
	}
	defer func() { _ = gz.Close() }()

	var fixture Fixture
	if err := json.NewDecoder(gz).Decode(&fixture); err != nil {
		return nil, fmt.Errorf("decoding fixture %s: %w", path, err)
	}
	return &fixture, nil
}

// SaveFixture writes a Fixture as a gzipped JSON file.
func SaveFixture(path string, fixture *Fixture) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating fixture %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()

	enc := json.NewEncoder(gz)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fixture); err != nil {
		return fmt.Errorf("encoding fixture %s: %w", path, err)
	}
	return nil
}
