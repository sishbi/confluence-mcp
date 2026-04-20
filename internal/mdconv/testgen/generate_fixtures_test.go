package testgen

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// StandardFixtures defines the deterministic test documents.
// Each has a pinned SHA256 — if any changes, the generator's determinism is broken.
var StandardFixtures = []struct {
	Name   string
	Config DocConfig
	SHA256 string // hex-encoded, pinned after first run
}{
	{
		Name:   "small-simple",
		Config: DocConfig{Seed: 100, Sections: 2, Complexity: 1},
		SHA256: "82c6ba894485e26c5b76a48ba5fc4cf541bf081ffe1e3912d1259ac9be6509e3",
	},
	{
		Name:   "medium-mixed",
		Config: DocConfig{Seed: 200, Sections: 5, Complexity: 2},
		SHA256: "4aaaeacc0b1d8951720933e4b7f0c7f88ef4f20fed2c86de31bdf5ef06c32c93",
	},
	{
		Name:   "medium-complex",
		Config: DocConfig{Seed: 300, Sections: 5, Complexity: 3},
		SHA256: "e7b13090739c1e122873806406820158a3015fad67f6c2240985956d2722fead",
	},
	{
		Name:   "large-mixed",
		Config: DocConfig{Seed: 400, Sections: 30, Complexity: 2, TargetBytes: 50_000},
		SHA256: "87c68881a09b274c3d861a942c1abe546703c190c9b38cc5e79c3fee3952efd8",
	},
	{
		Name:   "large-complex",
		Config: DocConfig{Seed: 500, Sections: 30, Complexity: 3, TargetBytes: 80_000},
		SHA256: "c8afb60518fb145297ca35d02a607bc8cca8c3f17a6c45e6db2e951b4bc74469",
	},
}

func TestGenerateStandardFixtures(t *testing.T) {
	testdataDir := filepath.Join("..", "testdata")
	if err := os.MkdirAll(testdataDir, 0o755); err != nil {
		t.Fatalf("creating testdata dir: %v", err)
	}

	for _, sf := range StandardFixtures {
		t.Run(sf.Name, func(t *testing.T) {
			doc := GenerateStorageFormat(sf.Config)
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
			t.Logf("SHA256 for %s: %s (len=%d)", sf.Name, hash, len(doc))

			if sf.SHA256 != "" {
				assert.Equal(t, sf.SHA256, hash, "generator determinism broken for %s", sf.Name)
			}

			path := filepath.Join(testdataDir, sf.Name+".json.gz")
			fixture := &Fixture{
				Name:    sf.Name,
				Source:  fmt.Sprintf("generated: seed=%d sections=%d complexity=%d", sf.Config.Seed, sf.Config.Sections, sf.Config.Complexity),
				Content: doc,
			}
			err := SaveFixture(path, fixture)
			require.NoError(t, err)

			loaded, err := LoadFixture(path)
			require.NoError(t, err)
			assert.Equal(t, doc, loaded.Content)

			fp := FingerprintStorageFormat(doc)
			t.Logf("Fingerprint: headings=%v codeBlocks=%d links=%d listItems=%d textLen=%d",
				fp.HeadingCount, fp.CodeBlockCount, fp.LinkCount, fp.ListItemCount, fp.TextLength)
		})
	}
}
