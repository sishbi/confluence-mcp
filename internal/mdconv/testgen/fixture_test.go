package testgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFixture_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json.gz")

	fixture := &Fixture{
		Name:    "test-doc",
		Source:  "unit test",
		Content: "<h1>Test</h1><p>Hello world</p>",
	}

	err := SaveFixture(path, fixture)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	loaded, err := LoadFixture(path)
	require.NoError(t, err)
	assert.Equal(t, fixture.Name, loaded.Name)
	assert.Equal(t, fixture.Source, loaded.Source)
	assert.Equal(t, fixture.Content, loaded.Content)
}

func TestLoadFixture_NotFound(t *testing.T) {
	_, err := LoadFixture("/nonexistent/path.json.gz")
	assert.Error(t, err)
}
